// MONITOR: these counters are advisory and can be distorted by anyone on the LAN who can spoof UDP:
// an Access-Request is recorded before its authenticity is known (CHAP or malformed ones cannot be
// verified), the bad-secret list keeps only the 20 newest sources (spoofed sources can push the real
// router out), and the total counts retransmissions. In memory only, LAN scale.

// Package linkstatus keeps the router link status of PLAN 8.2 in memory: when the
// router last sent an Access-Request or an Accounting packet, how the last
// Disconnect went, and which sources sent packets signed with a wrong shared
// secret. Nothing is written to the database; a restart starts empty.
package linkstatus

import (
	"net"
	"slices"
	"sync"
	"time"

	"hotspotpluslight/internal/clock"
)

// maxBadSources bounds the memory a flood of spoofed sources can use; the oldest source is dropped first.
const maxBadSources = 20

// Event is one remembered packet or Disconnect.
type Event struct {
	At     time.Time
	From   string // source (or, for a Disconnect, destination) IP, no port
	Detail string // Disconnect only: ACK, NAK or no answer
}

// BadSource counts the bad-secret packets of one source IP.
type BadSource struct {
	From  string
	Count int
	Last  time.Time
}

// Snapshot is a copy of the current state. Zero Events (At.IsZero) mean "never".
type Snapshot struct {
	AccessRequest, Accounting, Disconnect Event
	BadSecretTotal                        int
	BadSecretSources                      []BadSource // newest first
}

// Recorder is safe for concurrent use.
type Recorder struct {
	clock clock.Clock
	mu    sync.Mutex
	snap  Snapshot
}

func New(c clock.Clock) *Recorder { return &Recorder{clock: c} }

// host strips the port from "ip:port"; a bare address is returned as is.
func host(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

func (r *Recorder) event(from, detail string) Event {
	return Event{At: r.clock.Now(), From: host(from), Detail: detail}
}

// AccessRequest records a verifiable Access-Request from addr.
func (r *Recorder) AccessRequest(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap.AccessRequest = r.event(addr, "")
}

// Accounting records an authentic Accounting-Request from addr.
func (r *Recorder) Accounting(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap.Accounting = r.event(addr, "")
}

// Disconnect records the outcome of a Disconnect-Request sent to addr.
func (r *Recorder) Disconnect(addr, outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap.Disconnect = r.event(addr, outcome)
}

// BadSecret records a packet from addr that was not signed with the shared secret.
func (r *Recorder) BadSecret(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ev := r.event(addr, "")
	r.snap.BadSecretTotal++
	list := r.snap.BadSecretSources
	i := slices.IndexFunc(list, func(b BadSource) bool { return b.From == ev.From })
	b := BadSource{From: ev.From, Last: ev.At}
	if i >= 0 {
		b.Count = list[i].Count
		list = slices.Delete(list, i, i+1)
	}
	b.Count++
	list = slices.Insert(list, 0, b)
	if len(list) > maxBadSources {
		list = list[:maxBadSources]
	}
	r.snap.BadSecretSources = list
}

// Snapshot returns a copy of the current state.
func (r *Recorder) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.snap
	s.BadSecretSources = slices.Clone(s.BadSecretSources)
	return s
}
