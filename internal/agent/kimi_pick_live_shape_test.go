package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/provider"
)

// The live bug's exact shape (2026-11-16): [thinking] effort = "max",
// magpie-owned tables with vendor fallbacks (high on low/medium/high
// models, xhigh on xhigh-topped models) recorded in the wrote-record as
// magpie's own. A sync kept them: kimiOwn read was == wrote and returned
// the record's value as magpie's own, and kimiEfforts kept it — until the
// pick resolved to something the record disagreed with only by era, not
// by hand. The pick must win whenever the recorded value is one the pick
// rule would no longer write: a record names what magpie wrote, and what
// magpie writes follows the pick.
func TestKimiPickFollowsThroughStaleRecordedHigh(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	wrote, _ := json.Marshal(map[string]map[string]string{
		`models."magpie/think/deep"`: {"default_effort": "high"},
		`models."magpie/think/wide"`: {"default_effort": "high"},
	})
	stash, _ := json.Marshal(map[string]string{"kimi:" + path + ":wrote": string(wrote)})
	if err := os.WriteFile(filepath.Join(home, ".config", "magpie", "stash.json"), stash, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	ms := readKimiTOML(t, path)["models"].(map[string]any)
	got := map[string]string{}
	for k, v := range ms {
		if m, ok := v.(map[string]any); ok {
			if e, ok := m["default_effort"].(string); ok {
				got[k] = e
			}
		}
	}
	// no pick yet ([thinking] empty): the record's high is what magpie
	// last wrote, so it stays — that part is right
	if got["magpie/think/deep"] != "high" {
		t.Fatalf("no pick: deep %v", got)
	}
	// the pick lands; from then on every magpie-owned default is the
	// pick's answer (here both models' highest, high)
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	ms = readKimiTOML(t, path)["models"].(map[string]any)
	for k, v := range ms {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if e, ok := m["default_effort"].(string); ok && e != "high" {
			t.Fatalf("%s: default_effort %q after the pick, want high", k, e)
		}
	}
}

// The live failure itself: after the pick, a plain Sync (what the gateway
// loop runs, with no Apply in between) must still carry the pick into the
// tables — preferred comes from [thinking], read inside writeMagpie.
func TestKimiSyncCarriesThePickWithoutApply(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	// a stale record, as the live stash had
	wrote, _ := json.Marshal(map[string]map[string]string{
		`models."magpie/think/deep"`: {"default_effort": "low"},
		`models."magpie/think/wide"`: {"default_effort": "low"},
	})
	stash, _ := json.Marshal(map[string]string{"kimi:" + path + ":wrote": string(wrote)})
	if err := os.WriteFile(filepath.Join(home, ".config", "magpie", "stash.json"), stash, 0o644); err != nil {
		t.Fatal(err)
	}
	// the pick set the way the Agents page sets it
	if err := a.Field("effort").Set("high"); err != nil {
		t.Fatal(err)
	}
	// a later Sync alone: the pick still drives the tables
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	ms := readKimiTOML(t, path)["models"].(map[string]any)
	for k, v := range ms {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if e, ok := m["default_effort"].(string); ok && e != "high" {
			t.Fatalf("%s: default_effort %q after a Sync with the pick set, want high", k, e)
		}
	}
}

// k3.1-pro shape: efforts low/high/max, record high, pick max. The pick's
// answer (max) differs from the record: the write must take max. This is
// the live bug's sharpest case.
func TestKimiPickOverridesDifferingRecord(t *testing.T) {
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
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, "")
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	// record says magpie wrote the vendor fallback high (an older build)
	wrote, _ := json.Marshal(map[string]map[string]string{
		`models."magpie/think/k31"`: {"default_effort": "high"},
	})
	stash, _ := json.Marshal(map[string]string{"kimi:" + path + ":wrote": string(wrote)})
	if err := os.WriteFile(filepath.Join(home, ".config", "magpie", "stash.json"), stash, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	ms := readKimiTOML(t, path)["models"].(map[string]any)
	var e any
	for _, v := range ms {
		if m, ok := v.(map[string]any); ok {
			e = m["default_effort"]
		}
	}
	if e != "max" {
		t.Fatalf("default_effort %v after pick max, want max (the record's high is magpie's own, not the user's hand)", e)
	}
}

// The trigger the live box actually had: [thinking] effort = max already
// on disk, a differing record (high), and no Apply in this process — only
// a Sync, as SyncCatalog runs it. The tables must come to max.
func TestKimiSyncAloneAppliesTheOnDiskPick(t *testing.T) {
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
	path := filepath.Join(home, ".kimi-code", "config.toml")
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	// the pick from an earlier day, as the live config had it
	if err := a.Field("effort").Set("max"); err != nil {
		t.Fatal(err)
	}
	// the stale record from an older build
	wrote, _ := json.Marshal(map[string]map[string]string{
		`models."magpie/think/k31"`: {"default_effort": "high"},
	})
	stash, _ := json.Marshal(map[string]string{"kimi:" + path + ":wrote": string(wrote)})
	if err := os.WriteFile(filepath.Join(home, ".config", "magpie", "stash.json"), stash, 0o644); err != nil {
		t.Fatal(err)
	}
	// this process's only act: a Sync
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	ms := readKimiTOML(t, path)["models"].(map[string]any)
	var e any
	for _, v := range ms {
		if m, ok := v.(map[string]any); ok {
			e = m["default_effort"]
		}
	}
	if e != "max" {
		t.Fatalf("default_effort %v after a Sync with the pick on disk, want max", e)
	}
}
