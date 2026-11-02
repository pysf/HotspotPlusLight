package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

var t0 = time.Unix(1_700_000_000, 0).UTC()

// fakeDB is an in-memory Repo + AcctTx; InAcctTx rolls back by working on a copy.
type fakeDB struct {
	members  map[string]*domain.Member
	sessions []domain.Session
	retDays  int
	endErr   error // EndSession fails with it
	pruned   time.Time
	daily    map[string]domain.Counters // "id/day" -> sum
}

func newDB() *fakeDB {
	return &fakeDB{members: map[string]*domain.Member{"alice": {ID: 1, Username: "alice"}}}
}

func (f *fakeDB) CountOpenSessions(context.Context) (int, error)    { return len(f.sessions), nil }
func (f *fakeDB) SessionRetentionDays(context.Context) (int, error) { return f.retDays, nil }
func (f *fakeDB) PruneSessions(_ context.Context, c time.Time) (int, error) {
	f.pruned = c
	return 3, nil
}

func (f *fakeDB) GetSettings(context.Context) (domain.Settings, error) {
	return domain.Settings{Timezone: "UTC", AccountingIntervalSeconds: 60}, nil
}

// online mirrors the SQL predicate: not ended and updated since the cut-off.
func (f *fakeDB) online(since time.Time) []domain.Session {
	var out []domain.Session
	for _, s := range f.sessions {
		if s.EndedAt == nil && !s.LastUpdateAt.Before(since) {
			out = append(out, s)
		}
	}
	return out
}
func (f *fakeDB) CountOnlineSessions(_ context.Context, since time.Time) (int, error) {
	return len(f.online(since)), nil
}
func (f *fakeDB) ListOnlineSessions(_ context.Context, since time.Time, limit, offset int) ([]domain.Session, error) {
	all := f.online(since)
	if offset >= len(all) {
		return nil, nil
	}
	return all[offset:min(offset+limit, len(all))], nil
}
func (f *fakeDB) SessionTargetByID(_ context.Context, id int64) (domain.SessionTarget, bool, error) {
	if id < 1 || int(id) > len(f.sessions) {
		return domain.SessionTarget{}, false, domain.ErrNotFound
	}
	s := f.sessions[id-1]
	return domain.SessionTarget{ID: id, Username: s.Username, AcctSessionID: s.AcctSessionID, NASIP: s.NASIP}, s.EndedAt == nil, nil
}
func (f *fakeDB) EndSession(_ context.Context, id int64, now time.Time, cause string) error {
	if f.endErr != nil {
		return f.endErr
	}
	f.sessions[id-1].EndedAt, f.sessions[id-1].TerminateCause = &now, cause
	return nil
}

func (f *fakeDB) InAcctTx(_ context.Context, fn func(AcctTx) error) error {
	snapM := map[string]domain.Member{}
	for k, v := range f.members {
		snapM[k] = *v
	}
	snapS := append([]domain.Session(nil), f.sessions...)
	snapD := maps.Clone(f.daily)
	if err := fn(f); err != nil {
		f.daily = snapD
		f.members = map[string]*domain.Member{}
		for k, v := range snapM {
			v := v
			f.members[k] = &v
		}
		f.sessions = snapS
		return err
	}
	return nil
}

func (f *fakeDB) MemberByUsername(_ context.Context, u string) (domain.Member, error) {
	if m, ok := f.members[u]; ok {
		return *m, nil
	}
	return domain.Member{}, domain.ErrNotFound
}
func (f *fakeDB) SetMemberMACIfEmpty(_ context.Context, id int64, mac string, _ time.Time) error {
	for _, m := range f.members {
		if m.ID == id && m.MAC == "" {
			m.MAC = mac
		}
	}
	return nil
}
func (f *fakeDB) LatestSession(_ context.Context, u, sid, ip string) (*domain.Session, error) {
	for i := len(f.sessions) - 1; i >= 0; i-- {
		if s := f.sessions[i]; s.Username == u && s.AcctSessionID == sid && s.NASIP == ip {
			return &s, nil
		}
	}
	return nil, nil
}
func (f *fakeDB) InsertSession(_ context.Context, s domain.Session) error {
	s.ID = int64(len(f.sessions) + 1)
	f.sessions = append(f.sessions, s)
	return nil
}
func (f *fakeDB) UpdateSession(_ context.Context, s domain.Session) error {
	f.sessions[s.ID-1] = s
	return nil
}
func (f *fakeDB) AddUsage(_ context.Context, id int64, d domain.Counters, _ time.Time) error {
	for _, m := range f.members {
		if m.ID == id {
			m.UsedDownload += d.Download
			m.UsedUpload += d.Upload
			m.UsedSeconds += d.Seconds
		}
	}
	return nil
}

func (f *fakeDB) AddDailyUsage(_ context.Context, id int64, day string, d domain.Counters) error {
	if f.daily == nil {
		f.daily = map[string]domain.Counters{}
	}
	k := fmt.Sprintf("%d/%s", id, day)
	c := f.daily[k]
	c.Download, c.Upload, c.Seconds = c.Download+d.Download, c.Upload+d.Upload, c.Seconds+d.Seconds
	f.daily[k] = c
	return nil
}

// FakeDisconnector records targets; reusable by later phases' tests.
type FakeDisconnector struct {
	Calls []Target
	Err   error
}

func (f *FakeDisconnector) Disconnect(_ context.Context, t Target) error {
	f.Calls = append(f.Calls, t)
	return f.Err
}

func rec(st domain.AcctStatus, user string, dl int64) domain.AcctRecord {
	return domain.AcctRecord{Status: st, Username: user, SessionID: "s", NASIP: "1.2.3.4", MAC: "AA:BB:CC:DD:EE:FF", Cum: domain.Counters{Download: dl}}
}

func TestOpenCount(t *testing.T) {
	db := newDB()
	db.sessions = []domain.Session{{}, {}}
	s := New(db, &FakeDisconnector{}, clock.NewFake(t0))
	if n, err := s.OpenCount(context.Background()); err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
}

func TestAccountAppliesDeltasAndCapturesMACOnce(t *testing.T) {
	db := newDB()
	s := New(db, &FakeDisconnector{}, clock.NewFake(t0))
	ctx := context.Background()
	for _, r := range []domain.AcctRecord{rec(domain.AcctStart, "alice", 0), rec(domain.AcctInterim, "alice", 100), rec(domain.AcctInterim, "alice", 100), rec(domain.AcctInterim, "alice", 250)} {
		if err := s.Account(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	m := db.members["alice"]
	if m.UsedDownload != 250 || m.MAC != "AA:BB:CC:DD:EE:FF" || len(db.sessions) != 1 {
		t.Fatalf("member %+v sessions %d", m, len(db.sessions))
	}
	r := rec(domain.AcctInterim, "alice", 300)
	r.MAC = "11:22:33:44:55:66"
	s.Account(ctx, r)
	if db.members["alice"].MAC != "AA:BB:CC:DD:EE:FF" {
		t.Fatal("MAC overwritten")
	}
}

// The day row gets the same deltas as the member, under the date in settings.timezone (D12).
func TestAccountFeedsDayRowAndRollsItBack(t *testing.T) {
	db := newDB()
	s := New(db, &FakeDisconnector{}, clock.NewFake(t0))
	ctx := context.Background()
	for _, r := range []domain.AcctRecord{rec(domain.AcctStart, "alice", 0), rec(domain.AcctInterim, "alice", 100), rec(domain.AcctInterim, "alice", 100), rec(domain.AcctInterim, "alice", 250)} {
		if err := s.Account(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	key := "1/" + t0.Format(domain.DayFormat)
	if got := db.daily[key]; got.Download != 250 || len(db.daily) != 1 {
		t.Fatalf("daily %+v", db.daily)
	}
	boom := errors.New("crash")
	s2 := New(db, &FakeDisconnector{}, clock.NewFake(t0), WithBeforeCommit(func() error { return boom }))
	if err := s2.Account(ctx, rec(domain.AcctInterim, "alice", 900)); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if db.daily[key].Download != 250 {
		t.Fatalf("daily not rolled back: %+v", db.daily)
	}
}

func TestAccountUnknownUserAndOtherTypesChangeNothing(t *testing.T) {
	db := newDB()
	s := New(db, &FakeDisconnector{}, clock.NewFake(t0))
	for _, r := range []domain.AcctRecord{rec(domain.AcctStart, "ghost", 0), rec(domain.AcctOn, "alice", 5), rec(domain.AcctOff, "alice", 5)} {
		if err := s.Account(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	if len(db.sessions) != 0 || db.members["alice"].UsedDownload != 0 {
		t.Fatal("changed state")
	}
}

func TestAccountBeforeCommitErrorRollsBack(t *testing.T) {
	db := newDB()
	boom := errors.New("crash")
	s := New(db, &FakeDisconnector{}, clock.NewFake(t0), WithBeforeCommit(func() error { return boom }))
	if err := s.Account(context.Background(), rec(domain.AcctInterim, "alice", 100)); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
	if len(db.sessions) != 0 || db.members["alice"].UsedDownload != 0 {
		t.Fatal("not rolled back")
	}
}

func TestPrune(t *testing.T) {
	db := newDB()
	s := New(db, &FakeDisconnector{}, clock.NewFake(t0))
	if n, _ := s.Prune(context.Background()); n != 0 || !db.pruned.IsZero() {
		t.Fatal("retention 0 must keep everything")
	}
	db.retDays = 90
	if n, _ := s.Prune(context.Background()); n != 3 || !db.pruned.Equal(t0.Add(-90*24*time.Hour)) {
		t.Fatalf("cutoff %v", db.pruned)
	}
}

func openRows(db *fakeDB, now time.Time, ages ...time.Duration) {
	for i, a := range ages {
		db.sessions = append(db.sessions, domain.Session{ID: int64(i + 1), Username: "alice", AcctSessionID: "s", NASIP: "1.2.3.4", StartedAt: now.Add(-time.Hour), LastUpdateAt: now.Add(-a)})
	}
}

func TestOnlineListsOnlyActiveAndPages(t *testing.T) {
	db := newDB()
	// interval 60 s: active while updated within 120 s; the third is stale.
	openRows(db, t0, 0, 119*time.Second, 121*time.Second)
	ended := t0
	db.sessions[0].EndedAt = &ended
	s := New(db, &FakeDisconnector{}, clock.NewFake(t0))
	v, err := s.Online(context.Background(), 1, 20)
	if err != nil || v.Total != 1 || len(v.Rows) != 1 || v.Rows[0].ID != 2 || v.IntervalSeconds != 60 {
		t.Fatalf("%+v %v", v, err)
	}
	if v, _ := s.Online(context.Background(), 9, 3); v.Page != 1 || v.Per != 20 {
		t.Fatalf("paging not normalized: %+v", v)
	}
}

func TestDisconnectEndsSessionOnlyOnAck(t *testing.T) {
	db := newDB()
	openRows(db, t0, 0)
	disc := &FakeDisconnector{}
	s := New(db, disc, clock.NewFake(t0))
	ctx := context.Background()
	disc.Err = domain.ErrKickRefused
	if err := s.Kick(ctx, 1); !errors.Is(err, domain.ErrKickRefused) || db.sessions[0].EndedAt != nil {
		t.Fatalf("NAK: err %v ended %v", err, db.sessions[0].EndedAt)
	}
	disc.Err = nil
	if err := s.Kick(ctx, 1); err != nil || db.sessions[0].EndedAt == nil || db.sessions[0].TerminateCause != KickCause {
		t.Fatalf("ACK: err %v session %+v", err, db.sessions[0])
	}
	if err := s.Kick(ctx, 1); !errors.Is(err, domain.ErrSessionEnded) {
		t.Fatalf("again: %v", err)
	}
	if err := s.Kick(ctx, 7); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if len(disc.Calls) != 2 {
		t.Fatalf("%d Disconnects sent, want 2 (none for ended/unknown)", len(disc.Calls))
	}
}

// After a Disconnect-ACK the router did disconnect the user: a database failure
// while marking the row must not turn into an error (a retry would send a second
// Disconnect); it is logged and the router's Stop closes the row later.
func TestDisconnectAckThenStoreFailureIsSuccess(t *testing.T) {
	db := newDB()
	openRows(db, t0, 0)
	db.endErr = errors.New("database is locked")
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	disc := &FakeDisconnector{}
	s := New(db, disc, clock.NewFake(t0), WithLogger(log))
	if err := s.Kick(context.Background(), 1); err != nil {
		t.Fatalf("ACK + store failure must succeed, got %v", err)
	}
	if !strings.Contains(buf.String(), "level=ERROR") || !strings.Contains(buf.String(), "database is locked") {
		t.Fatalf("store failure not logged at ERROR: %q", buf.String())
	}
	if len(disc.Calls) != 1 {
		t.Fatalf("%d Disconnects", len(disc.Calls))
	}
}
