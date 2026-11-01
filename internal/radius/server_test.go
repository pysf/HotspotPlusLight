package radius

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"

	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/domain"
)

type staticSecret string

func (s staticSecret) RadiusSecret(context.Context) (string, error) { return string(s), nil }

// fakeAuth is a hand-written Authenticator.
type fakeAuth struct {
	mu      sync.Mutex
	verdict auth.Verdict
	err     error
	gotUser string
	gotPW   string
}

func (f *fakeAuth) Login(_ context.Context, u, p string) (auth.Verdict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotUser, f.gotPW = u, p
	return f.verdict, f.err
}

// fakeAcct is a hand-written Accountant.
type fakeAcct struct {
	mu   sync.Mutex
	recs []domain.AcctRecord
	err  error
}

func (f *fakeAcct) Account(_ context.Context, r domain.AcctRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recs = append(f.recs, r)
	return f.err
}

func (f *fakeAcct) got() []domain.AcctRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.AcctRecord(nil), f.recs...)
}

func startBoth(t *testing.T, a Authenticator, ac Accountant) (authAddr, acctAddr string) {
	t.Helper()
	ap, _ := net.ListenPacket("udp", "127.0.0.1:0")
	cp, _ := net.ListenPacket("udp", "127.0.0.1:0")
	s := New(staticSecret("s3cret"), a, ac, slog.Default())
	s.Serve(ap, cp)
	t.Cleanup(func() { s.Shutdown(context.Background()) })
	return ap.LocalAddr().String(), cp.LocalAddr().String()
}

func start(t *testing.T, a Authenticator) string {
	t.Helper()
	addr, _ := startBoth(t, a, &fakeAcct{})
	return addr
}

func acctPkt(typ rfc2866.AcctStatusType, sid string) *radius.Packet {
	p := radius.New(radius.CodeAccountingRequest, []byte("s3cret"))
	rfc2866.AcctStatusType_Set(p, typ)
	rfc2865.UserName_SetString(p, "Alice")
	if sid != "" {
		rfc2866.AcctSessionID_SetString(p, sid)
	}
	rfc2866.AcctInputOctets_Set(p, 10)
	rfc2869.AcctInputGigawords_Set(p, 2)
	rfc2866.AcctOutputOctets_Set(p, 20)
	rfc2866.AcctSessionTime_Set(p, 30)
	rfc2865.CallingStationID_SetString(p, "AA:BB:CC:DD:EE:FF")
	return p
}

func TestAccountingRecordAndReplyAfterAccount(t *testing.T) {
	fa := &fakeAcct{}
	_, addr := startBoth(t, &fakeAuth{}, fa)
	reply, err := exchange(t, addr, acctPkt(rfc2866.AcctStatusType_Value_InterimUpdate, "s1"), 2*time.Second)
	if err != nil || reply.Code != radius.CodeAccountingResponse {
		t.Fatalf("%v %v", reply, err)
	}
	recs := fa.got()
	if len(recs) != 1 {
		t.Fatalf("%d records", len(recs))
	}
	r := recs[0]
	want := domain.Counters{Download: 20, Upload: 2<<32 + 10, Seconds: 30}
	if r.Status != domain.AcctInterim || r.Username != "Alice" || r.SessionID != "s1" || r.NASIP != "127.0.0.1" || r.MAC != "AA:BB:CC:DD:EE:FF" || r.Cum != want {
		t.Fatalf("record %+v", r)
	}
}

func TestAccountingFailureMeansNoReply(t *testing.T) {
	_, addr := startBoth(t, &fakeAuth{}, &fakeAcct{err: errors.New("database is down")})
	if reply, err := exchange(t, addr, acctPkt(rfc2866.AcctStatusType_Value_Stop, "s1"), 500*time.Millisecond); err == nil {
		t.Fatalf("got a reply: %v", reply.Code)
	}
}

func TestAccountingOnOffAckedWithoutRecord(t *testing.T) {
	fa := &fakeAcct{}
	_, addr := startBoth(t, &fakeAuth{}, fa)
	for _, typ := range []rfc2866.AcctStatusType{rfc2866.AcctStatusType_Value_AccountingOn, rfc2866.AcctStatusType_Value_AccountingOff} {
		reply, err := exchange(t, addr, acctPkt(typ, ""), 2*time.Second)
		if err != nil || reply.Code != radius.CodeAccountingResponse {
			t.Fatalf("%v %v", reply, err)
		}
	}
	if len(fa.got()) != 0 {
		t.Fatalf("records %v", fa.got())
	}
}

func TestAccountingWithoutSessionIDDropped(t *testing.T) {
	fa := &fakeAcct{}
	_, addr := startBoth(t, &fakeAuth{}, fa)
	if _, err := exchange(t, addr, acctPkt(rfc2866.AcctStatusType_Value_Start, ""), 400*time.Millisecond); err == nil || len(fa.got()) != 0 {
		t.Fatal("a Start without Acct-Session-Id must be dropped")
	}
}

func exchange(t *testing.T, addr string, p *radius.Packet, wait time.Duration) (*radius.Packet, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	return radius.Exchange(ctx, p, addr)
}

func pap(user, pw string) *radius.Packet {
	p := radius.New(radius.CodeAccessRequest, []byte("s3cret"))
	rfc2865.UserName_SetString(p, user)
	rfc2865.UserPassword_SetString(p, pw)
	return p
}

func TestRejectCarriesCodeAndMessage(t *testing.T) {
	fa := &fakeAuth{verdict: auth.Verdict{Reason: domain.ReasonOutOfBulk}}
	reply, err := exchange(t, start(t, fa), pap("alice", "pw"), 2*time.Second)
	if err != nil || reply.Code != radius.CodeAccessReject {
		t.Fatalf("%v %v", reply, err)
	}
	if got := rfc2865.ReplyMessage_GetString(reply); got != "OUT_OF_BULK: Your data allowance is used up." {
		t.Fatalf("reply message %q", got)
	}
	fa.mu.Lock()
	defer fa.mu.Unlock()
	if fa.gotUser != "alice" || fa.gotPW != "pw" {
		t.Fatalf("authenticator got %q/%q", fa.gotUser, fa.gotPW)
	}
}

func TestAcceptAttributes(t *testing.T) {
	fa := &fakeAuth{verdict: auth.Verdict{Grant: domain.Grant{InterimSeconds: 60, SessionTimeout: 99, XmitLimit: 7, RateLimit: "1k/1k"}}}
	reply, err := exchange(t, start(t, fa), pap("alice", "pw"), 2*time.Second)
	if err != nil || reply.Code != radius.CodeAccessAccept {
		t.Fatalf("%v %v", reply, err)
	}
	if rfc2865.SessionTimeout_Get(reply) != 99 {
		t.Fatalf("session timeout %d", rfc2865.SessionTimeout_Get(reply))
	}
}

func TestInfrastructureErrorMeansNoReply(t *testing.T) {
	addr := start(t, &fakeAuth{err: errors.New("database is down")})
	if reply, err := exchange(t, addr, pap("alice", "pw"), 500*time.Millisecond); err == nil {
		t.Fatalf("got a reply: %v", reply.Code)
	}
}

func TestPlausiblePassword(t *testing.T) {
	for in, want := range map[string]bool{"": true, "abc123": true, "pässwörd": true, "bad\x01": false, "\xff\xfe": false, "tab\t": false} {
		if got := plausiblePassword([]byte(in)); got != want {
			t.Errorf("%q: %v", in, got)
		}
	}
}

func TestAccountingMalformedAttributeDroppedWithoutReply(t *testing.T) {
	fa := &fakeAcct{}
	_, addr := startBoth(t, &fakeAuth{}, fa)
	p := acctPkt(rfc2866.AcctStatusType_Value_InterimUpdate, "s1")
	p.Set(rfc2866.AcctInputOctets_Type, radius.Attribute{1, 2}) // not 4 bytes
	if reply, err := exchange(t, addr, p, 500*time.Millisecond); err == nil {
		t.Fatalf("got a reply: %v", reply.Code)
	}
	if n := len(fa.got()); n != 0 {
		t.Fatalf("%d records accounted", n)
	}
}
