package update

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

type memRepo struct{ u domain.UpdateState }

func (m *memRepo) GetUpdateState(context.Context) (domain.UpdateState, error) { return m.u, nil }
func (m *memRepo) SaveUpdateResult(_ context.Context, u domain.UpdateState) error {
	enabled := m.u.Enabled
	m.u = u
	m.u.Enabled = enabled
	return nil
}

func newChecker(repo *memRepo, version, url string, opts ...Option) *Checker {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(repo, clock.Real{}, log, version, url, opts...)
}

func TestCheckStoresAndSendsOnlyUserAgent(t *testing.T) {
	var ua, path atomic.Value
	var extra atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua.Store(r.UserAgent())
		path.Store(r.URL.Path)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.URL.RawQuery != "" {
			extra.Add(1)
		}
		w.Write([]byte(`{"tag_name":"v1.1.0","html_url":"https://x/r"}`))
	}))
	defer srv.Close()
	repo := &memRepo{u: domain.UpdateState{Enabled: true}}
	st, err := newChecker(repo, "v1.0.0", srv.URL).Check(context.Background())
	if err != nil || !st.Available || st.Latest != "v1.1.0" || st.URL != "https://x/r" || st.Error != "" || st.CheckedAt.IsZero() {
		t.Fatalf("status %+v, err %v", st, err)
	}
	if ua.Load() != "HotspotPlusLight/v1.0.0" || path.Load() != "/repos/pysf/HotspotPlusLight/releases/latest" || extra.Load() != 0 {
		t.Errorf("request: ua %v path %v extra %d", ua.Load(), path.Load(), extra.Load())
	}
}

func TestCheckErrorKeepsOldResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	repo := &memRepo{u: domain.UpdateState{Enabled: true, Latest: "v1.1.0", URL: "u"}}
	st, err := newChecker(repo, "v1.0.0", srv.URL).Check(context.Background())
	if err != nil || st.Error == "" || st.Latest != "v1.1.0" || !st.Available {
		t.Fatalf("status %+v, err %v", st, err)
	}
}

func TestNoCallWhenOffOrDev(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1) }))
	defer srv.Close()
	for _, c := range []struct {
		version string
		on      bool
	}{{"v1.0.0", false}, {"dev", true}, {"ci", true}} {
		repo := &memRepo{u: domain.UpdateState{Enabled: c.on, Latest: "v9.0.0"}}
		st, err := newChecker(repo, c.version, srv.URL).Check(context.Background())
		if err != nil || st.Available {
			t.Errorf("%+v: status %+v, err %v", c, st, err)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("%d calls, want 0", hits.Load())
	}
}

func TestRunChecksAfterDelay(t *testing.T) {
	hit := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit <- struct{}{}
		w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go newChecker(&memRepo{u: domain.UpdateState{Enabled: true}}, "v1.0.0", srv.URL, WithFirstDelay(10*time.Millisecond)).Run(ctx)
	select {
	case <-hit:
	case <-time.After(5 * time.Second):
		t.Fatal("no check")
	}
}
