// Package pageapi is the public listener's handler: the JSON API v1 used by the
// router-hosted login page (PLAN 8.1) and /healthz. Errors are RFC 9457
// problem+json with a stable `code` (O20).
package pageapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

// Consumer-side interfaces: implemented by auth.Evaluator and settings.Service.
type Accounts interface {
	// Account returns auth.ErrInvalidCredentials for a wrong username or password.
	Account(ctx context.Context, username, password string) (auth.Account, error)
	CredentialsByMAC(ctx context.Context, mac string) (username, password string, found bool, err error)
}

type SettingsReader interface {
	Get(ctx context.Context) (domain.Settings, error)
}

type Deps struct {
	Accounts Accounts
	Settings SettingsReader
	Clock    clock.Clock
	Log      *slog.Logger
}

// Codes of the problems that are not login rejections (those come from domain.Reason).
const (
	codeNotFound         = "NOT_FOUND"
	codeMethodNotAllowed = "METHOD_NOT_ALLOWED"
	codeDisabled         = "DISABLED"
	codeDeviceNotFound   = "DEVICE_NOT_FOUND"
)

const (
	// A client IP that collects maxFailures failed credential checks (401 on
	// /account) within failureWindow is refused for failureWindow (PLAN 8.1).
	// Unknown-MAC lookups do not count: new devices are normal.
	maxFailures   = 10
	failureWindow = time.Minute
)

type handlers struct {
	Deps
	throttle *auth.Throttle
}

// Handler builds the public listener's router: /healthz and /api/v1/*. Everything else is a problem+json 404.
// MONITOR: the throttle is per source IP and in memory (a restart forgives); clients behind one NAT
// share it. VERIFY-ON-HARDWARE: whether the router NATs hotspot clients towards the server (then every
// client shares one source IP and one bad guesser locks everybody out for a minute).
func Handler(d Deps) http.Handler {
	h := handlers{Deps: d, throttle: auth.NewThrottle(maxFailures, failureWindow)}
	r := chi.NewRouter()
	r.Use(cors)
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		problem(w, http.StatusNotFound, codeNotFound, "No such resource.")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", "GET, OPTIONS")
		problem(w, http.StatusMethodNotAllowed, codeMethodNotAllowed, "This resource only supports GET.")
	})
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n"))
	})
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/config", h.config)
		r.Group(func(r chi.Router) {
			r.Use(noStore)
			r.Get("/account", h.account)
			r.Get("/devices/{mac}/credentials", h.credentials)
		})
	})
	return r
}

// cors allows any origin (PLAN 8.1, D9): there are no cookies, credentials travel in the Authorization header.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Access-Control-Allow-Origin", "*")
		hd.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		hd.Set("Access-Control-Allow-Headers", "Authorization")
		if r.Method == http.MethodOptions {
			hd.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

type problemBody struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
	Code   string `json:"code"`
}

// problem writes an RFC 9457 problem document.
func problem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(problemBody{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail, Code: code})
}

func reasonProblem(w http.ResponseWriter, status int, r domain.Reason) {
	problem(w, status, string(r), r.Message())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (h handlers) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.Log.Error("page API request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	reasonProblem(w, http.StatusInternalServerError, domain.ReasonGeneralError)
}

func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// locked answers 429 and reports true while the client is locked out.
func (h handlers) locked(w http.ResponseWriter, r *http.Request) bool {
	wait := h.throttle.RetryAfter(clientKey(r), h.Clock.Now())
	if wait <= 0 {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	reasonProblem(w, http.StatusTooManyRequests, domain.ReasonTooManyAttempts)
	return true
}

func (h handlers) failed(r *http.Request) { h.throttle.Failure(clientKey(r), h.Clock.Now()) }

type configBody struct {
	SiteTitle     string `json:"siteTitle"`
	AfterLoginURL string `json:"afterLoginUrl"`
}

// MONITOR: /config is not throttled (a cheap settings read, no secrets).
func (h handlers) config(w http.ResponseWriter, r *http.Request) {
	st, err := h.Settings.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	writeJSON(w, configBody{SiteTitle: st.SiteTitle, AfterLoginURL: st.AfterLoginURL})
}

// basicCredentials reads the Authorization: Basic header. Anything malformed is "no credentials".
func basicCredentials(r *http.Request) (user, pass string, ok bool) {
	scheme, value, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Basic") {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return "", "", false
	}
	return strings.Cut(string(raw), ":")
}

type planBody struct {
	Name         string  `json:"name"`
	SpeedKbps    float64 `json:"speedKbps"`
	DurationDays int     `json:"durationDays"`
	MaxSessions  int     `json:"maxSessions"`
}

// dataBody and timeBody carry null allowed/remaining figures when unlimited.
type dataBody struct {
	Unlimited         bool   `json:"unlimited"`
	AllowedBytes      *int64 `json:"allowedBytes"`
	UsedDownloadBytes int64  `json:"usedDownloadBytes"`
	UsedUploadBytes   int64  `json:"usedUploadBytes"`
	RemainingBytes    *int64 `json:"remainingBytes"`
}

type timeBody struct {
	Unlimited        bool   `json:"unlimited"`
	AllowedSeconds   *int64 `json:"allowedSeconds"`
	UsedSeconds      int64  `json:"usedSeconds"`
	RemainingSeconds *int64 `json:"remainingSeconds"`
}

type accountBody struct {
	Plan         *planBody `json:"plan"`
	Data         *dataBody `json:"data"`
	Time         *timeBody `json:"time"`
	ValidUntil   *string   `json:"validUntil"`
	ExpiresAt    *string   `json:"expiresAt"`
	LoginAllowed bool      `json:"loginAllowed"`
	Reason       *string   `json:"reason"`
	Message      *string   `json:"message"`
}

func limited(unlimited bool, v int64) *int64 {
	if unlimited {
		return nil
	}
	return &v
}

func rfc3339(t time.Time) *string {
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func accountResponse(a auth.Account) accountBody {
	b := accountBody{LoginAllowed: a.Reason == ""}
	if a.Reason != "" {
		code, msg := string(a.Reason), a.Reason.Message()
		b.Reason, b.Message = &code, &msg
	}
	if a.State.ExpiresAt != nil {
		b.ExpiresAt = rfc3339(*a.State.ExpiresAt)
	}
	if a.Plan == nil { // NO_PLAN / PLAN_REMOVED: nothing to report about a plan
		return b
	}
	kbps, _ := domain.SpeedKbps(a.Plan.SpeedValue, a.Plan.SpeedUnit)
	q := a.Quota
	b.Plan = &planBody{Name: a.Plan.Name, SpeedKbps: kbps, DurationDays: a.Plan.DurationDays, MaxSessions: a.Plan.MaxSessions}
	b.Data = &dataBody{Unlimited: q.DataUnlimited, AllowedBytes: limited(q.DataUnlimited, q.AllowedBytes),
		UsedDownloadBytes: a.State.UsedDownload, UsedUploadBytes: a.State.UsedUpload, RemainingBytes: limited(q.DataUnlimited, q.RemainingBytes)}
	b.Time = &timeBody{Unlimited: q.TimeUnlimited, AllowedSeconds: limited(q.TimeUnlimited, q.AllowedSeconds),
		UsedSeconds: q.UsedSeconds, RemainingSeconds: limited(q.TimeUnlimited, q.RemainingSecs)}
	b.ValidUntil = rfc3339(q.WindowEnd)
	return b
}

// account is GET /api/v1/account: the credentials check, the balance and whether a login would be accepted.
// MONITOR: like the RADIUS edge there is no per-username brute-force protection; only the per-IP throttle
// (10 guesses/min/IP, in memory), so a 6-digit voucher password (O14) is guessable by a patient attacker.
// MONITOR: the answer is a prediction: loginAllowed=true for a lapsed auto-resubscribe member can still be
// rejected by the real login (lost renewal race, NO_MORE_SESSION).
func (h handlers) account(w http.ResponseWriter, r *http.Request) {
	defer h.throttle.Lock(clientKey(r))() // check, verify and record are atomic per source IP, or parallel guesses beat the limit
	if h.locked(w, r) {
		return
	}
	user, pass, ok := basicCredentials(r)
	if !ok {
		h.failed(r)
		reasonProblem(w, http.StatusUnauthorized, domain.ReasonInvalidCredentials)
		return
	}
	a, err := h.Accounts.Account(r.Context(), user, pass)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		h.failed(r)
		reasonProblem(w, http.StatusUnauthorized, domain.ReasonInvalidCredentials)
	case err != nil:
		h.serverError(w, r, err)
	default:
		writeJSON(w, accountResponse(a))
	}
}

// credentials is GET /api/v1/devices/{mac}/credentials: MAC auto-login (O8), only when the setting is on.
// MONITOR: unauthenticated, plaintext password, CORS "*": with O8 on, anyone on the LAN who knows or
// spoofs a registered MAC (or any web page opened on such a client) can read that member's credentials.
func (h handlers) credentials(w http.ResponseWriter, r *http.Request) {
	if h.locked(w, r) {
		return
	}
	st, err := h.Settings.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if !st.MacAutoLogin {
		problem(w, http.StatusForbidden, codeDisabled, "MAC auto-login is switched off.")
		return
	}
	mac, err := url.PathUnescape(chi.URLParam(r, "mac"))
	if err != nil {
		mac = ""
	}
	user, pass, found, err := h.Accounts.CredentialsByMAC(r.Context(), mac)
	switch {
	case err != nil:
		h.serverError(w, r, err)
	case !found:
		// Not a throttle failure: unregistered devices are normal (O8). MONITOR: the lookup can enumerate
		// registered MACs; the MAC is the only secret of O8 (see open-questions).
		problem(w, http.StatusNotFound, codeDeviceNotFound, "No account is registered for this device.")
	default:
		writeJSON(w, struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}{user, pass})
	}
}
