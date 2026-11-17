package agent

import (
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/edit"
)

// The effort pin (settings' EffortPin, "highest") reaches Claude Code's
// effort field: while it is on, whatever is set — a lower level, or
// Default ("") — is written as the current model's highest level. Opus
// 4.6, whose levels stop at max, gets max, which lasts a session only in
// the env, so it is CLAUDE_CODE_EFFORT_LEVEL every session starts with;
// Default doesn't turn thinking off. On the pre-change code this fails:
// low was written as low.
func TestClaudeEffortPinWritesTheHighest(t *testing.T) {
	home, path := claudeSettings(t, `{"model": "claude-opus-4-6[1m]"}`)
	pinOn(t)
	e := claude(home).Field("effort")
	if err := e.Set("low"); err != nil {
		t.Fatal(err)
	}
	get := func(k string) string { v, _ := edit.GetJSON(path, k); return v }
	// max is the env's, which every session starts with and asks for
	if v := get("env.CLAUDE_CODE_EFFORT_LEVEL"); v != "max" {
		t.Fatalf("the env's level: %q\n%s", v, readFile(path))
	}
	if got := e.Get(); got != "max" {
		t.Fatalf("read back %q, want max", got)
	}
	// Default ("") pins to the highest too, rather than clearing the level
	if err := e.Set(""); err != nil {
		t.Fatal(err)
	}
	if v := get("env.CLAUDE_CODE_EFFORT_LEVEL"); v != "max" {
		t.Fatalf("Default: %q\n%s", v, readFile(path))
	}
	// a model with no levels (the Haiku 4.5) keeps its own read: nothing
	// is written where there is no highest to write
	home, path = claudeSettings(t, `{"model": "claude-haiku-4-5"}`)
	e = claude(home).Field("effort")
	if err := e.Set("low"); err == nil || !strings.Contains(err.Error(), "no effort") {
		t.Fatalf("a model with no levels took a pin: %v", err)
	}
}
