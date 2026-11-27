package agent

import (
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/settings"
)

// effortSettingsHome is a HOME of its own with no settings in it, so the
// pin reads off until a test turns it on.
func effortSettingsHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	return home
}

// pinOn saves the pin on for the test and off after it, so no other test's
// settings say it.
func pinOn(t *testing.T) {
	t.Helper()
	if err := settings.Save(settings.Settings{EffortPin: settings.EffortPinHighest}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := settings.Save(settings.Settings{}); err != nil {
			t.Fatal(err)
		}
	})
}

// pinnedEffort is the model's strongest offered level while the pin is on,
// ranked as catalog.EffortRank orders the words (a vendor's own word for a
// level included), and "" while the pin is off, for a model with no
// rankable level (none alone, unknown words, nothing offered).
func TestPinnedEffort(t *testing.T) {
	for _, tc := range []struct {
		name   string
		levels []string
		out    string
	}{
		{"ranked, deep", []string{"none", "low", "high"}, "high"},
		{"ranked, wide", []string{"low", "medium", "high"}, "high"},
		{"ranked, with max", []string{"low", "high", "max"}, "max"},
		{"ranked, no max, xhigh", []string{"low", "medium", "high", "xhigh"}, "xhigh"},
		{"alias light is low", []string{"light", "low", "medium"}, "medium"},
		{"alias light alone", []string{"light", "high"}, "high"},
		{"none only", []string{"none"}, ""},
		{"none and none", []string{"none", "none"}, ""},
		{"unknown words", []string{"turbo", "ludicrous"}, ""},
		{"nothing offered", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pinOn(t)
			if got := pinnedEffort(tc.levels); got != tc.out {
				t.Errorf("pinnedEffort(%v) = %q, want %q", tc.levels, got, tc.out)
			}
			if !effortPinned() {
				t.Error("effortPinned says off while the pin is saved on")
			}
		})
	}
	// the pin off: nothing is pinned, whatever the levels
	t.Run("pin off", func(t *testing.T) {
		for _, levels := range [][]string{{"low", "high", "max"}, {"none", "low", "high"}} {
			if got := pinnedEffort(levels); got != "" {
				t.Errorf("pinnedEffort(%v) = %q, want %q", levels, got, "")
			}
		}
		if effortPinned() {
			t.Error("effortPinned says on with no pin saved")
		}
	})
}
