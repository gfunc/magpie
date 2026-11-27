package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/provider"
)

// kimiPoolHome is a sandbox HOME with magpie's provider "think" and two of
// its models of different levels: deep (low, high, max) and wide (low,
// high), so a pool of both stops at high.
func kimiPoolHome(t *testing.T) (home, path string) {
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("KIMI_SHARE_DIR", "")
	t.Setenv("KIMI_CODE_HOME", "")
	if err := provider.Save(provider.Provider{ID: "think", Name: "Think", Chat: "https://example.test/v1", Key: "k", Models: []string{"deep", "wide"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("think", "https://example.test/v1", []catalog.Model{
		{ID: "deep", Efforts: []string{"low", "high", "max"}},
		{ID: "wide", Efforts: []string{"low", "high"}},
	}); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, "")
	return home, path
}

// kimiPooled is the Kimi Code agent of the home, connected on deep, with
// the subagents' pool of the models named (the first the default).
func kimiPooled(t *testing.T, home string, models ...string) *Agent {
	t.Helper()
	a := kimi(home)
	if err := a.Pick("model", "magpie/think/deep"); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("subagent_pool").Set(strings.Join(models, "\n")); err != nil {
		t.Fatal(err)
	}
	return a
}

// kimiSecEffort reads the default_effort of the [secondary_model] section in
// the config at path, "" when it holds none.
func kimiSecEffort(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Secondary map[string]any `toml:"secondary_model"`
	}
	if err := toml.Unmarshal(b, &c); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	e, _ := c.Secondary["default_effort"].(string)
	return e
}

// The subagents' level is the Agents-page pick: the pick is what the lead
// session resolves, ahead of every table's own default, and a subagent
// never reads it — it reads [secondary_model].default_effort, which magpie
// writes for it and records as its own.
func TestKimiSubagentEffortFollowsThePick(t *testing.T) {
	home, path := kimiPoolHome(t)
	a := kimiPooled(t, home, "magpie/think/deep")
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "max" {
		t.Fatalf("the subagents' level: %q, want the pick's max", got)
	}
	if r := kimiWrote(path)[kimiSecondary]["default_effort"]; r != "max" {
		t.Fatalf("the write was not recorded: %v", kimiWrote(path))
	}
}

// The pick is brought down to a level every model of the pool takes: Kimi
// Code refuses a section whose effort one of them can't take. Measured with
// real Kimi Code 2.7.2 sessions on 2026-11-27: a pool of two models, the
// default one taking low and high only, and "max" in
// [secondary_model].default_effort — the whole value refused, not clamped,
// and the subagent ran at that model's own table default ("low"). magpie
// writes the level the pool shares instead, so the value stays one Kimi
// takes.
func TestKimiSubagentEffortClampsToThePool(t *testing.T) {
	home, path := kimiPoolHome(t)
	a := kimiPooled(t, home, "magpie/think/wide", "magpie/think/deep")
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "high" {
		t.Fatalf("the subagents' level: %q, want the pool's high", got)
	}
	if r := kimiWrote(path)[kimiSecondary]["default_effort"]; r != "high" {
		t.Fatalf("the write was not recorded: %v", kimiWrote(path))
	}
}

// A pick the pool takes is written as it is, at the low end too.
func TestKimiSubagentEffortTakesALowPick(t *testing.T) {
	home, path := kimiPoolHome(t)
	a := kimiPooled(t, home, "magpie/think/wide", "magpie/think/deep")
	if err := a.Apply("effort", "low"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "low" {
		t.Fatalf("the subagents' level: %q, want the pick's low", got)
	}
}

// The pick cleared (Default) takes the value magpie wrote out of the
// section: the subagents run on their pool model's own default then.
func TestKimiSubagentEffortPickClearedRemovesTheKey(t *testing.T) {
	home, path := kimiPoolHome(t)
	a := kimiPooled(t, home, "magpie/think/deep")
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "max" {
		t.Fatalf("the subagents' level: %q, want the pick's max", got)
	}
	if err := a.Apply("effort", ""); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "" {
		t.Fatalf("the pick cleared left the level at %q", got)
	}
	if r := kimiWrote(path)[kimiSecondary]; r != nil {
		t.Fatalf("the record of the value magpie took out is still there: %v", r)
	}
}

// A level the user set by hand is theirs: it stands against the pick
// until they clear it, and the pick's value is written again then.
func TestKimiSubagentEffortKeepsAHandSetValue(t *testing.T) {
	home, path := kimiPoolHome(t)
	a := kimiPooled(t, home, "magpie/think/deep")
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("subagent_effort").Set("high"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "high" {
		t.Fatalf("the hand-set level: %q, want high", got)
	}
	// cleared (Default): the pick's value is written again
	if err := a.Field("subagent_effort").Set(""); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "max" {
		t.Fatalf("after the hand-set level was cleared: %q, want the pick's max", got)
	}
}

// Without a pool of its own the section says nothing: a subagent runs on
// the lead's model and effort, so magpie writes nothing there.
func TestKimiSubagentEffortNoPoolWritesNothing(t *testing.T) {
	home, path := kimiPoolHome(t)
	a := kimi(home)
	if err := a.Pick("model", "magpie/think/deep"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "" {
		t.Fatalf("no pool, a level was written: %q", got)
	}
	if strings.Contains(readFile(path), "secondary_model") {
		t.Fatalf("no pool, a section was written:\n%s", readFile(path))
	}
}

// With the pin on the subagents are at their ceiling as a model is: the
// highest level every model of the pool takes, whatever the pick says.
func TestKimiSubagentEffortPinOnTakesThePoolsCeiling(t *testing.T) {
	home, path := kimiPoolHome(t)
	a := kimiPooled(t, home, "magpie/think/wide", "magpie/think/deep")
	pinOn(t)
	if err := a.Apply("effort", "low"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "high" {
		t.Fatalf("the subagents' level: %q, want the pool's ceiling high", got)
	}
}

// A [secondary_model] with a default_model and no pool of its own is a
// pool of one: Kimi Code's own resolveSubagentModelPool takes the
// section's models, else default_model's alone, else none. The subagents
// run on that model, so magpie writes their level.
func TestKimiSubagentEffortWithADefaultModelAlone(t *testing.T) {
	home, path := kimiPoolHome(t)
	a := kimi(home)
	if err := a.Pick("model", "magpie/think/deep"); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("subagent_model").Set("magpie/think/deep"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "max" {
		t.Fatalf("the subagents' level: %q, want the pick's max", got)
	}
}

// The Notice about a section-wide level is for a value the user set by
// hand: while magpie keeps the key in step with the pick, the subagents
// run at the pick and there is nothing to say.
func TestKimiSubagentEffortNoticeOnlyForAHandSetValue(t *testing.T) {
	home, _ := kimiPoolHome(t)
	a := kimiPooled(t, home, "magpie/think/deep")
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if n := a.Notice(); strings.Contains(n, "secondary_model") {
		t.Fatalf("a level magpie keeps in step with the pick was announced: %q", n)
	}
	if err := a.Field("subagent_effort").Set("high"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if n := a.Notice(); !strings.Contains(n, "secondary_model") {
		t.Fatalf("a hand-set level was not announced: %q", n)
	}
}

// Thinking off is a pick too: the Agents page shows off while [thinking]
// says enabled = false, and Kimi Code starts the lead session with thinking
// off. The subagents take the pick through their own key, so they take off
// the same way, and not the level the off left standing in [thinking] for
// the next on.
func TestKimiSubagentEffortFollowsThinkingOff(t *testing.T) {
	home, path := kimiPoolHome(t)
	a := kimiPooled(t, home, "magpie/think/deep", "magpie/think/wide")
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "high" {
		t.Fatalf("pick max: the subagents' level %q, want the pool's high", got)
	}
	if got := a.Field("effort").Get(); got != "max" {
		t.Fatalf("the page shows %q, want max", got)
	}
	if err := a.Apply("effort", "off"); err != nil {
		t.Fatal(err)
	}
	if got := a.Field("effort").Get(); got != "off" {
		t.Fatalf("the page shows %q, want off", got)
	}
	if got := kimiSecEffort(t, path); got != "off" {
		t.Fatalf("thinking off: the subagents' level %q, want off", got)
	}
	// on again at the level the off left standing
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "high" {
		t.Fatalf("back on: the subagents' level %q, want the pool's high", got)
	}
}

// kimiUserPoolHome is kimiPoolHome with a model of the user's own in the
// config, a sign-in table that stays when magpie is taken out.
func kimiUserPoolHome(t *testing.T) (home, path string) {
	home, path = kimiPoolHome(t)
	writeFile(t, path, strings.Replace(kimiSignInConfig, `"low", "high", "max"`, `"low", "high", "max"`, 1))
	return home, path
}

// Taking magpie out of Kimi Code takes out the level magpie wrote for its
// subagents, as it takes out its model tables, and not a value the user set
// by hand. A pool model of the user's own stays and still takes a level, so
// the pool alone does not take magpie's with it.
func TestKimiSubagentEffortGoesWithMagpie(t *testing.T) {
	home, path := kimiUserPoolHome(t)
	a := kimi(home)
	if err := a.Pick("model", "magpie/think/deep"); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("subagent_pool").Set("magpie/think/deep\nkimi-code/k3.1-pro"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("effort", "low"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "low" {
		t.Fatalf("wired: the subagents' level %q, want the pick's low", got)
	}
	// default: Kimi Code as it was, magpie out
	if err := a.Pick("model", ""); err != nil {
		t.Fatal(err)
	}
	sec, _, ok := kimiDoc(path)
	if !ok || len(sec.Pool) != 1 || !hasKey(sec.Pool, "kimi-code/k3.1-pro") {
		t.Fatalf("the user's own pool model went with magpie: %+v", sec.Pool)
	}
	if got := kimiSecEffort(t, path); got != "" {
		t.Fatalf("magpie's level stayed behind: %q", got)
	}
	// back on magpie: the subagents follow the pick again
	if err := a.Pick("model", "magpie/think/deep"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("effort", "high"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "high" {
		t.Fatalf("reconnected: the subagents' level %q, want the pick's high", got)
	}
}

// A level the user set by hand for the subagents is theirs to keep when
// magpie is taken out: only the one magpie wrote goes.
func TestKimiSubagentEffortHandSetStaysWhenMagpieGoes(t *testing.T) {
	home, path := kimiUserPoolHome(t)
	a := kimi(home)
	if err := a.Pick("model", "magpie/think/deep"); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("subagent_pool").Set("magpie/think/deep\nkimi-code/k3.1-pro"); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("subagent_effort").Set("medium"); err == nil {
		t.Fatal("medium: the pool does not take it")
	}
	if err := a.Field("subagent_effort").Set("low"); err != nil {
		t.Fatal(err)
	}
	if err := a.Pick("model", ""); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "low" {
		t.Fatalf("the user's level went with magpie: %q", got)
	}
}

// A pick that names no level magpie knows, "none" among them, is no pick
// for the subagents: magpie writes no level it cannot rank into their key.
// Off is a pick: a pool that takes it gets it, and one with a model that
// always thinks, which takes none, gets the pool's lowest level in its
// place.
func TestKimiSubagentEffortIgnoresAPickOfNoKnownLevel(t *testing.T) {
	home, path := kimiPoolHome(t)
	a := kimiPooled(t, home, "magpie/think/deep", "magpie/think/wide")
	for _, pick := range []string{"none", "ludicrous"} {
		if err := a.Apply("effort", pick); err != nil {
			t.Fatal(err)
		}
		if got := kimiSecEffort(t, path); got != "" {
			t.Fatalf("pick %q: the subagents' level %q, want none written", pick, got)
		}
	}
	if err := a.Apply("effort", "off"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "off" {
		t.Fatalf("off: the subagents' level %q, want off", got)
	}
}

// kimiAlwaysHome is kimiPoolHome with a third model that always thinks: a
// pool holding it takes no off.
func kimiAlwaysHome(t *testing.T) (home, path string) {
	home, path = kimiPoolHome(t)
	if err := catalog.SaveLive("think", "https://example.test/v1", []catalog.Model{
		{ID: "deep", Efforts: []string{"low", "high", "max"}},
		{ID: "wide", Efforts: []string{"low", "high"}},
		{ID: "always", Efforts: []string{"low", "high"}, AlwaysThinks: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "think", Name: "Think", Chat: "https://example.test/v1", Key: "k", Models: []string{"deep", "wide", "always"}}); err != nil {
		t.Fatal(err)
	}
	return home, path
}

// Thinking off, for a pool with a model that always thinks: Kimi Code
// refuses off there, so the subagents get the pool's lowest level.
func TestKimiSubagentEffortOffWhereAPoolModelAlwaysThinks(t *testing.T) {
	home, path := kimiAlwaysHome(t)
	a := kimiPooled(t, home, "magpie/think/deep", "magpie/think/always")
	if err := a.Apply("effort", "off"); err != nil {
		t.Fatal(err)
	}
	if got := kimiSecEffort(t, path); got != "low" {
		t.Fatalf("off, a model that always thinks in the pool: the subagents' level %q, want the pool's lowest, low", got)
	}
}
