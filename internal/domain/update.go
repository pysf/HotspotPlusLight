package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Version is a release version X.Y.Z.
type Version struct{ Major, Minor, Patch int }

// ParseVersion reads "v1.2.3" or "1.2.3". It returns false for anything else, such as "dev" or "1.2".
func ParseVersion(s string) (Version, bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || strconv.Itoa(v) != p {
			return Version{}, false
		}
		n[i] = v
	}
	return Version{n[0], n[1], n[2]}, true
}

// String returns the version as "vX.Y.Z".
func (v Version) String() string { return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Less reports whether v is older than o.
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// IsNewerVersion reports whether latest is a newer release than current.
// It is false when either text is not a version, so a dev build never gets an offer.
func IsNewerVersion(latest, current string) bool {
	l, ok1 := ParseVersion(latest)
	c, ok2 := ParseVersion(current)
	return ok1 && ok2 && c.Less(l)
}

// Release is the part of a GitHub release that the update check uses.
type Release struct {
	Tag    string
	URL    string
	Assets []string // file names
}

// ParseLatestRelease reads the JSON of the GitHub "latest release" answer.
// It fails when the tag is not a version, so a strange answer is never shown as an update.
func ParseLatestRelease(data []byte) (Release, error) {
	var in struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return Release{}, fmt.Errorf("release answer is not valid JSON: %w", err)
	}
	v, ok := ParseVersion(in.TagName)
	if !ok {
		return Release{}, errors.New("release tag is not a version: " + strconv.Quote(in.TagName))
	}
	r := Release{Tag: v.String()}
	if strings.HasPrefix(in.HTMLURL, "https://") { // a mirror must not put other schemes into the page
		r.URL = in.HTMLURL
	}
	for _, a := range in.Assets {
		r.Assets = append(r.Assets, a.Name)
	}
	return r, nil
}

// AssetName returns the release file name for an OS and CPU: a .zip on Windows, else a .tar.gz.
// The names match the Makefile release target.
func AssetName(goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return "hotspotpluslight-" + goos + "-" + goarch + ext
}

// ParseSHA256Sums reads a SHA256SUMS file ("<hex>  <name>" per line) into a map from file name to lower-case hash.
// Lines that do not look like that are skipped.
func ParseSHA256Sums(text string) map[string]string {
	sums := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || len(f[0]) != 64 || !isHex(f[0]) {
			continue
		}
		sums[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0]) // "*" marks binary mode
	}
	return sums
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// UpdateState is the stored result of the last update check.
type UpdateState struct {
	Enabled   bool      // the "Check for updates" setting
	Latest    string    // "vX.Y.Z", empty before the first good check
	URL       string    // release notes page
	CheckedAt time.Time // zero before the first check
	Error     string    // friendly text of the last failure, empty after a good check
}
