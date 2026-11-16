package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// The sign-in's own plan tables ([models."kimi-code/…"] on the managed
// provider, written by the kimi CLI from Kimi's list) sit outside the
// takeover's write set, so the Agents-page pick never reached them: with
// the pick at max, K3.1 Pro's table stayed at the vendor's high while the
// same model through magpie (magpie/kimi-code-cn/k3.1-pro) ran at max.
// While a pick is set, writeMagpie clamps the sign-in tables' magpie-owned
// default_effort to it as well; with no pick they keep the vendor's.
func TestKimiPickReachesTheSignInTables(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	// the sign-in's table, as the kimi CLI leaves it: the vendor's high
	writeFile(t, path, `[providers."managed:kimi-code"]
type = "kimi"

[models."kimi-code/k3.1-pro"]
provider = "managed:kimi-code"
model = "k3.1-pro"
support_efforts = ["low", "high", "max"]
default_effort = "high"
`)
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	read := func() string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var c struct {
			Models map[string]struct {
				DefaultEffort string `toml:"default_effort"`
			} `toml:"models"`
		}
		if err := toml.Unmarshal(b, &c); err != nil {
			t.Fatal(err)
		}
		return c.Models[`kimi-code/k3.1-pro`].DefaultEffort
	}
	// no pick: the sign-in's vendor default stays
	if got := read(); got != "high" {
		t.Fatalf("no pick: sign-in default %q, want the vendor's high", got)
	}
	// the pick: the sign-in table follows it (max, which it offers)
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "max" {
		t.Fatalf("pick max: sign-in default %q, want max", got)
	}
	// a hand-set value on the sign-in table is the user's, not the pick's
	if err := os.WriteFile(path, []byte(`[providers."managed:kimi-code"]
type = "kimi"

[models."kimi-code/k3.1-pro"]
provider = "managed:kimi-code"
model = "k3.1-pro"
support_efforts = ["low", "high", "max"]
default_effort = "low"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "low" {
		t.Fatalf("hand-set: sign-in default %q, want the user's low", got)
	}
	// the pick off again (Default): back to the vendor's
	if err := a.Apply("effort", ""); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "low" {
		t.Fatalf("pick off with a hand-set kept: %q, want low", got)
	}
}
