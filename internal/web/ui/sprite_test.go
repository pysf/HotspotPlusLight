package ui

import (
	"strings"
	"testing"
)

// Every icon name that the shell and the shared components use must exist in the sprite.
func TestSpriteHasEveryIconInUse(t *testing.T) {
	names := []string{"home", "users", "wifi", "layers", "ticket", "chart", "router", "settings", "key", "sun", "moon", "logout", "more",
		"check-circle", "clock", "alert", "slash", "info", "search", "plus", "x", "copy", "chevron-down", "chevron-left", "chevron-right", "trash"}
	for _, n := range names {
		if !strings.Contains(spriteSymbols, `id="i-`+n+`"`) {
			t.Errorf("icon %q is not in the sprite", n)
		}
	}
	for _, tone := range []string{"ok", "warn", "bad", "off", "info"} {
		if icon := pillIcon(tone); !strings.Contains(spriteSymbols, `id="i-`+icon+`"`) {
			t.Errorf("pill tone %q uses icon %q, which is not in the sprite", tone, icon)
		}
	}
}

func TestPillToneMapsMemberStatuses(t *testing.T) {
	for status, want := range map[string]string{"active": "ok", "expiring": "warn", "expired": "bad", "disabled": "off", "info": "info", "other": "off"} {
		if got := pillTone(status); got != want {
			t.Errorf("pillTone(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestParseThemeFallsBackToAuto(t *testing.T) {
	for in, want := range map[string]string{"": "auto", "light": "light", "dark": "dark", "air": "auto", `"><script>`: "auto"} {
		if got := ParseTheme(in); got != want {
			t.Errorf("ParseTheme(%q) = %q, want %q", in, got, want)
		}
	}
}
