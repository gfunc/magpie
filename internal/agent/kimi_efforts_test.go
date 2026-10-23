package agent

import (
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
