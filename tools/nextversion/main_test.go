package main

import "testing"

func TestParseCommits(t *testing.T) {
	in := "feat: a\n\nbody line\n\nBREAKING CHANGE: x\n\x00\nfix: b\n\x00\n"
	got := parseCommits(in)
	if len(got) != 2 {
		t.Fatalf("got %d commits, want 2", len(got))
	}
	if got[0].Subject != "feat: a" || got[1].Subject != "fix: b" {
		t.Fatalf("bad subjects: %+v", got)
	}
	if got[0].Body == "" || got[1].Body != "" {
		t.Fatalf("bad bodies: %+v", got)
	}
	if len(parseCommits("")) != 0 {
		t.Fatal("empty input must give no commits")
	}
}
