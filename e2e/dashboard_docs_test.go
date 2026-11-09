package e2e

import (
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// E19.01 the site has the Dashboard page, linked from the docs navigation of every docs page and from the search index.
func TestE19_01_DashboardDocsPage(t *testing.T) {
	const page = "docs/dashboard/index.html"
	if !slices.Contains(docsPages(t), page) {
		t.Fatalf("%s does not exist", page)
	}
	d := siteDoc(t, page)
	if h1 := strings.TrimSpace(d.Find("h1").Text()); h1 != "Dashboard" {
		t.Errorf("h1 %q", h1)
	}
	var ids []string
	d.Find(".doc section h2").Each(func(_ int, s *goquery.Selection) { ids = append(ids, s.AttrOr("id", "")) })
	for _, want := range []string{"tiles", "router-status", "attention", "charts", "failed-logins"} {
		if !slices.Contains(ids, want) {
			t.Errorf("the Dashboard page has no section %q (has %v)", want, ids)
		}
	}
	for _, p := range docsPages(t) {
		found := false
		siteDoc(t, p).Find(".side nav > ul > li > a").Each(func(_ int, s *goquery.Selection) {
			if strings.TrimSpace(s.Text()) == "Dashboard" && path.Join(path.Dir(p), s.AttrOr("href", ""), "index.html") == page {
				found = true
			}
		})
		if !found {
			t.Errorf("%s: the docs sidebar has no working Dashboard link", p)
		}
	}
	index := searchIndex(t)
	inIndex := map[string]bool{}
	for _, e := range index {
		if e.Page == "Dashboard" {
			inIndex[e.URL] = true
		}
	}
	for _, id := range ids {
		if !inIndex["docs/dashboard/#"+id] {
			t.Errorf("section %s of the Dashboard page is not in the search index", id)
		}
	}
	if !strings.Contains(siteRead(t, "sitemap.xml"), "/docs/dashboard/</loc>") {
		t.Error("sitemap.xml does not list the Dashboard page")
	}
}

type searchEntry struct{ Page, Heading, URL, Keywords string }

// searchIndex returns the entries of site/assets/js/search-index.js.
func searchIndex(t *testing.T) []searchEntry {
	t.Helper()
	src := siteRead(t, "assets/js/search-index.js")
	src = strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(src[strings.Index(src, "export default"):], "export default")), ";")
	var index []searchEntry
	if err := json.Unmarshal([]byte(src), &index); err != nil {
		t.Fatal(err)
	}
	return index
}

// movedPhrase is the one place that may name the Router page next to the router status: the README says where the panel came from.
const movedPhrase = "moved here from the Router page"

// E19.02 no site page, search entry or README text puts the link status (the router status) on the Router page.
// A mention of "Router page" within 120 characters of a status word fails, unless it is the exact movedPhrase.
func TestE19_02_NoLinkStatusOnRouterPage(t *testing.T) {
	texts := map[string]string{"README.md": readRoot(t, "README.md")}
	for _, e := range searchIndex(t) {
		texts["search index "+e.URL] = e.Keywords
	}
	for _, p := range sitePages(t) {
		texts[p] = siteText(t, p)
	}
	status := regexp.MustCompile(`(?i)link status|router status|status panel`)
	routerPage := regexp.MustCompile(`(?i)router page`)
	for name, text := range texts {
		text = strings.Join(strings.Fields(strings.ReplaceAll(text, movedPhrase, "")), " ")
		for _, at := range routerPage.FindAllStringIndex(text, -1) {
			window := text[max(0, at[0]-120):min(len(text), at[1]+120)]
			if status.MatchString(window) {
				t.Errorf("%s may still put the link status on the Router page: %q", name, window)
			}
		}
	}
	if !strings.Contains(texts["README.md"], movedPhrase) {
		t.Error("the README no longer says that the router status moved to the Dashboard")
	}
	if !strings.Contains(texts["README.md"], "router status panel on the Dashboard") {
		t.Error("the README package map does not say the router status panel is on the Dashboard")
	}
}

// E19.03 the README and the site describe the failed-login counter: what is counted, no personal data, and how long it is kept.
func TestE19_03_FailedLoginCounterDocumented(t *testing.T) {
	readme := readRoot(t, "README.md")
	for _, want := range []string{"Failed logins", "No user names and no addresses are stored", "counts once", "session retention", "0 = forever"} {
		if !strings.Contains(readme, want) {
			t.Errorf("the README lacks %q", want)
		}
	}
	page := strings.Join(strings.Fields(siteText(t, "docs/dashboard/index.html")), " ")
	for _, want := range []string{"How failed logins are counted", "No personal data", "No user names and no addresses", "A retry counts once",
		"1024 requests for 60 seconds", "wrong shared secret", "Session retention (days)", "90 by default", "deleted with the old sessions", "With 0 they are kept forever"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Dashboard page lacks %q", want)
		}
	}
}
