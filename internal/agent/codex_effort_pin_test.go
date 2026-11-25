package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/edit"
)

// The effort pin (settings' EffortPin, "highest") reaches Codex's settle,
// the write a takeover and every later sync ends in: while it is on,
// model_reasoning_effort is the model's own highest level whatever new
// sessions started at — low on a model of low, high and max is written to
// the highest level Codex's picker offers it, which for a model of another
// vendor than OpenAI is Ultra after its own levels (codexcat.Levels) —
// recorded as magpie's own change the way the nearest-level guard
// records its writes, so Disconnect puts back what was there.
func TestCodexEffortPinWritesTheHighest(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`,
		"model = \"gpt-5.5\"\nmodel_reasoning_effort = \"low\"\n")
	if err := catalog.SaveLive("fake", "https://example.test/v1", []catalog.Model{
		{ID: "m1", Efforts: []string{"low", "high", "max"}}, // not an OpenAI model: Ultra follows its own levels
	}); err != nil {
		t.Fatal(err)
	}
	pinOn(t)
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, `model_reasoning_effort = "ultra"`) {
		t.Fatalf("the pin left the effort where it was:\n%s", cfg)
	}
	// already the pinned value: nothing is rewritten
	path := filepath.Join(home, ".codex", "config.toml")
	before, _ := os.Stat(path)
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(path); !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("an already-pinned effort was rewritten:\n%s", read())
	}
	// a later sync of a wired Codex keeps it pinned when Codex's own picker
	// (its /model) has saved a lower level in the meantime
	if err := edit.SetTOMLTop(path, edit.KV{Path: "model_reasoning_effort", Value: "low"}); err != nil {
		t.Fatal(err)
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, `model_reasoning_effort = "ultra"`) {
		t.Fatalf("a level Codex's picker saved was left below the pin:\n%s", cfg)
	}
}
