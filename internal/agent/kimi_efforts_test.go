package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/provider"
)

// #333: the new Kimi Code is told each thinking model's levels
// (support_efforts, default_effort high where there is one), or its picker
// has thinking on and off only; none is its own off, so it isn't a level,
// and kimi-cli, which knows neither key, gets neither.
func TestKimiEfforts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("KIMI_SHARE_DIR", "")
	t.Setenv("KIMI_CODE_HOME", "")
	if err := provider.Save(provider.Provider{
		ID: "think", Name: "Think", Chat: "https://example.test/v1", Key: "key",
		Models: []string{"deep", "wide", "plain"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("think", "https://example.test/v1", []catalog.Model{
		{ID: "deep", Efforts: []string{"none", "high", "max"}},
		{ID: "wide", Efforts: []string{"low", "medium"}},
		{ID: "plain"},
	}); err != nil {
		t.Fatal(err)
	}
	type model struct {
		Capabilities   []string `toml:"capabilities"`
		SupportEfforts []string `toml:"support_efforts"`
		DefaultEffort  string   `toml:"default_effort"`
	}
	read := func(path string) map[string]model {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var c struct{ Models map[string]model }
		if err := toml.Unmarshal(b, &c); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		return c.Models
	}

	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, "")
	if err := kimi(home).Field("model").Set("magpie/think/deep"); err != nil {
		t.Fatal(err)
	}
	ms := read(path)
	for k, want := range map[string]model{
		"magpie/think/deep":  {[]string{"thinking", "tool_use"}, []string{"high", "max"}, "high"},
		"magpie/think/wide":  {[]string{"thinking", "tool_use"}, []string{"low", "medium"}, ""},
		"magpie/think/plain": {[]string{"tool_use"}, nil, ""},
	} {
		if got := ms[k]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v, want %+v", k, got, want)
		}
	}

	os.RemoveAll(filepath.Join(home, ".kimi-code"))
	legacy := filepath.Join(home, ".kimi", "config.toml")
	writeFile(t, legacy, "")
	if err := kimi(home).Field("model").Set("magpie/think/deep"); err != nil {
		t.Fatal(err)
	}
	if got := read(legacy)["magpie/think/deep"]; got.SupportEfforts != nil || got.DefaultEffort != "" {
		t.Errorf("kimi-cli's: %+v", got)
	}
}

// @apple_test_src on X: Kimi Code had no thinking level to pick where
// every other agent had one. It is Kimi Code's [thinking] (enabled,
// effort), as its own /model saves it, the rest of the table kept; the
// levels are the current model's; kimi-cli has default_thinking only.
func TestKimiThinkingField(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("KIMI_SHARE_DIR", "")
	t.Setenv("KIMI_CODE_HOME", "")
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, `default_model = "deep"

[models.deep]
provider = "p"
model = "deep"
capabilities = ["thinking"]
support_efforts = ["low", "high", "max"]
default_effort = "high"

[models.plain]
provider = "p"
model = "plain"

[models.bare]
provider = "p"
model = "bare"
capabilities = ["thinking"]

[models.always]
provider = "p"
model = "always"
support_efforts = ["low"]

[models.always.overrides]
capabilities = ["thinking", "always_thinking"]
support_efforts = ["medium", "max"]

[thinking]
keep = "all"
`)
	f := kimi(home).Field("effort")
	if f == nil {
		t.Fatal("Kimi Code has no thinking field")
	}
	type th struct {
		Enabled *bool  `toml:"enabled"`
		Effort  string `toml:"effort"`
		Keep    string `toml:"keep"`
	}
	read := func() *th {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var c struct{ Thinking *th }
		if err := toml.Unmarshal(b, &c); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		return c.Thinking
	}
	yes, no := true, false
	for _, step := range []struct {
		set  string
		want th
	}{
		{"max", th{&yes, "max", "all"}},
		{"off", th{&no, "max", "all"}},
		{"low", th{&yes, "low", "all"}},
		{"", th{nil, "", "all"}},
	} {
		if err := f.Set(step.set); err != nil {
			t.Fatal(err)
		}
		if got := read(); got == nil || !reflect.DeepEqual(*got, step.want) {
			t.Fatalf("set %q: [thinking] %+v, want %+v", step.set, got, step.want)
		}
		if got := f.Get(); got != step.set {
			t.Errorf("set %q, read back %q", step.set, got)
		}
	}
	// with nothing of the user's beside it, Default takes the table out
	writeFile(t, path, "[models.deep]\nprovider = \"p\"\n\n[thinking]\nenabled = false\n")
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != nil {
		t.Errorf("Default left [thinking] %+v", *got)
	}
	if got := f.Get(); got != "" {
		t.Errorf("no [thinking] reads %q", got)
	}

	writeFile(t, path, `[models.deep]
capabilities = ["thinking"]
support_efforts = ["low", "high", "max"]
default_effort = "high"

[models.plain]
model = "plain"

[models.bare]
capabilities = ["thinking"]

[models.always]
support_efforts = ["low"]

[models.always.overrides]
capabilities = ["thinking", "always_thinking"]
support_efforts = ["medium", "max"]
`)
	vals := func(opts []Option) []string {
		out := []string{}
		for _, o := range opts {
			out = append(out, o.Value+"/"+o.Takes)
		}
		return out
	}
	for model, want := range map[string][]string{
		"deep":    {"/high", "off/", "low/", "high/", "max/"},
		"plain":   {},
		"bare":    {"off/"},
		"always":  {"medium/", "max/"},
		"unknown": {"off/", "low/", "medium/", "high/"},
	} {
		if got := vals(f.Options(map[string]string{"model": model})); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: levels %v, want %v", model, got, want)
		}
	}

	// kimi-cli: default_thinking, on or off
	os.RemoveAll(filepath.Join(home, ".kimi-code"))
	legacy := filepath.Join(home, ".kimi", "config.toml")
	writeFile(t, legacy, "default_model = \"x\"\n")
	f = kimi(home).Field("effort")
	if got := vals(f.Options(nil)); !reflect.DeepEqual(got, []string{"off/", "on/"}) {
		t.Errorf("kimi-cli levels %v", got)
	}
	for _, step := range []struct{ set, line string }{
		{"on", "default_thinking = true"},
		{"off", "default_thinking = false"},
		{"", ""},
	} {
		if err := f.Set(step.set); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(legacy)
		if step.line != "" && !strings.Contains(string(b), step.line) || step.line == "" && strings.Contains(string(b), "default_thinking") {
			t.Errorf("set %q: %s", step.set, b)
		}
		if got := f.Get(); got != step.set {
			t.Errorf("kimi-cli: set %q, read back %q", step.set, got)
		}
	}
}

const kimiThinkingConfig = `default_model = "kimi-code/k3"
telemetry = false

[thinking]
enabled = false
keep = "all"

[models."kimi-code/k3"]
provider = "managed:kimi-code"
model = "k3"
max_context_size = 1048576
support_efforts = ["low", "high", "max"]
default_effort = "high"

[models."kimi-code/k3".overrides]
default_effort = "low"

[providers."managed:kimi-code"]
type = "kimi"
base_url = "https://api.kimi.com/coding/v1"
api_key = ""

[secondary_model]
default_model = "kimi-code/k3"
default_effort = "low"
force = true

[loop_control]
max_attempts_per_step = 10
`

// The rule in one place: a table holds the model's own default where the
// model offers it, else high where the model offers that, else no key, so
// Kimi Code starts on the middle level. With the pin on every table is at
// its own strongest level. The Agents-page pick is not an input: Kimi Code
// resolves it ahead of every table's own default.
func TestKimiDefaultEffort(t *testing.T) {
	effortSettingsHome(t)
	deep := []string{"none", "low", "high", "max"}     // glm's shape: a max on top
	wide := []string{"low", "medium", "high"}          // wide's medium: a vendor default
	grok := []string{"low", "medium", "high", "xhigh"} // grok's: no max, xhigh on top
	ultra := []string{"low", "high", "ultra"}          // gpt-6.1-sol's: a level past max
	odd := []string{"turbo", "ludicrous"}              // words EffortRank doesn't know
	for _, tc := range []struct {
		name    string
		vendor  string
		efforts []string
		out     string
	}{
		{"vendor offered", "medium", wide, "medium"},
		{"vendor offered among more", "max", deep, "max"},
		{"vendor not offered, high is", "max", []string{"low", "medium", "high"}, "high"},
		{"neither offered", "max", []string{"low", "medium"}, ""},
		{"no vendor default, high is", "", []string{"low", "high"}, "high"},
		{"no vendor default, no high", "", []string{"low", "medium"}, ""},
		{"a vendor word nothing ranks is taken as it is", "turbo", []string{"high", "turbo"}, "turbo"},
		{"empty efforts", "high", nil, ""},
	} {
		if got := kimiDefaultEffort(tc.vendor, tc.efforts); got != tc.out {
			t.Errorf("%s: kimiDefaultEffort(%q, %v) = %q, want %q", tc.name, tc.vendor, tc.efforts, got, tc.out)
		}
	}
	// the pin on: every table at its own strongest level, whatever the
	// vendor's default is
	pinOn(t)
	for _, tc := range []struct {
		name    string
		efforts []string
		out     string
	}{
		{"deep", deep, "max"},
		{"wide", wide, "high"},
		{"grok", grok, "xhigh"},
		{"ultra", ultra, "ultra"},
		{"none is never a level", []string{"none"}, ""},
		{"words nothing ranks", odd, ""},
	} {
		if got := kimiDefaultEffort("medium", tc.efforts); got != tc.out {
			t.Errorf("pin on, %s: kimiDefaultEffort = %q, want %q", tc.name, got, tc.out)
		}
	}
}

// kimiHighestEffort is levels' strongest as catalog.EffortRank orders the
// words, a vendor's own word for one ranking as the level it names, and
// "" when none of them ranks.
func TestKimiHighestEffort(t *testing.T) {
	for _, tc := range []struct {
		levels []string
		out    string
	}{
		{[]string{"none", "low", "high", "max"}, "max"},
		{[]string{"low", "medium", "high", "xhigh"}, "xhigh"},
		{[]string{"low", "medium"}, "medium"},
		{[]string{"high"}, "high"},
		// EffortRank's order: max above ultra's would be wrong either way
		// round; the words below are ordered as EffortRank has them
		{[]string{"ultra", "low"}, "ultra"},
		{[]string{"minimal", "low", "medium"}, "medium"},
		// a vendor's own word for a level (Trae's light, EffortAliases)
		// ranks as the level it names: light is that list's low
		{[]string{"light", "low"}, "light"},
		{[]string{"extra_high", "high"}, "extra_high"},
		// none is excluded: it is thinking off, not a level
		{[]string{"none"}, ""},
		{[]string{"none", "none"}, ""},
		// words EffortRank doesn't know don't rank
		{[]string{"turbo", "ludicrous"}, ""},
		{nil, ""},
	} {
		if got := kimiHighestEffort(tc.levels); got != tc.out {
			t.Errorf("kimiHighestEffort(%v) = %q, want %q", tc.levels, got, tc.out)
		}
	}
}

// The Agents-page pick is Kimi Code's own: it resolves the pick ahead of
// every model's default_effort, so a table that holds the model's own
// default follows the pick without magpie copying it into the table.
// While the agent is wired the tables keep the model's own default, and
// the pick lands in [thinking] alone. A hand-set value keeps its standing
// (kimiOwn), and one the model does not offer gives way to magpie's own.
func TestKimiPickStaysOutOfTheModelTables(t *testing.T) {
	home := keptEffortHome(t)
	// a second provider for the shapes the default helper lacks: glm
	// (deep, max under high) and grok (wide, xhigh with no max)
	if err := provider.Save(provider.Provider{ID: "brain", Name: "Brain", Chat: "https://example.test/v1", Key: "k", Models: []string{"glm", "grok"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("brain", "https://example.test/v1", []catalog.Model{
		{ID: "glm", Efforts: []string{"none", "low", "high", "max"}},
		{ID: "grok", Efforts: []string{"low", "medium", "high", "xhigh"}, DefaultEffort: "xhigh"},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, "")
	a := kimi(home)
	if err := a.Pick("model", "magpie/brain/glm"); err != nil {
		t.Fatal(err)
	}
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
	// no pick yet: the model's own default (glm none → high, grok its
	// vendor xhigh)
	if got := efforts(); got["magpie/brain/glm"] != "high" || got["magpie/brain/grok"] != "xhigh" {
		t.Fatalf("no pick: %v", got)
	}
	// a pick does not move the tables
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/brain/glm"] != "high" || got["magpie/brain/grok"] != "xhigh" {
		t.Fatalf("pick max reached the tables: %v", got)
	}
	// the pick itself is Kimi Code's own, in [thinking]
	if v, _ := edit.GetTOMLTable(path, "thinking"); v["effort"] != "max" {
		t.Fatalf("the pick was not kept as picked: %v", v)
	}
	if err := a.Apply("effort", "low"); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/brain/glm"] != "high" || got["magpie/brain/grok"] != "xhigh" {
		t.Fatalf("pick low reached the tables: %v", got)
	}
	// back to default: the tables are as they were
	if err := a.Pick("effort", ""); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/brain/glm"] != "high" || got["magpie/brain/grok"] != "xhigh" {
		t.Fatalf("pick cleared: %v", got)
	}
	// a hand-set default stays while the model offers it, and one the
	// model does not offer gives way to magpie's own
	a = kimi(home)
	if err := edit.SetTOMLKey(path, `models."magpie/brain/grok"`, "default_effort", "medium"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/brain/grok"] != "medium" {
		t.Fatalf("hand-set in-levels default replaced: %v", got)
	}
	if err := edit.SetTOMLKey(path, `models."magpie/brain/grok"`, "default_effort", "ludicrous"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/brain/grok"] != "xhigh" {
		t.Fatalf("hand-set out-of-levels default kept: %v", got)
	}
}

// A table holding the model's vendor default with no record of it (dev's
// gpt-6.1-sol kept ultra through a sync: magpie wrote that ultra before it
// kept records, and the record-less read mistook it for the user's
// hand-set) is magpie's own again: the record is kept and the vendor's
// default is written. A value that is neither high nor the vendor default
// is the user's and stays.
func TestKimiUnrecordedVendorDefaultIsMagpiesOwn(t *testing.T) {
	home := keptEffortHome(t)
	if err := provider.Save(provider.Provider{ID: "uco", Name: "UCO", Chat: "https://example.test/v1", Key: "k", Models: []string{"gpt-6.1-sol", "gpt-6.1-terra"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("uco", "https://example.test/v1", []catalog.Model{
		{ID: "gpt-6.1-sol", Efforts: []string{"low", "high", "ultra", "max"}, DefaultEffort: "ultra"},
		{ID: "gpt-6.1-terra", Efforts: []string{"low", "high", "ultra", "max"}, DefaultEffort: "ultra"},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, "")
	a := kimi(home)
	if err := a.Pick("model", "magpie/uco/gpt-6.1-sol"); err != nil {
		t.Fatal(err)
	}
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
	// the vendor's default is what a table holds
	if got := efforts(); got["magpie/uco/gpt-6.1-sol"] != "ultra" {
		t.Fatalf("the vendor's default: %v", got)
	}
	// a table whose record was lost (a magpie from before records kept),
	// holding a value the rule would write, is magpie's own again: the
	// vendor's default is written and the record is kept
	edit.SetTOMLKey(path, `models."magpie/uco/gpt-6.1-sol"`, "default_effort", "high")
	wrote := kimiWrote(path)
	delete(wrote, `models."magpie/uco/gpt-6.1-sol"`)
	b, err := json.Marshal(wrote)
	if err != nil {
		t.Fatal(err)
	}
	stash(map[string]string{kimiWroteKey(path): string(b)})
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/uco/gpt-6.1-sol"] != "ultra" {
		t.Fatalf("unrecorded value the rule writes: %v", got)
	}
	if r := kimiWrote(path)[`models."magpie/uco/gpt-6.1-sol"`]["default_effort"]; r != "ultra" {
		t.Fatalf("the re-owned table was not recorded: %v", kimiWrote(path))
	}
	// a hand-set value that is neither high nor the vendor default is the
	// user's, record or none, and stays while the model has it
	edit.SetTOMLKey(path, `models."magpie/uco/gpt-6.1-terra"`, "default_effort", "low")
	wrote = kimiWrote(path)
	delete(wrote, `models."magpie/uco/gpt-6.1-terra"`)
	b, err = json.Marshal(wrote)
	if err != nil {
		t.Fatal(err)
	}
	stash(map[string]string{kimiWroteKey(path): string(b)})
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/uco/gpt-6.1-terra"] != "low" {
		t.Fatalf("hand-set value with no record replaced: %v", got)
	}
	// and with the record kept, the value the record names is magpie's own
	edit.SetTOMLKey(path, `models."magpie/uco/gpt-6.1-sol"`, "default_effort", "ultra")
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/uco/gpt-6.1-sol"] != "ultra" {
		t.Fatalf("recorded value replaced: %v", got)
	}
}

// A table rests at the model's own default: the vendor's where the model
// offers it, else high where the model offers that, else no key at all, so
// Kimi Code starts on the middle level. The pick is Kimi Code's own: it
// lands in [thinking] and moves nothing in the tables.
func TestKimiTablesRestAtTheModelsOwnDefault(t *testing.T) {
	home := keptEffortHome(t)
	if err := provider.Save(provider.Provider{ID: "rest", Name: "Rest", Chat: "https://example.test/v1", Key: "k", Models: []string{"plain", "top", "mid", "bare"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("rest", "https://example.test/v1", []catalog.Model{
		{ID: "plain", Efforts: []string{"low", "high", "max"}, DefaultEffort: "high"},
		{ID: "top", Efforts: []string{"low", "high", "max"}, DefaultEffort: "max"},
		{ID: "mid", Efforts: []string{"low", "medium", "high"}},
		{ID: "bare", Efforts: []string{"low", "medium"}},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, "")
	a := kimi(home)
	if err := a.Pick("model", "magpie/rest/plain"); err != nil {
		t.Fatal(err)
	}
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
	// the vendor's own default, high where the model has it, and no key
	// where it has neither
	if got := efforts(); got["magpie/rest/plain"] != "high" || got["magpie/rest/top"] != "max" || got["magpie/rest/mid"] != "high" {
		t.Fatalf("the model's own default: %v", got)
	}
	if _, ok := efforts()["magpie/rest/bare"]; ok {
		t.Fatalf("a model with no high got a default: %v", efforts())
	}
	// the pick is Kimi Code's own: it lands in [thinking] and moves
	// nothing in the tables
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/rest/plain"] != "high" || got["magpie/rest/top"] != "max" || got["magpie/rest/mid"] != "high" {
		t.Fatalf("the pick moved the tables: %v", got)
	}
	if v, _ := edit.GetTOMLTable(path, "thinking"); v["effort"] != "max" {
		t.Fatalf("the pick was not kept as picked: %v", v)
	}
}

// The pick alone, on an agent not wired (default_model the user's own),
// changes nothing in the model tables: only [thinking].effort moves.
func TestKimiEffortPickWhileNotWiredChangesNoTables(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, kimiThinkingConfig)
	a := kimi(home)
	was := readFile(path)
	if err := a.Pick("effort", "max"); err != nil {
		t.Fatal(err)
	}
	now := readFile(path)
	if now == was {
		t.Fatalf("pick wrote nothing at all:\n%s", now)
	}
	strip := func(s string) string {
		t.Helper()
		var c struct {
			Thinking map[string]any `toml:"thinking"`
		}
		if err := toml.Unmarshal([]byte(s), &c); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(s, "\n")
		var out []string
		for _, l := range lines {
			// a level picked turns thinking on (enabled), as Kimi
			// Code's own picker does; off would leave it as it was
			if !strings.HasPrefix(l, "effort") && !strings.HasPrefix(l, "enabled") {
				out = append(out, l)
			}
		}
		return strings.Join(out, "\n")
	}
	if stripped := strip(now); stripped != strip(was) {
		t.Fatalf("pick while not wired changed more than [thinking]:\n%s", now)
	}
	// the thinking table alone changed: the pick turned it on at its level
	if now != strings.Replace(was, "enabled = false\n", "enabled = true\n", 1) &&
		!strings.Contains(now, "effort = \"max\"") {
		t.Fatalf("pick while not wired rewrote the config:\n%s", now)
	}
}

// Disconnect clears the model field before the effort field, so the
// effort field's Set("") finds usesMagpie false and no magpie table is
// recreated on the way out.
func TestKimiDisconnectRecreatesNoTables(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, kimiThinkingConfig)
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if got := readKimiTOML(t, path)["models"]; len(got.(map[string]any)) == 0 {
		t.Fatalf("connect wrote no model tables")
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFile(path), "magpie/") {
		t.Fatalf("disconnect recreated magpie tables:\n%s", readFile(path))
	}
	// the pick reads off now (upstream's [thinking] semantics: Disconnect
	// cleared the level, enabled=false stays as Kimi Code's own off)
	if !a.Wired() && a.Values()["effort"] != "off" {
		t.Fatalf("disconnect left the effort pick: %v", a.Values())
	}
}

// A stash the write can't record rolls the whole takeover back: the
// config file and the wrote record succeed or fail together, or the next
// sync would read magpie's own default as hand-set and stop following.
func TestKimiFailedStashWriteRollsBack(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, "")
	a := kimi(home)
	if err := a.Pick("model", "magpie/think/deep"); err != nil {
		t.Fatal(err)
	}
	was := readFile(path)
	// a directory where the stash would be written fails the write
	st := stashPath()
	os.RemoveAll(st)
	if err := os.MkdirAll(st, 0o755); err != nil {
		t.Fatal(err)
	}
	defer func() { os.Remove(st) }()
	if err := a.Apply("effort", "max"); err == nil {
		t.Fatal("a stash that can't be written took the agent over anyway")
	}
	if now := readFile(path); now != was {
		t.Fatalf("a failed stash write changed config.toml:\n%s", now)
	}
}

// A section-wide [secondary_model].default_effort comes before every
// per-table default (Kimi Code's own resolution), so magpie never writes
// it and its Notice names it while one is set: the user knows the
// per-table defaults it writes are inert until it is taken out.
func TestKimiSectionWideEffortNotice(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, kimiThinkingConfig) // has [secondary_model] default_effort = "low"
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if n := a.Notice(); !strings.Contains(n, "secondary_model") || !strings.Contains(n, "low") {
		t.Fatalf("no notice of the section-wide effort: %q", n)
	}
	if sec, err := edit.GetTOMLTable(path, "secondary_model"); err != nil || sec["default_effort"] != "low" {
		t.Fatalf("section-wide effort changed: %v %v", sec, err)
	}
	// gone, the Notice goes and the per-table defaults are what count
	if err := edit.DelTOMLKey(path, "secondary_model", "default_effort"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if n := a.Notice(); strings.Contains(n, "secondary_model") {
		t.Fatalf("notice outlived the section-wide effort: %q", n)
	}
}

// A table magpie wrote under the old rule holds the copied pick. With
// the pin off the first sync writes the model's own default over it and
// keeps the record in step, and a value the user set by hand is theirs
// and stays.
func TestKimiPinOffRevertsWhatThePickCopied(t *testing.T) {
	home := keptEffortHome(t)
	// a model whose vendor default is high and whose own top is max
	if err := provider.Save(provider.Provider{ID: "old", Name: "Old", Chat: "https://example.test/v1", Key: "k", Models: []string{"k31"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("old", "https://example.test/v1", []catalog.Model{
		{ID: "k31", Efforts: []string{"low", "high", "max"}, DefaultEffort: "high"},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, "")
	a := kimi(home)
	if err := a.Pick("model", "magpie/old/k31"); err != nil {
		t.Fatal(err)
	}
	table := `models."magpie/old/k31"`
	// the old rule's bytes: the pick copied into the table, the record
	// of it, and the pick itself in [thinking]
	if err := edit.SetTOMLKey(path, table, "default_effort", "max"); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetTOMLKey(path, "thinking", "effort", "max"); err != nil {
		t.Fatal(err)
	}
	wrote, _ := json.Marshal(map[string]map[string]string{table: {"default_effort": "max"}})
	stash(map[string]string{kimiWroteKey(path): string(wrote)})
	// the first sync with the pin off: the model's own default is written
	// over the copied pick, and the record follows it
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := kimiTableEffort(t, path, "magpie/old/k31"); got != "high" {
		t.Fatalf("the copied pick was not reverted: %q", got)
	}
	if got := kimiWrote(path)[table]["default_effort"]; got != "high" {
		t.Fatalf("the record was not updated: %v", kimiWrote(path))
	}
	// the pick itself is Kimi Code's own and is left as it is
	if v, _ := edit.GetTOMLTable(path, "thinking"); v["effort"] != "max" {
		t.Fatalf("the pick was changed: %v", v)
	}
	// a value the user set by hand is theirs and stays
	if err := edit.SetTOMLKey(path, table, "default_effort", "low"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := kimiTableEffort(t, path, "magpie/old/k31"); got != "low" {
		t.Fatalf("the hand-set value was replaced: %q", got)
	}
}

func readKimiTOML(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := toml.Unmarshal(b, &c); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	return c
}

// kimiTableEffort reads the default_effort of one model table in the
// config at path, "" when it holds none.
func kimiTableEffort(t *testing.T, path, model string) string {
	t.Helper()
	ms := readKimiTOML(t, path)["models"].(map[string]any)
	m, ok := ms[model].(map[string]any)
	if !ok {
		t.Fatalf("no table for %s in %s", model, path)
	}
	e, _ := m["default_effort"].(string)
	return e
}
