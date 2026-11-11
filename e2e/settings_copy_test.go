package e2e

import (
	"github.com/PuerkitoBio/goquery"
	"strings"
	"testing"

	"hotspotpluslight/e2e/harness"
)

// E23.01 the RADIUS secret has a Copy button wired like the other Copy buttons. It points at the masked input, so the
// secret is not put into the page a second time and the admin does not need to reveal it first.
func TestE23_01_SecretCopyButton(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	r := c.Get("/admin/settings")
	d := r.Doc(t)

	in := d.Find(`input[name="radius_shared_secret"]`)
	secret := in.AttrOr("value", "")
	if in.AttrOr("type", "") != "password" || secret == "" {
		t.Fatalf("secret input: type %q", in.AttrOr("type", ""))
	}
	btn := d.Find(`button[data-copy-target="radius_shared_secret"]`)
	if btn.Length() != 1 || btn.AttrOr("type", "") != "button" || btn.AttrOr("onclick", "") != "" {
		t.Fatalf("want one markup-only copy button for the secret, got %d", btn.Length())
	}
	if d.Find(`[data-copy-text]`).FilterFunction(func(_ int, b *goquery.Selection) bool {
		return strings.Contains(b.AttrOr("data-copy-text", ""), secret)
	}).Length() != 0 {
		t.Error("the secret must not be copied from a data-copy-text attribute")
	}
	if n := strings.Count(r.Body, secret); n != 1 {
		t.Errorf("secret occurs %d times in the page, want 1", n)
	}
}
