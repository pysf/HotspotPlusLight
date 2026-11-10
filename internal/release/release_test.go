package release

import "testing"

func TestNextVersion(t *testing.T) {
	tests := []struct {
		name    string
		last    string
		commits []Commit
		want    string
		ok      bool
	}{
		{"no commits", "v0.9.0", nil, "", false},
		{"fix is patch", "v0.9.0", []Commit{{Subject: "fix: typo"}}, "v0.9.1", true},
		{"feat is minor", "v0.9.0", []Commit{{Subject: "feat: new page"}}, "v0.10.0", true},
		{"feat with scope", "v1.2.3", []Commit{{Subject: "feat(radius): add x"}}, "v1.3.0", true},
		{"empty scope is not a commit type", "v1.2.3", []Commit{{Subject: "feat(): x"}}, "", false},
		{"fix with scope", "v1.2.3", []Commit{{Subject: "fix(ui): y"}}, "v1.2.4", true},
		{"bang is major", "v1.2.3", []Commit{{Subject: "refactor!: drop api"}}, "v2.0.0", true},
		{"bang with scope", "v1.2.3", []Commit{{Subject: "feat(api)!: drop"}}, "v2.0.0", true},
		{"breaking in body", "v1.2.3", []Commit{{Subject: "fix: z", Body: "text\n\nBREAKING CHANGE: old config gone\n"}}, "v2.0.0", true},
		{"breaking below 1.0", "v0.9.0", []Commit{{Subject: "feat!: big"}}, "v1.0.0", true},
		{"highest wins", "v1.0.0", []Commit{{Subject: "fix: a"}, {Subject: "feat: b"}, {Subject: "fix: c"}}, "v1.1.0", true},
		{"major beats feat", "v1.4.2", []Commit{{Subject: "feat: b"}, {Subject: "fix!: c"}}, "v2.0.0", true},
		{"docs only", "v0.9.0", []Commit{{Subject: "docs: a"}, {Subject: "chore: b"}, {Subject: "test: c"}}, "", false},
		{"other types", "v0.9.0", []Commit{
			{Subject: "site: a"}, {Subject: "ci: b"}, {Subject: "refactor: c"},
			{Subject: "build: d"}, {Subject: "style: e"}, {Subject: "perf: f"},
		}, "", false},
		{"free text subject", "v0.9.0", []Commit{{Subject: "Fix: with capital"}, {Subject: "fix something"}}, "", false},
		{"breaking text not at line start", "v0.9.0", []Commit{{Subject: "docs: a", Body: "see BREAKING CHANGE: x"}}, "", false},
		{"upper case type", "v0.9.0", []Commit{{Subject: "FEAT: a"}}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := NextVersion(tt.last, tt.commits)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want || ok != tt.ok {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestNextVersionBadInput(t *testing.T) {
	for _, last := range []string{"", "0.9.0", "v1", "v1.2", "v1.2.x", "v1.2.3-rc1", "v01.2.3", "v1.2.3.4", "v-1.2.3"} {
		if _, _, err := NextVersion(last, []Commit{{Subject: "fix: a"}}); err == nil {
			t.Errorf("last %q: want an error", last)
		}
	}
}
