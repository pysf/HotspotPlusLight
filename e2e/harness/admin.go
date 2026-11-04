package harness

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/crypto/bcrypt"
)

// AdminClient is a browser stand-in for the admin listener: cookie jar, no
// automatic redirects (tests assert the statuses), CSRF taken from the page
// the way htmx does (the hx-headers attribute on <body>).
type AdminClient struct {
	t    *testing.T
	Base string
	HTTP *http.Client
	CSRF string
}

// Resp is a fully read response.
type Resp struct {
	*http.Response
	Body string
}

// Doc parses the body as HTML.
func (r Resp) Doc(t *testing.T) *goquery.Document {
	t.Helper()
	d, err := goquery.NewDocumentFromReader(strings.NewReader(r.Body))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// NewAdminClient returns an anonymous client for the server's admin listener.
func NewAdminClient(t *testing.T, s *Server) *AdminClient {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &AdminClient{t: t, Base: "http://" + s.Addrs.Admin, CSRF: "",
		HTTP: &http.Client{Jar: jar, Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Do sends a request. csrf is the X-CSRF-Token value ("" = header omitted).
func (c *AdminClient) Do(method, path string, form url.Values, csrf string) Resp {
	c.t.Helper()
	return c.DoHX(method, path, form, csrf, "")
}

// DoHX is Do plus the HX-Target header htmx sends ("" = omitted): the list region
// or the sheet body asks for a fragment instead of the full page.
func (c *AdminClient) DoHX(method, path string, form url.Values, csrf, target string) Resp {
	c.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, c.Base+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	req.Header.Set("HX-Request", "true")
	if target != "" {
		req.Header.Set("HX-Target", target)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return Resp{resp, string(b)}
}

// Get is a plain GET.
func (c *AdminClient) Get(path string) Resp { c.t.Helper(); return c.Do("GET", path, nil, "") }

// GetHX is a GET sent with the given HX-Target.
func (c *AdminClient) GetHX(path, target string) Resp {
	c.t.Helper()
	return c.DoHX("GET", path, nil, "", target)
}

// SendHX is Send with the given HX-Target.
func (c *AdminClient) SendHX(method, path string, form url.Values, target string) Resp {
	c.t.Helper()
	return c.DoHX(method, path, form, c.CSRF, target)
}

// Send performs a non-GET request with the client's current CSRF token.
func (c *AdminClient) Send(method, path string, form url.Values) Resp {
	c.t.Helper()
	return c.Do(method, path, form, c.CSRF)
}

// PostLogin submits the login form without touching CSRF state.
func (c *AdminClient) PostLogin(username, password string) Resp {
	c.t.Helper()
	return c.Do("POST", "/admin/login", url.Values{"username": {username}, "password": {password}}, "")
}

// Login logs in and loads the CSRF token from the landing page: the members page, or
// the password page while the first-start password is still in use. Any other outcome
// fails the test.
func (c *AdminClient) Login(username, password string) {
	c.t.Helper()
	r := c.PostLogin(username, password)
	loc := r.Header.Get("Location")
	if r.StatusCode != http.StatusSeeOther || (loc != "/admin/members" && loc != "/admin/password") {
		c.t.Fatalf("login: status %d location %q", r.StatusCode, loc)
	}
	c.loadCSRF(loc)
}

// RefreshCSRF re-reads the CSRF token from an authenticated page.
func (c *AdminClient) RefreshCSRF() {
	c.t.Helper()
	c.loadCSRF("/admin/members")
}

// RefreshCSRFFrom reads the CSRF token from the given authenticated page.
func (c *AdminClient) RefreshCSRFFrom(path string) { c.t.Helper(); c.loadCSRF(path) }

func (c *AdminClient) loadCSRF(path string) {
	c.t.Helper()
	r := c.Get(path)
	if r.StatusCode != 200 {
		c.t.Fatalf("%s: %d", path, r.StatusCode)
	}
	c.CSRF = CSRFFromDoc(c.t, r.Doc(c.t))
}

// CSRFFromDoc extracts the token the way htmx would send it.
func CSRFFromDoc(t *testing.T, d *goquery.Document) string {
	t.Helper()
	raw, ok := d.Find("body").Attr("hx-headers")
	if !ok {
		t.Fatal("body has no hx-headers")
	}
	var h map[string]string
	if err := json.Unmarshal([]byte(raw), &h); err != nil || h["X-CSRF-Token"] == "" {
		t.Fatalf("hx-headers %q: %v", raw, err)
	}
	return h["X-CSRF-Token"]
}

// SessionCookie returns the hsp_admin cookie value held by the client ("" if none).
func (c *AdminClient) SessionCookie() string {
	u, _ := url.Parse(c.Base)
	for _, ck := range c.HTTP.Jar.Cookies(u) {
		if ck.Name == "hsp_admin" {
			return ck.Value
		}
	}
	return ""
}

// AdminPassword is the password LoginAsAdmin sets at the first forced change.
const AdminPassword = "Test-Admin-Pass-42"

// SetPassword sends the password page form (current, new, confirm) as an htmx request.
func (c *AdminClient) SetPassword(current, next, confirm string) Resp {
	c.t.Helper()
	return c.Send("PUT", "/admin/password", url.Values{"current_password": {current}, "new_password": {next}, "confirm_password": {confirm}})
}

var (
	adminHashOnce sync.Once
	adminHash     string
)

// LoginAsAdmin returns a logged-in client. A server that still has the first-start
// password (admin/admin) gets AdminPassword written straight into the admin row (flag
// cleared), so tests do not pay for the forced-change flow. The bcrypt hash is made once
// per process. Tests of the forced flow log in by hand with admin/admin instead.
func LoginAsAdmin(t *testing.T, s *Server) *AdminClient {
	t.Helper()
	c := NewAdminClient(t, s)
	db := DB(t, s.DataDir)
	var must int
	if err := db.QueryRow(`SELECT must_change_password FROM admin WHERE id=1`).Scan(&must); err != nil {
		t.Fatal(err)
	}
	if must != 0 {
		adminHashOnce.Do(func() {
			h, err := bcrypt.GenerateFromPassword([]byte(AdminPassword), bcrypt.DefaultCost)
			if err != nil {
				panic(err)
			}
			adminHash = string(h)
		})
		if _, err := db.Exec(`UPDATE admin SET password_hash=?, must_change_password=0 WHERE id=1`, adminHash); err != nil {
			t.Fatal(err)
		}
	}
	c.Login("admin", AdminPassword)
	return c
}

// PlanForm returns valid plan form values; override entries as needed.
func PlanForm(name string, override ...string) url.Values {
	v := url.Values{
		"name": {name}, "duration_days": {"30"},
		"speed_value": {"2"}, "speed_unit": {"Mbps"},
		"bulk_value": {"10"}, "bulk_unit": {"GB"},
		"time_limit_minutes": {"0"}, "max_sessions": {"1"},
	}
	for i := 0; i+1 < len(override); i += 2 {
		if override[i+1] == "-" {
			v.Del(override[i])
		} else {
			v.Set(override[i], override[i+1])
		}
	}
	return v
}

// CreatePlan creates a plan through the admin UI and returns its id.
func (c *AdminClient) CreatePlan(form url.Values) int64 {
	c.t.Helper()
	r := c.Send("POST", "/admin/plans", form)
	if r.StatusCode != http.StatusCreated {
		c.t.Fatalf("create plan: %d %s", r.StatusCode, r.Body)
	}
	loc := r.Header.Get("Location")
	id, ok := strings.CutPrefix(loc, "/admin/plans/")
	if !ok {
		c.t.Fatalf("create plan: Location %q", loc)
	}
	var n int64
	for _, ch := range id {
		n = n*10 + int64(ch-'0')
	}
	return n
}

// FormValues collects what a browser would submit for the form matching sel:
// inputs, selects (selected option), textareas, checkboxes only when checked.
func FormValues(t *testing.T, d *goquery.Document, sel string) url.Values {
	t.Helper()
	f := d.Find(sel)
	if f.Length() != 1 {
		t.Fatalf("form %q: %d matches", sel, f.Length())
	}
	v := url.Values{}
	f.Find("input, select, textarea").Each(func(_ int, s *goquery.Selection) {
		name, ok := s.Attr("name")
		if !ok {
			return
		}
		switch goquery.NodeName(s) {
		case "select":
			sel := s.Find("option[selected]")
			if sel.Length() == 0 {
				sel = s.Find("option").First()
			}
			val, _ := sel.Attr("value")
			v.Set(name, val)
		case "textarea":
			v.Set(name, s.Text())
		default:
			typ, _ := s.Attr("type")
			if typ == "checkbox" || typ == "radio" {
				if _, checked := s.Attr("checked"); !checked {
					return
				}
				val, has := s.Attr("value")
				if !has {
					val = "on"
				}
				v.Set(name, val)
				return
			}
			val, _ := s.Attr("value")
			if typ == "hidden" {
				v.Add(name, val) // hidden fields may repeat (e.g. a list of ids)
			} else {
				v.Set(name, val)
			}
		}
	})
	return v
}

// MemberForm returns valid member form values for the given plan; override
// entries as needed ("-" deletes a key).
func MemberForm(planID int64, username string, override ...string) url.Values {
	v := url.Values{"username": {username}, "plan_id": {strconv.FormatInt(planID, 10)}, "active": {"on"}}
	for i := 0; i+1 < len(override); i += 2 {
		if override[i+1] == "-" {
			v.Del(override[i])
		} else {
			v.Set(override[i], override[i+1])
		}
	}
	return v
}

// CreateMember creates a member through the admin UI and returns its id.
func (c *AdminClient) CreateMember(form url.Values) int64 {
	c.t.Helper()
	r := c.Send("POST", "/admin/members", form)
	if r.StatusCode != http.StatusCreated {
		c.t.Fatalf("create member: %d %s", r.StatusCode, r.Body)
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(r.Header.Get("Location"), "/admin/members/"), 10, 64)
	if err != nil {
		c.t.Fatalf("create member: Location %q", r.Header.Get("Location"))
	}
	return id
}
