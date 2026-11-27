package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/yetone/magpie/internal/edit"
)

// kimiSignInConfig is the sign-in's own plan table, as the kimi CLI leaves
// it: the vendor's default.
const kimiSignInConfig = `[providers."managed:kimi-code"]
type = "kimi"

[models."kimi-code/k3.1-pro"]
provider = "managed:kimi-code"
model = "k3.1-pro"
support_efforts = ["low", "high", "max"]
default_effort = "high"
`

// kimiSignInDefault reads the default_effort of the sign-in's own table in
// the config at path.
func kimiSignInDefault(t *testing.T, path string) string {
	t.Helper()
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
	writeFile(t, path, kimiSignInConfig)
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	table := `models."kimi-code/k3.1-pro"`
	// the pin off: the sign-in's vendor default stands, and magpie keeps
	// no record of the table
	if got := kimiSignInDefault(t, path); got != "high" {
		t.Fatalf("no pin: sign-in default %q, want the vendor's high", got)
	}
	if r := kimiWrote(path)[table]; r != nil {
		t.Fatalf("the sign-in table was recorded: %v", r)
	}
	// a pick does not reach it
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSignInDefault(t, path); got != "high" {
		t.Fatalf("pick max: sign-in default %q, want the vendor's high", got)
	}
	// the pin on: the table is at its own highest level
	pinOn(t)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := kimiSignInDefault(t, path); got != "max" {
		t.Fatalf("pin on: sign-in default %q, want max", got)
	}
}

// The record of a sign-in table magpie wrote is kept when a sync writes
// nothing: the record says the value on disk is magpie's to change, and a
// hand-set value made later is protected by it. Before, the record was
// dropped on every sync that wrote nothing.
func TestKimiSignInRecordKeptWhenNothingIsWritten(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, kimiSignInConfig)
	pinOn(t)
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	table := `models."kimi-code/k3.1-pro"`
	if got := kimiWrote(path)[table]["default_effort"]; got != "max" {
		t.Fatalf("the write was not recorded: %v", kimiWrote(path))
	}
	// a sync that writes nothing: the table is already at the pin's level
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := kimiWrote(path)[table]["default_effort"]; got != "max" {
		t.Fatalf("the record was dropped: %v", kimiWrote(path))
	}
}

// A hand-set value on a sign-in table survives the pin once magpie has
// recorded that table: the record says the value on disk is magpie's to
// change, so a value that differs from it is the user's. With no record
// the value on disk is Kimi Code's own write and magpie claims it, which
// is what a fresh home gets: the file cannot tell a hand-set low from a
// CLI-written one.
func TestKimiSignInHandSetSurvivesOnlyOnceRecorded(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, kimiSignInConfig)
	table := `models."kimi-code/k3.1-pro"`
	// the user hand-sets low before magpie has written the table
	if err := edit.SetTOMLKey(path, table, "default_effort", "low"); err != nil {
		t.Fatal(err)
	}
	pinOn(t)
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if got := kimiSignInDefault(t, path); got != "max" {
		t.Fatalf("the hand-set value on an unrecorded table: %q", got)
	}
	// magpie has recorded the table now: a hand-set value is the user's
	// and stays, and the record still names what magpie wrote
	if err := edit.SetTOMLKey(path, table, "default_effort", "low"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := kimiSignInDefault(t, path); got != "low" {
		t.Fatalf("the hand-set value on a recorded table: %q", got)
	}
	if got := kimiWrote(path)[table]["default_effort"]; got != "max" {
		t.Fatalf("the record names the user's value: %v", kimiWrote(path))
	}
}
