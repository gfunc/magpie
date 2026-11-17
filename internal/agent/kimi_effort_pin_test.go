package agent

import (
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/edit"
)

// The effort pin (settings' EffortPin, "highest") is the fork's ask that
// every model an agent runs starts at its own highest thinking level
// whatever the agent or the user picked. Kimi Code's write path is
// writeMagpie: with the pin on, the Agents-page pick stands down and every
// magpie-owned default_effort is the model's strongest level — deep
// (none/low/high) high, wide (low/medium/high) high — whether the pick is
// low, or there is none. On the pre-change code this fails: with no pick
// deep stayed high (the vendor/high fallback) but wide kept its vendor
// default medium, and with a low pick both sat at low.
func TestKimiEffortPinWritesTheHighest(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, "")
	pinOn(t)
	a := kimi(home)
	efforts := func() map[string]string {
		t.Helper()
		ms := readKimiTOML(t, path)["models"].(map[string]any)
		out := map[string]string{}
		for k, v := range ms {
			m, ok := v.(map[string]any)
			if !ok {
				continue
			}
			if e, ok := m["default_effort"].(string); ok {
				out[k] = e
			}
		}
		return out
	}
	// a takeover write with no pick at all ([thinking] empty): the pin
	// alone puts every model at its highest
	if err := a.Pick("model", "magpie/think/deep"); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/think/deep"] != "high" || got["magpie/think/wide"] != "high" {
		t.Fatalf("pin on, no pick: %v", got)
	}
	// a pick below the model's top stands down to the same answer, and so
	// does the [thinking] pick the field's Set writes
	for _, pick := range []string{"low", "high"} {
		if err := a.Apply("effort", pick); err != nil {
			t.Fatal(err)
		}
		if got := efforts(); got["magpie/think/deep"] != "high" || got["magpie/think/wide"] != "high" {
			t.Fatalf("pin on, pick %q: %v", pick, got)
		}
	}
	// the pick itself is still written as picked ([thinking] is the user's
	// own control); only the tables' defaults are pinned
	if v, _ := edit.GetTOMLTable(path, "thinking"); v["effort"] != "high" {
		t.Fatalf("the pick was not kept as picked: %v", v)
	}
}
