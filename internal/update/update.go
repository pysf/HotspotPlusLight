// Package update checks GitHub for a newer release (Checker) and installs it (Installer).
package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

// DefaultAPIURL is the GitHub API base. Tests and mirrors set HSP_UPDATE_API_URL instead.
const DefaultAPIURL = "https://api.github.com"

// latestPath is the release endpoint of this project, relative to the API base.
const latestPath = "/repos/pysf/HotspotPlusLight/releases/latest"

const (
	requestTimeout = 10 * time.Second
	maxBody        = 1 << 20 // a release answer is a few KB; this only stops a runaway answer
	firstDelay     = 30 * time.Second
	checkEvery     = time.Hour
)

// Repo is what the checker needs from storage (store.Store).
type Repo interface {
	GetUpdateState(ctx context.Context) (domain.UpdateState, error)
	SaveUpdateResult(ctx context.Context, u domain.UpdateState) error
}

// Status is what the pages show.
type Status struct {
	Current   string // the running version as built, for example "v0.9.0" or "dev"
	Latest    string // empty before the first good check
	URL       string // release notes page
	CheckedAt time.Time
	Error     string
	Enabled   bool // the "Check for updates" setting
	Dev       bool // a dev build never checks and never offers an update
	Available bool // Latest is newer than Current
}

// Checker asks GitHub for the latest release and keeps the answer.
type Checker struct {
	repo       Repo
	clock      clock.Clock
	log        *slog.Logger
	version    string
	apiURL     string
	client     *http.Client
	firstDelay time.Duration

	mu     sync.Mutex
	assets []string // file names of the release of the last good check; nil before one
}

// Option changes a Checker.
type Option func(*Checker)

// WithFirstDelay sets the wait before the first background check (default 30 s).
func WithFirstDelay(d time.Duration) Option { return func(c *Checker) { c.firstDelay = d } }

// New returns a Checker for the running version. An empty apiURL means DefaultAPIURL.
func New(repo Repo, c clock.Clock, log *slog.Logger, version, apiURL string, opts ...Option) *Checker {
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}
	ck := &Checker{repo: repo, clock: c, log: log, version: version, apiURL: strings.TrimRight(apiURL, "/"),
		client: &http.Client{Timeout: requestTimeout}, firstDelay: firstDelay}
	for _, o := range opts {
		o(ck)
	}
	return ck
}

// isDev is true for a build without a release version.
func (c *Checker) isDev() bool {
	_, ok := domain.ParseVersion(c.version)
	return !ok
}

// Status returns the stored result of the last check, without a network call.
func (c *Checker) Status(ctx context.Context) (Status, error) {
	u, err := c.repo.GetUpdateState(ctx)
	if err != nil {
		return Status{}, err
	}
	s := Status{Current: c.version, Latest: u.Latest, URL: u.URL, CheckedAt: u.CheckedAt, Error: u.Error, Enabled: u.Enabled, Dev: c.isDev()}
	s.Available = !s.Dev && u.Enabled && domain.IsNewerVersion(u.Latest, c.version)
	if v, ok := domain.ParseVersion(c.version); ok {
		s.Current = v.String()
	}
	return s, nil
}

// Check asks GitHub now and stores the answer. A failed call is stored as a friendly error text and is not
// returned as an error. With the setting off, or in a dev build, it makes no call and returns the stored status.
func (c *Checker) Check(ctx context.Context) (Status, error) {
	st, err := c.Status(ctx)
	if err != nil || !st.Enabled || st.Dev {
		return st, err
	}
	u := domain.UpdateState{Latest: st.Latest, URL: st.URL, CheckedAt: c.clock.Now()}
	rel, err := c.fetch(ctx)
	if err != nil {
		c.log.Warn("update check failed", "err", err)
		u.Error = friendly(err)
	} else {
		u.Latest, u.URL = rel.Tag, rel.URL
		c.mu.Lock()
		c.assets = append([]string{}, rel.Assets...)
		c.mu.Unlock()
	}
	if err := c.repo.SaveUpdateResult(ctx, u); err != nil {
		return st, err
	}
	return c.Status(ctx)
}

// Assets returns the file names of the release of the last good check since the start.
// The second value is false before such a check, because the names are not stored in the database.
func (c *Checker) Assets() ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.assets, c.assets != nil
}

// allowHTTP is true when the API address is not GitHub's, so a test or mirror may serve plain http.
func (c *Checker) allowHTTP() bool { return c.apiURL != DefaultAPIURL }

// errStatus is a non-200 answer from the server.
type errStatus int

func (e errStatus) Error() string { return fmt.Sprintf("the server answered %d", int(e)) }

// friendly turns a failure into a short text for the page.
func friendly(err error) string {
	var code errStatus
	switch {
	case errors.As(err, &code):
		return fmt.Sprintf("GitHub answered with an error (%d). Try again later.", int(code))
	case errors.Is(err, context.DeadlineExceeded):
		return "GitHub did not answer in time. Try again later."
	case errors.Is(err, errBadAnswer):
		return "GitHub sent an answer this version does not understand."
	}
	return "Could not reach GitHub. Check the internet connection of this computer."
}

var errBadAnswer = errors.New("bad answer")

// fetch reads the latest release. The request carries only the User-Agent with the version.
func (c *Checker) fetch(ctx context.Context) (domain.Release, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL+latestPath, nil)
	if err != nil {
		return domain.Release{}, err
	}
	req.Header.Set("User-Agent", "HotspotPlusLight/"+c.version)
	resp, err := c.client.Do(req)
	if err != nil {
		return domain.Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return domain.Release{}, errStatus(resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return domain.Release{}, err
	}
	rel, err := domain.ParseLatestRelease(body)
	if err != nil {
		return domain.Release{}, fmt.Errorf("%w: %v", errBadAnswer, err)
	}
	return rel, nil
}

// Run checks once after a short delay and then every hour, until ctx ends.
// The setting is read on every round, so a change applies without a restart.
func (c *Checker) Run(ctx context.Context) {
	timer := time.NewTimer(c.firstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if _, err := c.Check(ctx); err != nil && ctx.Err() == nil {
			c.log.Warn("update check could not be saved", "err", err)
		}
		timer.Reset(checkEvery)
	}
}
