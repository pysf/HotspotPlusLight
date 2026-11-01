package session

import (
	"context"
	"errors"
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
	pruned   time.Time
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

func (f *fakeDB) InAcctTx(_ context.Context, fn func(AcctTx) error) error {
	snapM := map[string]domain.Member{}
	for k, v := range f.members {
		snapM[k] = *v
	}
	snapS := append([]domain.Session(nil), f.sessions...)
	if err := fn(f); err != nil {
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
	if m.UsedDownload != 250 || m.MAC != "aabbccddeeff" || len(db.sessions) != 1 {
		t.Fatalf("member %+v sessions %d", m, len(db.sessions))
	}
	r := rec(domain.AcctInterim, "alice", 300)
	r.MAC = "11:22:33:44:55:66"
	s.Account(ctx, r)
	if db.members["alice"].MAC != "aabbccddeeff" {
		t.Fatal("MAC overwritten")
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
