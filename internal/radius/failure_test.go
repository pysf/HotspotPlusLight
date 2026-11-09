package radius

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"layeh.com/radius"

	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

var rt0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func TestRetryCache(t *testing.T) {
	c := newRetryCache()
	k := retryKey{from: "10.0.0.1:1000", id: 7}
	if c.repeated(k, rt0) {
		t.Fatal("first sight is not a repeat")
	}
	if !c.repeated(k, rt0.Add(5*time.Second)) || !c.repeated(k, rt0.Add(59*time.Second)) {
		t.Error("a repeat within 60 s must be recognised")
	}
	// A repeat does not renew the time: at 60 s after the first count the same packet counts again.
	if c.repeated(k, rt0.Add(60*time.Second)) {
		t.Error("after 60 s the packet must count again")
	}
	other := []retryKey{{from: "10.0.0.1:1001", id: 7}, {from: "10.0.0.1:1000", id: 8}, {from: "10.0.0.1:1000", id: 7, auth: [16]byte{1}}}
	for _, o := range other {
		if c.repeated(o, rt0.Add(61*time.Second)) {
			t.Errorf("%+v is a different request", o)
		}
	}
}

func TestRetryCacheCapacity(t *testing.T) {
	c := newRetryCache()
	for i := range retryCapacity + 10 {
		c.repeated(retryKey{from: "a", id: byte(i), auth: [16]byte{byte(i >> 8)}}, rt0)
	}
	if len(c.seen) > retryCapacity || len(c.order) > retryCapacity {
		t.Fatalf("cache grew to %d / %d entries", len(c.seen), len(c.order))
	}
	// The oldest ones were dropped first, so the first request counts again; the newest is still known.
	if c.repeated(retryKey{from: "a", id: 0, auth: [16]byte{0}}, rt0) {
		t.Error("the oldest entry should be gone")
	}
	last := retryCapacity + 9
	if !c.repeated(retryKey{from: "a", id: byte(last), auth: [16]byte{byte(last >> 8)}}, rt0) {
		t.Error("the newest entry should be kept")
	}
}

// fakeFailures is a hand-written FailureRecorder.
type fakeFailures struct {
	mu      sync.Mutex
	reasons []domain.Reason
	err     error
}

func (f *fakeFailures) RecordLoginFailure(_ context.Context, r domain.Reason) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reasons = append(f.reasons, r)
	return f.err
}

func (f *fakeFailures) got() []domain.Reason {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Reason(nil), f.reasons...)
}

func startCounting(t *testing.T, fa *fakeAuth, ff *fakeFailures, clk clock.Clock, log *slog.Logger) string {
	t.Helper()
	ap, _ := net.ListenPacket("udp", "127.0.0.1:0")
	cp, _ := net.ListenPacket("udp", "127.0.0.1:0")
	s := New(staticSecret("s3cret"), fa, &fakeAcct{}, &fakeLink{}, log, WithFailureRecorder(ff), WithClock(clk))
	s.Serve(ap, cp)
	t.Cleanup(func() { s.Shutdown(context.Background()) })
	return ap.LocalAddr().String()
}

// sendWire writes wire on conn and reads one reply.
func sendWire(t *testing.T, conn net.Conn, wire []byte) *radius.Packet {
	t.Helper()
	if _, err := conn.Write(wire); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("no reply: %v", err)
	}
	p, err := radius.Parse(buf[:n], []byte("s3cret"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRetransmittedRejectCountsOnce(t *testing.T) {
	fa := &fakeAuth{verdict: auth.Verdict{Reason: domain.ReasonInvalidCredentials}}
	ff := &fakeFailures{}
	fc := clock.NewFake(rt0)
	addr := startCounting(t, fa, ff, fc, slog.Default())
	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	p := pap("alice", "wrong")
	wire, _ := p.Encode()
	for range 3 { // the router repeats the same packet from the same port
		if r := sendWire(t, conn, wire); r.Code != radius.CodeAccessReject {
			t.Fatalf("code %v", r.Code)
		}
	}
	if n := len(ff.got()); n != 1 {
		t.Fatalf("counted %d times, want 1", n)
	}
	fc.Advance(61 * time.Second)
	sendWire(t, conn, wire)
	if n := len(ff.got()); n != 2 {
		t.Fatalf("after 60 s counted %d times, want 2", n)
	}
	// A new Identifier is a new request.
	p2 := pap("alice", "wrong")
	p2.Identifier = p.Identifier + 1
	wire2, _ := p2.Encode()
	sendWire(t, conn, wire2)
	if n := len(ff.got()); n != 3 {
		t.Fatalf("a new request counted %d times, want 3", n)
	}
}

// lockedBuffer is a log sink that the server goroutine and the test can use at the same time.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// E17.08 in unit form: a counter that fails never stops the reject; the error is logged.
func TestCounterFailureStillSendsReject(t *testing.T) {
	fa := &fakeAuth{verdict: auth.Verdict{Reason: domain.ReasonOutOfBulk}}
	ff := &fakeFailures{err: errors.New("database is locked")}
	logs := &lockedBuffer{}
	addr := startCounting(t, fa, ff, clock.NewFake(rt0), slog.New(slog.NewTextHandler(logs, nil)))
	reply, err := exchange(t, addr, pap("alice", "pw"), 2*time.Second)
	if err != nil || reply.Code != radius.CodeAccessReject {
		t.Fatalf("%v %v", reply, err)
	}
	if !strings.Contains(logs.String(), "failed-login counter not written") {
		t.Errorf("counter error not logged:\n%s", logs.String())
	}
}

// A request whose count could not be written is not remembered, so its retransmission is counted again.
func TestFailedCountIsRetriedOnRetransmission(t *testing.T) {
	fa := &fakeAuth{verdict: auth.Verdict{Reason: domain.ReasonInvalidCredentials}}
	ff := &fakeFailures{err: errors.New("database is locked")}
	addr := startCounting(t, fa, ff, clock.NewFake(rt0), slog.New(slog.NewTextHandler(&lockedBuffer{}, nil)))
	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	wire, _ := pap("alice", "wrong").Encode()
	for range 2 {
		if r := sendWire(t, conn, wire); r.Code != radius.CodeAccessReject {
			t.Fatalf("code %v", r.Code)
		}
	}
	if n := len(ff.got()); n != 2 {
		t.Fatalf("tried to count %d times, want 2 (the first write failed)", n)
	}
}

func TestAcceptIsNotCounted(t *testing.T) {
	fa := &fakeAuth{verdict: auth.Verdict{Grant: domain.Grant{InterimSeconds: 60, SessionTimeout: 99}}}
	ff := &fakeFailures{}
	addr := startCounting(t, fa, ff, clock.NewFake(rt0), slog.Default())
	if reply, err := exchange(t, addr, pap("alice", "pw"), 2*time.Second); err != nil || reply.Code != radius.CodeAccessAccept {
		t.Fatalf("%v %v", reply, err)
	}
	if n := len(ff.got()); n != 0 {
		t.Errorf("an Accept was counted %d times", n)
	}
}
