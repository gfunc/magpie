package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// The sign-in's own plan tables ([models."kimi-code/…"] on the managed
// provider, written by the kimi CLI from Kimi's list) are Kimi Code's own:
// magpie writes nothing in them and records nothing, so the vendor's
// default stands until Kimi Code's own start-up puts it back. The Agents-
// page pick does not reach them either: Kimi Code resolves the pick ahead
// of every table's own default. The pin is the one thing that reaches
// them: with it on every one of them is at its own highest level, as a
// model run through magpie is.
func TestKimiSignInTablesFollowOnlyThePin(t *testing.T) {
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
	// the pin off: the sign-in's vendor default stands, and magpie keeps
	// no record of the table
	if got := read(); got != "high" {
		t.Fatalf("no pin: sign-in default %q, want the vendor's high", got)
	}
	if r := kimiWrote(path)[`models."kimi-code/k3.1-pro"`]; r != nil {
		t.Fatalf("the sign-in table was recorded: %v", r)
	}
	// a pick does not reach it
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "high" {
		t.Fatalf("pick max: sign-in default %q, want the vendor's high", got)
	}
	// the pin on: the table is at its own highest level
	pinOn(t)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "max" {
		t.Fatalf("pin on: sign-in default %q, want max", got)
	}
}
