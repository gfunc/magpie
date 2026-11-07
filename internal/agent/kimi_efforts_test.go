package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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

func TestKimiThinkingEffort(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, kimiThinkingConfig)
	a := kimi(home)
	f := a.Field("effort")
	if f == nil {
		t.Fatal("modern Kimi Code has no effort field")
	}
	if f.Label != "thinking" || a.Field("thinking") != f || f.Quiet || f.Live {
		t.Fatalf("thinking field: %+v", f)
	}
	levels := []string{"", "low", "medium", "high", "xhigh", "max"}
	var offered []string
	for _, o := range f.Options(a.Values()) {
		offered = append(offered, o.Value)
	}
	if !reflect.DeepEqual(offered, levels) {
		t.Fatalf("thinking levels: %v, want %v", offered, levels)
	}
	if opts := f.Options(a.Values()); opts[0].Note != "default" {
		t.Fatalf("default effort is not labelled for the TUI: %+v", opts[0])
	}
	was := readKimiTOML(t, path)
	if got := f.Get(); got != "" {
		t.Fatalf("initial effort: %q", got)
	}
	for _, level := range levels[1:] {
		if err := a.Apply("thinking", level); err != nil {
			t.Fatal(err)
		}
		if got := kimi(home).Field("effort").Get(); got != level {
			t.Fatalf("read effort: %q, want %q", got, level)
		}
		now := readKimiTOML(t, path)
		thinking := now["thinking"].(map[string]any)
		if got := thinking["effort"]; got != level {
			t.Fatalf("thinking.effort: %v, want %q", got, level)
		}
		delete(thinking, "effort")
		if !reflect.DeepEqual(now, was) {
			t.Fatalf("setting %s changed other preferences:\n%s", level, readFile(path))
		}
	}
	if err := a.Pick("effort", ""); err != nil {
		t.Fatal(err)
	}
	if got := f.Get(); got != "" {
		t.Fatalf("reset effort: %q", got)
	}
	if now := readKimiTOML(t, path); !reflect.DeepEqual(now, was) {
		t.Fatalf("reset changed other preferences:\n%s", readFile(path))
	}
}

func TestKimiThinkingEffortRejectsInvalid(t *testing.T) {
	for _, exists := range []bool{false, true} {
		for _, via := range []string{"apply", "pick"} {
			for _, value := range []string{"auto", "off", "none", "minimal", "MAX", "default", "--help", "magpie", "magpie/think/deep"} {
				t.Run(via+"/"+value+"/existing="+strconv.FormatBool(exists), func(t *testing.T) {
					home := keptEffortHome(t)
					a := kimi(home)
					if exists {
						writeFile(t, a.Path, "[thinking]\nenabled = false\neffort = \"low\"\nkeep = \"all\"\n")
					}
					was := readFile(a.Path)
					set := a.Apply
					if via == "pick" {
						set = a.Pick
					}
					if err := set("effort", value); err == nil {
						t.Fatalf("took invalid thinking effort %q", value)
					}
					if now := readFile(a.Path); now != was {
						t.Fatalf("invalid effort %q changed config:\n%s", value, now)
					}
					if _, err := os.Stat(a.Path); !exists && !os.IsNotExist(err) {
						t.Fatalf("invalid effort created config: %v", err)
					}
				})
			}
		}
	}
}

func TestKimiThinkingEffortLifecycle(t *testing.T) {
	for _, choice := range []string{"max", ""} {
		for _, prior := range []string{"", "low"} {
			t.Run("choice="+choice+"/prior="+prior, func(t *testing.T) {
				home := keptEffortHome(t)
				path := filepath.Join(home, ".kimi-code", "config.toml")
				writeFile(t, path, kimiThinkingConfig)
				a := kimi(home)
				if prior != "" {
					if err := edit.SetTOMLKey(path, "thinking", "effort", prior); err != nil {
						t.Fatal(err)
					}
				}
				was := readKimiTOML(t, path)
				if err := a.Connect(); err != nil {
					t.Fatal(err)
				}
				if !a.Wired() || a.Values()["effort"] != prior {
					t.Fatalf("connect changed prior effort: %v", a.Values())
				}
				if err := a.Pick("thinking", choice); err != nil {
					t.Fatal(err)
				}
				model := a.Values()["model"]
				if err := a.Disconnect(); err != nil {
					t.Fatal(err)
				}
				if a.Wired() || !a.Disconnected() || a.Values()["effort"] != prior {
					t.Fatalf("disconnect did not restore prior effort: %v", a.Values())
				}
				if now := readKimiTOML(t, path); !reflect.DeepEqual(now, was) {
					t.Fatalf("disconnect changed prior preferences:\n%s", readFile(path))
				}
				if err := a.Sync(); err != nil {
					t.Fatal(err)
				}
				if now := readKimiTOML(t, path); !reflect.DeepEqual(now, was) {
					t.Fatalf("disconnected sync changed preferences:\n%s", readFile(path))
				}
				a = kimi(home)
				if how, err := a.ConnectHow(); err != nil || how.How != "again" {
					t.Fatalf("reconnect: %+v %v", how, err)
				}
				if got := a.Values(); !a.Wired() || a.Disconnected() || got["model"] != model || got["effort"] != choice {
					t.Fatalf("reconnect lost prior pick: %v", got)
				}
				if err := a.Disconnect(); err != nil {
					t.Fatal(err)
				}
				if now := readKimiTOML(t, path); !reflect.DeepEqual(now, was) {
					t.Fatalf("second disconnect changed prior preferences:\n%s", readFile(path))
				}
			})
		}
	}
}

func TestKimiThinkingEffortSurvivesModelChanges(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, strings.Replace(kimiThinkingConfig, "[thinking]\n", "[thinking]\neffort = \"xhigh\"\n", 1))
	was := readKimiTOML(t, path)
	a := kimi(home)
	if err := a.Pick("model", "magpie/think/deep"); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetTOMLKey(path, `models."magpie/think/deep"`, "default_effort", "low"); err != nil {
		t.Fatal(err)
	}
	check := func(effort string) {
		t.Helper()
		now := readKimiTOML(t, path)
		if a.Field("thinking").Get() != effort || now["thinking"].(map[string]any)["effort"] != effort {
			t.Fatalf("global thinking effort changed:\n%s", readFile(path))
		}
		models := now["models"].(map[string]any)
		if models["magpie/think/deep"].(map[string]any)["default_effort"] != "low" || models["magpie/think/wide"].(map[string]any)["default_effort"] != "high" {
			t.Fatalf("model defaults changed:\n%s", readFile(path))
		}
		now["default_model"] = "kimi-code/k3"
		delete(models, "magpie/think/deep")
		delete(models, "magpie/think/wide")
		delete(now["providers"].(map[string]any), "magpie")
		if !reflect.DeepEqual(now, was) {
			t.Fatalf("model change lost user preferences:\n%s", readFile(path))
		}
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	check("xhigh")
	if err := a.Pick("model", "magpie/think/wide"); err != nil {
		t.Fatal(err)
	}
	check("xhigh")
	if err := a.Apply("thinking", "max"); err != nil {
		t.Fatal(err)
	}
	was["thinking"].(map[string]any)["effort"] = "max"
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	check("max")
}

func TestKimiThinkingEffortKeepsExternalEdit(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, strings.Replace(kimiThinkingConfig, "[thinking]\n", "[thinking]\neffort = \"low\"\n", 1))
	was := readKimiTOML(t, path)
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetTOMLKey(path, "thinking", "effort", "medium"); err != nil {
		t.Fatal(err)
	}
	was["thinking"].(map[string]any)["effort"] = "medium"
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := a.Field("effort").Get(); got != "medium" {
		t.Fatalf("sync overwrote external effort: %q", got)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if now := readKimiTOML(t, path); !reflect.DeepEqual(now, was) {
		t.Fatalf("disconnect overwrote external edit:\n%s", readFile(path))
	}
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if got := a.Field("effort").Get(); got != "medium" {
		t.Fatalf("reconnect overwrote external effort: %q", got)
	}
}

func TestKimiThinkingEffortKeepsExternalRemoval(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, strings.Replace(kimiThinkingConfig, "[thinking]\n", "[thinking]\neffort = \"low\"\n", 1))
	was := readKimiTOML(t, path)
	a := kimi(home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	if err := edit.DelTOMLKey(path, "thinking", "effort"); err != nil {
		t.Fatal(err)
	}
	delete(was["thinking"].(map[string]any), "effort")
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if now := readKimiTOML(t, path); !reflect.DeepEqual(now, was) {
		t.Fatalf("disconnect overwrote external effort removal:\n%s", readFile(path))
	}
}

func TestKimiThinkingEffortWhileDisconnected(t *testing.T) {
	for _, wasConnected := range []bool{false, true} {
		t.Run("was-connected="+strconv.FormatBool(wasConnected), func(t *testing.T) {
			home := keptEffortHome(t)
			path := filepath.Join(home, ".kimi-code", "config.toml")
			writeFile(t, path, kimiThinkingConfig)
			a := kimi(home)
			if wasConnected {
				if err := a.Connect(); err != nil {
					t.Fatal(err)
				}
				if err := a.Disconnect(); err != nil {
					t.Fatal(err)
				}
			}
			was := readKimiTOML(t, path)
			for _, value := range []string{"max", ""} {
				if err := a.Pick("thinking", value); err != nil {
					t.Fatal(err)
				}
				if a.Wired() || a.Disconnected() != wasConnected || a.Values()["effort"] != value {
					t.Fatalf("effort %q rewired disconnected agent: %v", value, a.Values())
				}
				if err := a.Sync(); err != nil {
					t.Fatal(err)
				}
				now := readKimiTOML(t, path)
				thinking := now["thinking"].(map[string]any)
				if value != "" && thinking["effort"] != value {
					t.Fatalf("effort %q not written:\n%s", value, readFile(path))
				}
				if value == "" {
					if _, ok := thinking["effort"]; ok {
						t.Fatalf("default kept thinking.effort:\n%s", readFile(path))
					}
				}
				delete(thinking, "effort")
				if !reflect.DeepEqual(now, was) {
					t.Fatalf("disconnected effort %q changed other preferences:\n%s", value, readFile(path))
				}
			}
		})
	}
}

func TestKimiThinkingEffortCustomHome(t *testing.T) {
	home := keptEffortHome(t)
	paths := []string{
		filepath.Join(home, ".kimi-code", "config.toml"),
		filepath.Join(home, ".kimi", "config.toml"),
	}
	for _, path := range paths {
		writeFile(t, path, kimiThinkingConfig)
	}
	dir := filepath.Join(home, "custom-kimi")
	t.Setenv("KIMI_CODE_HOME", dir)
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, kimiThinkingConfig)
	a := kimi(home)
	if a.Path != path || a.Field("effort") == nil {
		t.Fatalf("custom home: path %q, effort %+v", a.Path, a.Field("effort"))
	}
	if err := a.Pick("thinking", "max"); err != nil {
		t.Fatal(err)
	}
	if got := readKimiTOML(t, path)["thinking"].(map[string]any)["effort"]; got != "max" {
		t.Fatalf("custom home's thinking.effort: %v", got)
	}
	if got := kimi(home).Field("effort").Get(); got != "max" {
		t.Fatalf("custom home's read effort: %q", got)
	}
	if err := a.Apply("effort", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := readKimiTOML(t, path)["thinking"].(map[string]any)["effort"]; ok {
		t.Fatalf("custom home's effort not reset:\n%s", readFile(path))
	}
	for _, path := range paths {
		if now := readFile(path); now != kimiThinkingConfig {
			t.Fatalf("custom home touched %s:\n%s", path, now)
		}
	}
}

func TestKimiThinkingEffortWSL(t *testing.T) {
	home := keptEffortHome(t)
	hostPath := filepath.Join(home, "custom-kimi", "config.toml")
	writeFile(t, hostPath, kimiThinkingConfig)
	t.Setenv("KIMI_CODE_HOME", filepath.Dir(hostPath))
	d := distro{Name: "Ubuntu", Root: t.TempDir(), Home: "/home/me", Running: true, Mirrored: true}
	path := filepath.Join(d.local(d.Home), ".kimi-code", "config.toml")
	writeFile(t, path, kimiThinkingConfig)
	a := wslAgent(wslKindOf("kimi"), d)
	if a.Path != path || a.Field("effort") == nil {
		t.Fatalf("WSL home: path %q, effort %+v", a.Path, a.Field("effort"))
	}
	if err := a.Pick("thinking", "max"); err != nil {
		t.Fatal(err)
	}
	if got := readKimiTOML(t, path)["thinking"].(map[string]any)["effort"]; got != "max" || a.Values()["effort"] != "max" || a.Wired() {
		t.Fatalf("WSL thinking.effort: %v, values %v", got, a.Values())
	}
	if err := a.Apply("effort", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := readKimiTOML(t, path)["thinking"].(map[string]any)["effort"]; ok {
		t.Fatalf("WSL effort not reset:\n%s", readFile(path))
	}
	if now := readFile(hostPath); now != kimiThinkingConfig {
		t.Fatalf("WSL effort changed host config:\n%s", now)
	}
}

func TestKimiThinkingEffortStoppedWSL(t *testing.T) {
	keptEffortHome(t)
	d := distro{Name: "Stopped", Root: t.TempDir(), Home: "/home/me", Running: false}
	a := wslAgent(wslKindOf("kimi"), d)
	if a.Field("effort") != nil {
		t.Fatal("stopped distro offers effort before modern Kimi is known")
	}
}

func TestKimiThinkingEffortLegacy(t *testing.T) {
	for _, moved := range []bool{false, true} {
		t.Run("share-dir="+strconv.FormatBool(moved), func(t *testing.T) {
			home := keptEffortHome(t)
			dir := filepath.Join(home, ".kimi")
			if moved {
				dir = filepath.Join(home, "legacy-kimi")
				t.Setenv("KIMI_SHARE_DIR", dir)
			}
			path := filepath.Join(dir, "config.toml")
			writeFile(t, path, kimiThinkingConfig)
			a := kimi(home)
			if a.Path != path || a.Field("effort") != nil || a.Field("thinking") != nil {
				t.Fatalf("legacy thinking field: path %q, fields %+v", a.Path, a.Fields)
			}
		})
	}
}

// kimiDefaultEffort is the whole selection rule for the default_effort
// magpie writes in one of its model tables, pure: the Agents-page pick
// (want) where the model offers it, else the model's strongest level
// (kimiHighestEffort), so a pick below a model's floor still reaches its
// strongest; a want nothing ranks falls through to the vendor/high chain
// exactly as today, so no key today's rule writes is dropped. With no
// pick, the vendor's default where the model offers it, else high, else
// no key at all. "none" is never a level: Kimi Code's own off turns
// thinking off.
func TestKimiDefaultEffort(t *testing.T) {
	deep := []string{"none", "low", "high", "max"}     // glm's shape: no pick high, max clamps nothing
	wide := []string{"low", "medium", "high"}          // wide's medium: a vendor default a pick replaces
	grok := []string{"low", "medium", "high", "xhigh"} // grok's: no max, a max pick clamps to xhigh
	ultra := []string{"low", "high", "ultra"}          // gpt-6.1-sol's: a pick beats the vendor default
	odd := []string{"turbo", "ludicrous"}              // words EffortRank doesn't know
	for _, tc := range []struct {
		name         string
		want, vendor string
		efforts      []string
		out          string
	}{
		{"no pick, vendor offered", "", "medium", wide, "medium"},
		{"no pick, vendor offered", "", "medium", wide, "medium"},
		{"no pick, vendor offered among more", "", "max", deep, "max"},
		{"no pick, vendor not offered, high is", "", "max", []string{"low", "medium", "high"}, "high"},
		{"no pick, neither offered", "", "max", []string{"low", "medium"}, ""},
		{"pick offered as is", "max", "high", deep, "max"},
		{"pick beats vendor default", "high", "ultra", ultra, "high"},
		{"below-floor pick clamps to strongest", "max", "xhigh", grok, "xhigh"},
		{"pick above everything clamps to the model's top", "max", "high", ultra, "ultra"},
		{"pick offered, no vendor default", "low", "", []string{"low", "high"}, "low"},
		{"a model with no default gains one with a pick", "medium", "", []string{"low", "medium"}, "medium"},
		{"none is never a level", "none", "medium", wide, "medium"},
		{"unrankable pick falls to vendor", "max", "ludicrous", odd, "ludicrous"},
		{"unrankable pick, no vendor, high is", "max", "", []string{"high", "turbo"}, "high"},
		{"unrankable pick, nothing to fall to", "max", "", odd, ""},
		{"unrankable vendor, rankable pick", "low", "turbo", []string{"low", "turbo"}, "low"},
		{"no pick, unrankable vendor, high is", "", "turbo", []string{"high", "turbo"}, "turbo"},
		{"empty efforts", "max", "high", nil, ""},
	} {
		if got := kimiDefaultEffort(tc.want, tc.vendor, tc.efforts); got != tc.out {
			t.Errorf("%s: kimiDefaultEffort(%q, %q, %v) = %q, want %q", tc.name, tc.want, tc.vendor, tc.efforts, got, tc.out)
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

// While the agent is wired, an Agents-page effort pick (kimiThinkingEffort,
// [thinking].effort) drives every magpie-owned default_effort: the pick
// where the model offers it, else its strongest level (a grok with no max
// stays at xhigh), and hand-set values keep their standing (kimiOwn).
// Picking default ("") restores the vendor-default/high fallback. On the
// pre-change code this fails: deep stayed high and grok-shaped wide kept
// its vendor default instead of both following max.
func TestKimiEffortPickDrivesModelTables(t *testing.T) {
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
	// no pick yet: the vendor/high fallback, as before (glm none → high,
	// grok its vendor xhigh)
	if got := efforts(); got["magpie/brain/glm"] != "high" || got["magpie/brain/grok"] != "xhigh" {
		t.Fatalf("no pick: %v", got)
	}
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	// glm takes max as offered; grok, with none, clamps to xhigh
	if got := efforts(); got["magpie/brain/glm"] != "max" || got["magpie/brain/grok"] != "xhigh" {
		t.Fatalf("pick max: %v", got)
	}
	if err := a.Apply("effort", "low"); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/brain/glm"] != "low" || got["magpie/brain/grok"] != "low" {
		t.Fatalf("pick low: %v", got)
	}
	// back to default: the vendor/high fallback is restored
	if err := a.Pick("effort", ""); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/brain/glm"] != "high" || got["magpie/brain/grok"] != "xhigh" {
		t.Fatalf("pick cleared: %v", got)
	}
	// a hand-set default stays while the model offers it, and a magpie
	// table's value follows the pick again once it isn't
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
	// an unrecorded legacy high (a magpie from before records kept left
	// none) is re-owned and migrates to the pick on the next sync; a
	// recorded magpie value moves with the pick on its own (checked above
	// with each Apply)
	edit.SetTOMLKey(path, `models."magpie/brain/glm"`, "default_effort", "high")
	wrote := kimiWrote(path)
	delete(wrote, `models."magpie/brain/glm"`)
	b, err := json.Marshal(wrote)
	if err != nil {
		t.Fatal(err)
	}
	stash(map[string]string{kimiWroteKey(path): string(b)})
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/brain/glm"] != "max" || got["magpie/brain/grok"] != "xhigh" {
		t.Fatalf("unrecorded legacy high migrated: %v", got)
	}
}

// The same re-own covers a table holding the model's vendor default with
// no record of it (dev's gpt-6.1-sol kept ultra through a wired max sync:
// magpie wrote that ultra before it kept records, and the record-less
// read mistook it for the user's hand-set). A value that is neither high
// nor the vendor default is the user's and stays.
func TestKimiUnrecordedVendorDefaultFollowsThePick(t *testing.T) {
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
	if err := a.Apply("effort", "max"); err != nil {
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
	if got := efforts(); got["magpie/uco/gpt-6.1-sol"] != "max" {
		t.Fatalf("pick max: %v", got)
	}
	// a table the record was lost from (a magpie from before records
	// kept), holding the vendor default, follows the pick again
	edit.SetTOMLKey(path, `models."magpie/uco/gpt-6.1-sol"`, "default_effort", "ultra")
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
	if got := efforts(); got["magpie/uco/gpt-6.1-sol"] != "max" {
		t.Fatalf("unrecorded vendor default followed: %v", got)
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
	// and with the record kept, today's semantics stand
	edit.SetTOMLKey(path, `models."magpie/uco/gpt-6.1-sol"`, "default_effort", "ultra")
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := efforts(); got["magpie/uco/gpt-6.1-sol"] != "ultra" {
		t.Fatalf("recorded hand-set replaced: %v", got)
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
			if !strings.HasPrefix(l, "effort") {
				out = append(out, l)
			}
		}
		return strings.Join(out, "\n")
	}
	if stripped := strip(now); stripped != strip(was) {
		t.Fatalf("pick while not wired changed more than thinking.effort:\n%s", now)
	}
	// the thinking table alone gained the effort line, keyed where the
	// table's keys are
	if now != strings.Replace(was, "keep = \"all\"\n", "keep = \"all\"\neffort = \"max\"\n", 1) {
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
	if !a.Wired() && a.Values()["effort"] != "" {
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

// A [thinking] that can't be read as a table (an array table, valid TOML
// that syncs fine) degrades to the pre-change write: the takeover still
// happens, the defaults keep the vendor/high fallback, and a Notice names
// the unreadable table. Unknown is not unset.
func TestKimiUnreadableThinkingDegrades(t *testing.T) {
	home := keptEffortHome(t)
	path := filepath.Join(home, ".kimi-code", "config.toml")
	writeFile(t, path, "[[thinking]]\neffort = \"max\"\n")
	a := kimi(home)
	if err := a.Pick("model", "magpie/think/deep"); err != nil {
		t.Fatal(err)
	}
	ms := readKimiTOML(t, path)["models"].(map[string]any)
	deep := ms["magpie/think/deep"].(map[string]any)
	if deep["default_effort"] != "high" {
		t.Fatalf("degraded write followed the pick anyway: %v", deep["default_effort"])
	}
	if n := a.Notice(); !strings.Contains(n, "[thinking]") {
		t.Fatalf("no notice of the unreadable [thinking]: %q", n)
	}
	// the effort field's Set, which writes the key it is asked for, is
	// refused on a [thinking] that isn't a table — the writer's degrade
	// doesn't reach it — and leaves the file as it is
	if err := a.Apply("effort", "low"); err == nil || !strings.Contains(err.Error(), "array table") {
		t.Fatalf("effort set on an unreadable [thinking]: %v", err)
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
