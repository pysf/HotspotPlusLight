// Command nextversion prints the next release version, or nothing.
//
// Usage: git log --format=%B%x00 v0.9.0..HEAD | nextversion v0.9.0
//
// Standard input holds full commit messages, each ended by a NUL byte.
// The first line of a message is the subject; the rest is the body.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"hotspotpluslight/internal/release"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: nextversion <lastversion> < gitlog")
		os.Exit(2)
	}
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nextversion:", err)
		os.Exit(1)
	}
	next, ok, err := release.NextVersion(os.Args[1], parseCommits(string(in)))
	if err != nil {
		fmt.Fprintln(os.Stderr, "nextversion:", err)
		os.Exit(1)
	}
	if ok {
		fmt.Println(next)
	}
}

// parseCommits splits NUL-ended messages into commits and skips empty ones.
func parseCommits(s string) []release.Commit {
	var out []release.Commit
	for rec := range strings.SplitSeq(s, "\x00") {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		subject, body, _ := strings.Cut(rec, "\n")
		out = append(out, release.Commit{Subject: strings.TrimSpace(subject), Body: body})
	}
	return out
}
