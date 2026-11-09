package cli

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/settingsapi"
)

// settingsHome is a home of its own whose settings.json has the user's
// own choices in it, and gives that file's bytes.
func settingsHome(t *testing.T) []byte {
	t.Helper()
	helpHome(t)
	s := settings.Defaults()
	s.Theme, s.NoBird, s.Currency, s.Proxy = "dark", true, "cny", "http://127.0.0.1:7890"
	s.RedactWords = []string{"acme"}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(settings.Path())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSettingsSetGetReset(t *testing.T) {
	settingsHome(t)
	if _, err := runOut(t, "settings", "set", "lang", "ja"); err != nil {
		t.Fatal(err)
	}
	if out, err := runOut(t, "settings", "get", "lang"); err != nil || strings.TrimSpace(out) != "ja" {
		t.Fatalf("get lang = %q, %v", out, err)
	}
	s := settings.Load()
	if s.Lang != "ja" || s.Theme != "dark" || !s.NoBird || s.Currency != "cny" || s.Proxy != "http://127.0.0.1:7890" || len(s.RedactWords) != 1 {
		t.Fatalf("setting lang changed what the user set: %+v", s)
	}
	// a switch to off is sent, not left out as a zero value
	if _, err := runOut(t, "settings", "set", "noBird", "off"); err != nil {
		t.Fatal(err)
	}
	if settings.Load().NoBird {
		t.Error("noBird off stayed on")
	}
	// numbers, lists, and a value the save puts in its own form
	for _, c := range []struct{ key, value, want string }{
		{"usageAlert", "80", "80"},
		{"trayUsageEvery", "10", "10"},
		{"redactWords", "acme, globex", "acme,globex"},
		{"redactWords", `["a","b"]`, "a,b"},
		{"redactWords", "", "[]"},
		{"codexWarmup", "off", `""`},
		{"refusalRetries", "2", "2"},
		{"otel", `{"enabled":true,"endpoint":"http://127.0.0.1:4318"}`, ""},
	} {
		if _, err := runOut(t, "settings", "set", c.key, c.value); err != nil {
			t.Fatalf("set %s %q: %v", c.key, c.value, err)
		}
		out, _ := runOut(t, "settings", "get", c.key)
		if c.want != "" && strings.TrimSpace(out) != c.want {
			t.Errorf("set %s %q, then get = %q, want %q", c.key, c.value, out, c.want)
		}
	}
	if _, err := runOut(t, "settings", "reset", "theme"); err != nil {
		t.Fatal(err)
	}
	if s := settings.Load(); s.Theme != "system" || s.Currency != "cny" {
		t.Errorf("reset theme: theme %q currency %q", s.Theme, s.Currency)
	}
	if _, err := runOut(t, "settings", "reset", "refusalRetries"); err != nil {
		t.Fatal(err)
	}
	if s := settings.Load(); s.RefusalRetries != nil {
		t.Errorf("reset refusalRetries left %d", *s.RefusalRetries)
	}
}

func TestSettingsLightweight(t *testing.T) {
	settingsHome(t)
	get := func(want string) {
		t.Helper()
		if out, err := runOut(t, "settings", "get", "lightweight"); err != nil || strings.TrimSpace(out) != want {
			t.Fatalf("get lightweight = %q, %v; want %q", out, err, want)
		}
	}
	get("off")
	out, err := runOut(t, "settings", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []settingRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Key == "lightweight" {
			found = true
			if r.Value != false || r.Default != false {
				t.Errorf("lightweight row = %+v, want value and default off", r)
			}
		}
	}
	if !found {
		t.Fatal("settings list has no lightweight row")
	}
	if _, err := runOut(t, "settings", "set", "lightweight", "on"); err != nil {
		t.Fatal(err)
	}
	get("on")
	if !settings.Load().LightweightOn() {
		t.Fatal("lightweight set on read back off")
	}
	if _, err := runOut(t, "settings", "reset", "lightweight"); err != nil {
		t.Fatal(err)
	}
	get("off")
	if settings.Load().LightweightOn() {
		t.Fatal("lightweight reset read back on")
	}
}

// A key or value that isn't one, and a flag where a value goes, write
// nothing and say why.
func TestSettingsRefusesAndWritesNothing(t *testing.T) {
	before := settingsHome(t)
	for _, args := range [][]string{
		{"settings", "set", "nope", "1"},
		{"settings", "set", "port", "9999"},  // a route of its own
		{"settings", "set", "theme", "neon"}, // not one of its choices
		{"settings", "set", "noBird", "maybe"},
		{"settings", "set", "usageAlert", "lots"},
		{"settings", "set", "usageAlert", "200"}, // the save's own range
		{"settings", "set", "trayUsageEvery", "4"},
		{"settings", "set", "vision", "nobody/nothing"},
		{"settings", "set", "otel", `{"bogus":1}`},
		{"settings", "set", "theme", "--help-me"},
		{"settings", "set", "theme"},
		{"settings", "reset", "nope"},
		{"settings", "get", "nope"},
		{"settings", "frob"},
	} {
		out, err := runOut(t, args...)
		if err == nil {
			t.Errorf("magpie %s: no error (%q)", strings.Join(args, " "), out)
		}
		after, _ := os.ReadFile(settings.Path())
		if string(after) != string(before) {
			t.Fatalf("magpie %s changed settings.json", strings.Join(args, " "))
		}
	}
	_, err := runOut(t, "settings", "set", "them", "dark")
	if err == nil || !strings.Contains(err.Error(), "theme") {
		t.Errorf("a near miss doesn't name the key: %v", err)
	}
	// a negative number is a value, not a flag
	_, err = runOut(t, "settings", "set", "usageAlert", "-1")
	if err == nil || strings.Contains(err.Error(), "flag") {
		t.Errorf("-1 read as a flag or saved: %v", err)
	}
}

// The model registry's settings are the CLI's to set, like the window's
// (the stacks' upgrade path is `magpie settings set registryOn true`).
// The wires are RegistryWireIDs' four — the legacy group id is refused,
// the groups having their own switch now — and a registry left with no
// wire and the groups off is refused: nothing is written.
func TestSettingsRegistry(t *testing.T) {
	settingsHome(t)
	if _, err := runOut(t, "settings", "set", "registryOn", "true"); err != nil {
		t.Fatal(err)
	}
	if out, err := runOut(t, "settings", "get", "registryOn"); err != nil || strings.TrimSpace(out) != "on" {
		t.Fatalf("get registryOn = %q, %v", out, err)
	}
	if !settings.Load().RegistryOn {
		t.Error("registryOn is off after it was set")
	}
	if _, err := runOut(t, "settings", "set", "registryEffort", "max"); err != nil {
		t.Fatal(err)
	}
	if out, err := runOut(t, "settings", "get", "registryEffort"); err != nil || strings.TrimSpace(out) != "max" {
		t.Fatalf("get registryEffort = %q, %v", out, err)
	}
	if _, err := runOut(t, "settings", "set", "registrySync", "on"); err != nil {
		t.Fatal(err)
	}
	if !settings.Load().RegistrySync {
		t.Error("registrySync is off after it was set")
	}
	// the routing-groups switch round-trips, and the pointer it is stored
	// as is shown as the switch it is, never as null
	var nilGroups *bool
	if got := shown(nilGroups); got != "on" {
		t.Errorf("a nil groups switch shows as %q, want on", got)
	}
	if _, err := runOut(t, "settings", "set", "registryGroups", "false"); err != nil {
		t.Fatal(err)
	}
	if out, err := runOut(t, "settings", "get", "registryGroups"); err != nil || strings.TrimSpace(out) != "off" {
		t.Fatalf("get registryGroups = %q, %v", out, err)
	}
	if s := settings.Load(); s.RegistryGroups == nil || *s.RegistryGroups {
		t.Error("registryGroups set false stayed on")
	}
	if _, err := runOut(t, "settings", "set", "registryGroups", "true"); err != nil {
		t.Fatal(err)
	}
	if out, err := runOut(t, "settings", "get", "registryGroups"); err != nil || strings.TrimSpace(out) != "on" {
		t.Fatalf("get registryGroups = %q, %v", out, err)
	}
	// the wires carry their own ids, gemini the fourth
	if _, err := runOut(t, "settings", "set", "registryBuckets", "magpie-openai,magpie-gemini"); err != nil {
		t.Fatal(err)
	}
	if out, err := runOut(t, "settings", "get", "registryBuckets"); err != nil || strings.TrimSpace(out) != "magpie-openai,magpie-gemini" {
		t.Fatalf("get registryBuckets = %q, %v", out, err)
	}
	// the legacy group id is a wire no longer: the groups have their own
	// switch, and a value the save refuses writes nothing
	before, err := os.ReadFile(settings.Path())
	if err != nil {
		t.Fatal(err)
	}
	out, err := runOut(t, "settings", "set", "registryBuckets", "magpie-openai,magpie-group")
	if err == nil {
		t.Errorf("the legacy group id was taken as a wire (%q)", out)
	}
	if err != nil && !strings.Contains(err.Error(), `not "magpie-group"`) {
		t.Errorf("the refused wire didn't say which: %v", err)
	}
	after, _ := os.ReadFile(settings.Path())
	if string(after) != string(before) {
		t.Fatal("a refused wire list changed settings.json")
	}
	// no wire and the groups off is the registry listing nothing, and the
	// refusal says so without a bucket left in it
	if _, err := runOut(t, "settings", "set", "registryGroups", "off"); err != nil {
		t.Fatal(err)
	}
	before, err = os.ReadFile(settings.Path())
	if err != nil {
		t.Fatal(err)
	}
	out, err = runOut(t, "settings", "set", "registryBuckets", "")
	if err == nil {
		t.Errorf("magpie settings set registryBuckets \"\": no error (%q)", out)
	}
	if err != nil && !errors.Is(err, settings.ErrRegistryEmpty) {
		t.Errorf("the refused registry didn't say why: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "bucket") {
		t.Errorf("the refusal still speaks of buckets: %v", err)
	}
	after, _ = os.ReadFile(settings.Path())
	if string(after) != string(before) {
		t.Fatal("a refused registry save changed settings.json")
	}
	// back to defaults: the groups on, all four wires
	if _, err := runOut(t, "settings", "reset", "registryGroups"); err != nil {
		t.Fatal(err)
	}
	if out, err := runOut(t, "settings", "get", "registryGroups"); err != nil || strings.TrimSpace(out) != "on" {
		t.Fatalf("get registryGroups after a reset = %q, %v", out, err)
	}
	if _, err := runOut(t, "settings", "reset", "registryBuckets"); err != nil {
		t.Fatal(err)
	}
	if out, err := runOut(t, "settings", "get", "registryBuckets"); err != nil || strings.TrimSpace(out) != "magpie-openai,magpie-anthropic,magpie-openai-responses,magpie-gemini" {
		t.Fatalf("get registryBuckets after a reset = %q, %v", out, err)
	}
}

func TestSettingsListAndJSON(t *testing.T) {
	settingsHome(t)
	out, err := runOut(t, "settings", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []settingRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("list --json: %v\n%s", err, out)
	}
	if len(rows) != len(settingsapi.Keys) {
		t.Fatalf("list --json has %d rows, Keys %d", len(rows), len(settingsapi.Keys))
	}
	for _, r := range rows {
		if r.Key == "theme" && (r.Value != "dark" || r.Default != "system") {
			t.Errorf("theme row: %+v", r)
		}
	}
	out, err = runOut(t, "settings")
	if err != nil || !strings.Contains(out, "theme") || !strings.Contains(out, "default system") {
		t.Errorf("settings list: %v\n%s", err, out)
	}
	out, err = runOut(t, "settings", "get", "redactWords", "--json")
	if err != nil || strings.TrimSpace(strings.Join(strings.Fields(out), "")) != `["acme"]` {
		t.Errorf("get redactWords --json = %q, %v", out, err)
	}
}
