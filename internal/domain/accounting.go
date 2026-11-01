package domain

import (
	"math"
	"time"
)

// AcctStatus is the RADIUS Acct-Status-Type (RFC 2866). The values 1, 2 and 3
// are also what the session.status column stores.
type AcctStatus int

const (
	AcctStart   AcctStatus = 1
	AcctStop    AcctStatus = 2
	AcctInterim AcctStatus = 3
	AcctOn      AcctStatus = 7
	AcctOff     AcctStatus = 8
)

// Counters are the three usage figures of BR-11, as cumulative values of one
// session or as amounts to add to a member.
type Counters struct {
	Download, Upload, Seconds int64
}

// Gigawords is the 2^32 that one Acct-*-Gigawords unit stands for (RFC 2869).
const gigaword = int64(1) << 32

// CumulativeFromAcct builds the cumulative counters of an accounting packet
// (BR-11): upload = Acct-Input-Octets (+ Input-Gigawords << 32), download =
// Acct-Output-Octets (+ Output-Gigawords << 32), seconds = Acct-Session-Time.
// VERIFY-ON-HARDWARE (PLAN 13.5/13.6): direction (Input = upload) and when
// RouterOS starts sending Gigawords.
func CumulativeFromAcct(inOctets, inGiga, outOctets, outGiga, sessionTime uint32) Counters {
	return Counters{Upload: withGiga(inOctets, inGiga), Download: withGiga(outOctets, outGiga), Seconds: int64(sessionTime)}
}

// withGiga saturates instead of overflowing int64 (only a bogus 8 EiB+ reading can).
func withGiga(octets, giga uint32) int64 {
	if giga >= 1<<31 {
		return math.MaxInt64
	}
	return int64(giga)*gigaword + int64(octets)
}

// MONITOR: a reordered/delayed Interim (cur < prev) is indistinguishable from a
// counter restart and is treated as one (E4.04): it over-counts and rewinds the
// stored counter. LAN UDP reordering is rare; accepted.
func fieldDelta(prev, cur int64) int64 {
	if cur < prev {
		return cur // the router's counter restarted (BR-11)
	}
	return cur - prev
}

// DeltaTo is what a new cumulative reading adds to the member: per field
// `cur - prev`, or `cur` when the counter went backwards (a restart). A
// retransmitted packet therefore adds nothing.
func (prev Counters) DeltaTo(cur Counters) Counters {
	return Counters{
		Download: fieldDelta(prev.Download, cur.Download),
		Upload:   fieldDelta(prev.Upload, cur.Upload),
		Seconds:  fieldDelta(prev.Seconds, cur.Seconds),
	}
}

// IsZero reports that nothing would be added.
func (c Counters) IsZero() bool { return c == Counters{} }

// Covers reports whether every field of c is at least the one of o.
func (c Counters) Covers(o Counters) bool {
	return c.Download >= o.Download && c.Upload >= o.Upload && c.Seconds >= o.Seconds
}

// Session is one router session (a row of the session table).
type Session struct {
	ID, MemberID                                  int64
	Username, AcctSessionID, NASIP, NASIdentifier string
	FramedIP, MAC                                 string
	Status                                        AcctStatus
	StartedAt, LastUpdateAt                       time.Time
	EndedAt                                       *time.Time
	TerminateCause                                string
	Cum                                           Counters // last cumulative values from the router
}

// AcctRecord is one Start/Interim/Stop packet reduced to what the rules need.
type AcctRecord struct {
	Status                              AcctStatus
	Username, SessionID                 string
	NASIP, NASIdentifier, FramedIP, MAC string // MAC as sent; normalized by the service (BR-3)
	TerminateCause                      string
	Cum                                 Counters
}

// AcctResult says what to store for one accounting record.
type AcctResult struct {
	Ignore  bool     // a late duplicate of a finished session: store nothing
	Insert  bool     // Session is new (ID 0); otherwise it replaces the stored row
	Closed  *Session // an open row of the same key that this Start replaced: store it as ended (no counter change)
	Delta   Counters // to add to the member's usage counters
	Session Session
}

// kickEnded reports whether r is the router's final Stop of a session the admin
// kicked (BR-18): the stored row is finished (EndedAt set) yet its status is not
// Stop, and r is a Stop whose figures are all >= the stored ones. Only then does
// r complete this same row, so the usage is counted once and the row keeps its
// Admin-Reset cause. Anything else (an Interim, a Stop with a counter reset
// because the router reused the Acct-Session-Id) follows the normal path.
func kickEnded(s *Session, r AcctRecord) bool {
	return s != nil && s.EndedAt != nil && s.Status != AcctStop && r.Status == AcctStop && r.Cum.Covers(s.Cum)
}

// ApplyAccounting is the pure core of PLAN 7.3 + BR-11. latest is the stored
// session for the identity key (username, Acct-Session-Id, NAS address), nil if
// none; now is the server receive time (O19).
//
//   - Start: reuse an open row that has not counted any time yet (a
//     retransmission), else a new row (a reused Acct-Session-Id after a Stop
//     is a new session).
//     If the open row has already counted time, the router reused the id after
//     a reboot: that row is returned in Closed (ended, cause Lost-Service).
//   - Interim / Stop: update the open row; with none, create one starting
//     `Acct-Session-Time` ago (the server was down, or a Start was lost) -
//     unless the latest row is already finished and covers these figures, which
//     is a retransmitted Stop or a delayed Interim: ignored.
func ApplyAccounting(latest *Session, r AcctRecord, now time.Time) AcctResult {
	open := latest != nil && latest.EndedAt == nil
	var s Session
	var closed *Session
	switch {
	case r.Status == AcctStart && open && latest.Cum.Seconds == 0:
		s = *latest
	case r.Status == AcctStart:
		if open { // the router reused the id (reboot): the old row is dead, or it would stay online and count toward max_sessions (BR-9)
			c := *latest
			end := now
			c.EndedAt, c.TerminateCause, c.Status, c.LastUpdateAt = &end, "Lost-Service", AcctStop, now
			closed = &c
		}
		s = Session{StartedAt: now}
	case open, kickEnded(latest, r):
		s = *latest
	case latest != nil && latest.Cum.Covers(r.Cum):
		return AcctResult{Ignore: true}
	default:
		// MONITOR: a late Interim/Stop with no open row and figures above the latest finished row
		// creates a new row and double-bills the overlap. Needed for the server-downtime case (E4.17).
		s = Session{StartedAt: now.Add(-time.Duration(r.Cum.Seconds) * time.Second)}
	}
	res := AcctResult{Insert: s.ID == 0, Delta: s.Cum.DeltaTo(r.Cum), Closed: closed}
	s.Username, s.AcctSessionID, s.NASIP = r.Username, r.SessionID, r.NASIP
	if r.NASIdentifier != "" {
		s.NASIdentifier = r.NASIdentifier
	}
	if r.FramedIP != "" {
		s.FramedIP = r.FramedIP
	}
	if r.MAC != "" {
		s.MAC = r.MAC
	}
	s.Status, s.Cum, s.LastUpdateAt = r.Status, r.Cum, now
	if r.Status == AcctStop && s.EndedAt == nil {
		end := now
		s.EndedAt, s.TerminateCause = &end, r.TerminateCause
	}
	res.Session = s
	return res
}
