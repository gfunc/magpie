package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/provider"
)

// kimiMaxHome is a sandbox HOME with one model that offers max, so the
// pin's level and the model's own default are two different words.
func kimiMaxHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("KIMI_SHARE_DIR", "")
	t.Setenv("KIMI_CODE_HOME", "")
	if err := provider.Save(provider.Provider{ID: "think", Name: "Think", Chat: "https://example.test/v1", Key: "k", Models: []string{"k31"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("think", "https://example.test/v1", []catalog.Model{
		{ID: "k31", Efforts: []string{"low", "high", "max"}, DefaultEffort: "high"},
	}); err != nil {
		t.Fatal(err)
	}
	return home
}

// kimiStash writes the wrote-record a live config carried, as the stash
// keeps it.
func kimiStash(t *testing.T, home, path, table, value string) {
	t.Helper()
	wrote, _ := json.Marshal(map[string]map[string]string{table: {"default_effort": value}})
	stash, _ := json.Marshal(map[string]string{"kimi:" + path + ":wrote": string(wrote)})
	if err := os.WriteFile(filepath.Join(home, ".config", "magpie", "stash.json"), stash, 0o644); err != nil {
		t.Fatal(err)
	}
}

// kimiEffort reads the default_effort of the one model table in the config
// at path.
func kimiEffort(t *testing.T, path string) any {
	t.Helper()
	ms := readKimiTOML(t, path)["models"].(map[string]any)
	var e any
	for _, v := range ms {
		if m, ok := v.(map[string]any); ok {
			e = m["default_effort"]
		}
	}
	return e
}

// The live bug's exact shape (2026-11-16): a recorded value that the rule
// would no longer write must not freeze the table. A record names what
// magpie wrote, and what magpie writes is the pin's level, so a table
// whose record says high is taken to max while the pin is on.
func TestKimiPinFollowsThroughStaleRecordedHigh(t *testing.T) {
	home := kimiMaxHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	kimiStash(t, home, path, `models."magpie/think/k31"`, "high")
	pinOn(t)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := kimiEffort(t, path); got != "max" {
		t.Fatalf("the stale record froze the table: %v", got)
	}
}

// The live failure itself, the other way round: a plain Sync (what the
// gateway loop runs, with no Apply in between) carries the pin into the
// tables. The pin is in magpie's settings, and every sync reads it.
func TestKimiSyncCarriesThePinWithoutApply(t *testing.T) {
	home := kimiMaxHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	// the value magpie wrote under the old rule, and the record of it
	if err := edit.SetTOMLKey(path, `models."magpie/think/k31"`, "default_effort", "low"); err != nil {
		t.Fatal(err)
	}
	kimiStash(t, home, path, `models."magpie/think/k31"`, "low")
	pinOn(t)
	// a later Sync alone: the pin still drives the tables
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := kimiEffort(t, path); got != "max" {
		t.Fatalf("a Sync without an Apply: %v", got)
	}
}

// k3.1-pro shape: efforts low/high/max, record high, the pin on. The
// pin's answer (max) differs from the record: the write must take max.
func TestKimiPinOverridesDifferingRecord(t *testing.T) {
	home := kimiMaxHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	// record says magpie wrote the vendor fallback high (an older build)
	kimiStash(t, home, path, `models."magpie/think/k31"`, "high")
	pinOn(t)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := kimiEffort(t, path); got != "max" {
		t.Fatalf("default_effort %v with the pin on, want max (the record's high is magpie's own, not the user's hand)", got)
	}
}

// The trigger the live box actually had, now the other way round:
// [thinking] effort = max already on disk, a differing record (low), and
// no Apply in this process, only a Sync, as SyncCatalog runs it. The
// pick is the lead session's own, ahead of every table's default, so the
// table rests at the model's own default.
func TestKimiSyncAloneIgnoresTheOnDiskPick(t *testing.T) {
	home := kimiMaxHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	// the pick from an earlier day, as the live config had it
	if err := a.Field("effort").Set("max"); err != nil {
		t.Fatal(err)
	}
	// the value magpie wrote under the old rule, and the record of it
	if err := edit.SetTOMLKey(path, `models."magpie/think/k31"`, "default_effort", "low"); err != nil {
		t.Fatal(err)
	}
	kimiStash(t, home, path, `models."magpie/think/k31"`, "low")
	// this process's only act: a Sync
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := kimiEffort(t, path); got != "high" {
		t.Fatalf("default_effort %v after a Sync with the pick on disk, want the model's own high", got)
	}
}
