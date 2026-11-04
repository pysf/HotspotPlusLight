package routerpack

import (
	"fmt"
	"html"
	"path"
	"regexp"
	"strings"
)

// PreviewPages lists the pages that can be previewed (the visible ones).
var PreviewPages = []string{"login", "alogin", "status", "logout", "error"}

// sampleVars are the router values used in the preview.
var sampleVars = map[string]string{
	"link-login-only": "#", "link-login": "#", "link-logout": "#", "link-status": "#", "link-orig": "https://example.com/",
	"link-redirect": "#", "username": "guest42", "uptime": "12m 30s", "session-time-left": "47m 30s",
	"remain-bytes-total": "1288490188", "bytes-in-nice": "12.4 MiB", "bytes-out-nice": "310.2 MiB",
	"ip": "192.168.88.10", "mac": "AA:BB:CC:DD:EE:01", "login-by": "http-pap",
}

// Preview returns the HTML of page (one of PreviewPages) of design key, filled with sample
// values. errCode is the sample value of $(error), for example OUT_OF_BULK; empty means no error.
// It only approximates RouterOS: an unknown variable is empty. Relative links (css/, img/) are
// served by PreviewAsset.
func (p *Pack) Preview(key, page, errCode string) ([]byte, error) {
	files, err := p.Files(key)
	if err != nil {
		return nil, err
	}
	src, ok := files[page+".html"]
	if !ok {
		return nil, fmt.Errorf("no page %q", page)
	}
	vars := map[string]string{"error": errCode}
	for k, v := range sampleVars {
		vars[k] = v
	}
	return []byte(renderTemplate(string(src), vars)), nil
}

// PreviewAsset returns a stylesheet or image of design key with its content type, and false if
// there is no such file.
func (p *Pack) PreviewAsset(key, name string) ([]byte, string, bool) {
	types := map[string]string{".css": "text/css; charset=utf-8", ".svg": "image/svg+xml", ".png": "image/png"}
	if !strings.HasPrefix(name, "css/") && !strings.HasPrefix(name, "img/") {
		return nil, "", false
	}
	ct, ok := types[path.Ext(name)]
	if !ok {
		return nil, "", false
	}
	files, err := p.Files(key)
	if err != nil || files[name] == nil {
		return nil, "", false
	}
	return files[name], ct, true
}

// tagRE matches a RouterOS $(...) tag.
var tagRE = regexp.MustCompile(`\$\(([^()]*)\)`)

// frame is the state of one open if block in renderTemplate.
type frame struct{ parent, taken, on bool }

// renderTemplate resolves $(var) and $(if ...)$(elif ...)$(else)$(endif) against vars.
func renderTemplate(src string, vars map[string]string) string {
	var out strings.Builder
	var stack []frame
	emit := func() bool { return len(stack) == 0 || stack[len(stack)-1].on }
	last := 0
	for _, loc := range tagRE.FindAllStringSubmatchIndex(src, -1) {
		if emit() {
			out.WriteString(src[last:loc[0]])
		}
		last = loc[1]
		tag := strings.TrimSpace(src[loc[2]:loc[3]])
		word, rest, _ := strings.Cut(tag, " ")
		switch word {
		case "if":
			parent := emit()
			c := parent && condition(rest, vars)
			stack = append(stack, frame{parent, c, c})
		case "elif":
			if n := len(stack); n > 0 {
				f := &stack[n-1]
				c := f.parent && !f.taken && condition(rest, vars)
				f.on, f.taken = c, f.taken || c
			}
		case "else":
			if n := len(stack); n > 0 {
				f := &stack[n-1]
				f.on, f.taken = f.parent && !f.taken, true
			}
		case "endif":
			if n := len(stack); n > 0 {
				stack = stack[:n-1]
			}
		default:
			if emit() {
				out.WriteString(html.EscapeString(vars[tag]))
			}
		}
	}
	if emit() {
		out.WriteString(src[last:])
	}
	return out.String()
}

// condition reports whether expr is true. It handles "name", "name == value" and "name != value" (value in quotes or bare).
func condition(expr string, vars map[string]string) bool {
	expr = strings.TrimSpace(expr)
	for _, op := range []string{"==", "!="} {
		if l, r, ok := strings.Cut(expr, op); ok {
			got := vars[strings.TrimSpace(l)]
			want := strings.Trim(strings.TrimSpace(r), `"'`)
			return (got == want) == (op == "==")
		}
	}
	return vars[expr] != ""
}
