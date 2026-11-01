package harness

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
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

// Login logs in and loads the CSRF token from the landing page; it fails the
// test on any other outcome.
func (c *AdminClient) Login(username, password string) {
	c.t.Helper()
	r := c.PostLogin(username, password)
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/admin/members" {
		c.t.Fatalf("login: status %d location %q", r.StatusCode, r.Header.Get("Location"))
	}
	c.RefreshCSRF()
}

// RefreshCSRF re-reads the CSRF token from an authenticated page.
func (c *AdminClient) RefreshCSRF() {
	c.t.Helper()
	r := c.Get("/admin/members")
	if r.StatusCode != 200 {
		c.t.Fatalf("members page: %d", r.StatusCode)
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

// LoginAsAdmin returns a logged-in client using the first-start printed password.
func LoginAsAdmin(t *testing.T, s *Server) *AdminClient {
	t.Helper()
	pw, ok := AdminPasswordFromStdout(s.Stdout.String())
	if !ok {
		t.Fatal("no first-start password in stdout")
	}
	c := NewAdminClient(t, s)
	c.Login("admin", pw)
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
			v.Set(name, val)
		}
	})
	return v
}
