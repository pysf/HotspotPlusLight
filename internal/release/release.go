// Package release works out the next version number from commit messages.
// It follows the conventional commits rules and does no I/O.
package release

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Commit is one commit message, split into its first line and the rest.
type Commit struct {
	Subject string
	Body    string
}

type bump int

const (
	bumpNone bump = iota
	bumpPatch
	bumpMinor
	bumpMajor
)

var (
	versionRe = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	subjectRe = regexp.MustCompile(`^([a-z]+)(\([^)]+\))?(!)?: .+`)
)

// NextVersion returns the version that follows last, such as "v0.9.1".
// It returns ok false when no commit makes a release. It returns an error
// when last is not in the form vMAJOR.MINOR.PATCH.
func NextVersion(last string, commits []Commit) (next string, ok bool, err error) {
	m := versionRe.FindStringSubmatch(last)
	if m == nil {
		return "", false, fmt.Errorf("release: bad version %q, want vMAJOR.MINOR.PATCH", last)
	}
	var v [3]int
	for i := range v {
		if v[i], err = strconv.Atoi(m[i+1]); err != nil {
			return "", false, fmt.Errorf("release: bad version %q: %w", last, err)
		}
	}

	top := bumpNone
	for _, c := range commits {
		top = max(top, commitBump(c))
	}
	switch top {
	case bumpMajor:
		v = [3]int{v[0] + 1, 0, 0}
	case bumpMinor:
		v = [3]int{v[0], v[1] + 1, 0}
	case bumpPatch:
		v[2]++
	default:
		return "", false, nil
	}
	return fmt.Sprintf("v%d.%d.%d", v[0], v[1], v[2]), true, nil
}

func commitBump(c Commit) bump {
	m := subjectRe.FindStringSubmatch(c.Subject)
	if m == nil {
		return bumpNone
	}
	if m[3] == "!" || hasBreakingLine(c.Body) {
		return bumpMajor
	}
	switch m[1] {
	case "feat":
		return bumpMinor
	case "fix":
		return bumpPatch
	}
	return bumpNone
}

// hasBreakingLine reports whether a body line starts with the breaking marker.
func hasBreakingLine(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, "BREAKING CHANGE:") || strings.HasPrefix(line, "BREAKING-CHANGE:") {
			return true
		}
	}
	return false
}
