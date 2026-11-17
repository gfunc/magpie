package agent

import (
	"github.com/yetone/magpie/internal/settings"
)

// effortPinned says the pin is on: every model an agent runs, while magpie
// has taken it over, starts at its own highest thinking level whatever the
// agent or the user picked. The three takeover write paths (kimi's model
// tables, Codex's settle, Claude Code's effort field) each ask this before
// following a pick.
func effortPinned() bool { return settings.Shared().EffortPin == settings.EffortPinHighest }

// pinnedEffort reports the effort a takeover should write for a model when
// the pin is on: the model's highest offered level, "" when the pin is off
// or the model offers none (nothing ranks — words catalog.EffortRank
// doesn't know, or "none" alone, which is thinking off rather than a
// level). Callers treat "" as "no pin", so a model with no rankable level
// keeps its own default rather than being written at a word it doesn't
// take.
func pinnedEffort(levels []string) string {
	if !effortPinned() {
		return ""
	}
	return kimiHighestEffort(levels)
}
