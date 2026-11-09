package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/settings"
)

// regSettings is the registry config as the five fields come and go in a
// bundle: written through Save, so what the tests put in is exactly what a
// machine would have on disk.
func regSettings(t *testing.T, s settings.Settings) settings.Settings {
	t.Helper()
	s.Theme = "dark"
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	return settings.Load()
}

// regFields says which of the registry's keys a bundle's settings part
// carries, as the file travels them: by name in the raw JSON.
func regFields(t *testing.T, s *settings.Settings) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	if s == nil {
		return out
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"registryOn", "registryEffort", "registryGroups", "registrySync"} {
		if _, ok := m[k]; ok {
			out[k] = true
		}
	}
	if m["registryBuckets"] != nil { // null is the not-carried spelling now
		out["registryBuckets"] = true
	}
	return out
}

// An off machine's bundle carries none of the registry's config — the five
// fields' keys all absent, the groups switch absent rather than false —
// whichever way the bundle is made: a whole collect and the automatic
// backup's. An on machine's carries it whole, the groups switch included.
func TestCollectStripsRegistryWhenSyncOff(t *testing.T) {
	off := false
	home(t)
	setUp(t)
	s := settings.Load()
	s.RegistryOn, s.RegistryEffort = true, "max"
	s.RegistryBuckets, s.RegistryGroups, s.RegistrySync = []string{"magpie-openai", "magpie-gemini"}, &off, false
	s = regSettings(t, s)
	b, err := Collect(true, "test")
	if err != nil {
		t.Fatal(err)
	}
	f := regFields(t, b.Settings)
	if f["registryOn"] || f["registryEffort"] || f["registryBuckets"] || f["registryGroups"] {
		t.Fatalf("an off machine's bundle carries the config: %v", f)
	}
	if b.Settings.RegistryGroups != nil {
		t.Fatalf("the strip spelled not-carried as %v, want nil", *b.Settings.RegistryGroups)
	}
	if f["registrySync"] { // omitempty: an off switch rides as the absent key
		t.Fatal("the switch reads on in an off machine's bundle")
	}
	if b.Settings.RegistryBuckets != nil {
		t.Fatalf("the stripped marker: %v", b.Settings.RegistryBuckets)
	}

	// the same through the automatic backup's collect
	home(t)
	setUp(t)
	s = regSettings(t, settings.Settings{RegistryOn: true, RegistryEffort: "max", RegistryBuckets: []string{"magpie-openai"}, RegistryGroups: &off, RegistrySync: false})
	b, err = Collect(false, "test")
	if err != nil {
		t.Fatal(err)
	}
	if f := regFields(t, b.Settings); f["registryOn"] || f["registryEffort"] || f["registryBuckets"] || f["registryGroups"] {
		t.Fatalf("a keyless collect carries the config: %v", f)
	}

	// on: whole
	home(t)
	setUp(t)
	on := true
	regSettings(t, settings.Settings{RegistryOn: true, RegistryEffort: "max", RegistryBuckets: []string{"magpie-openai", "magpie-gemini"}, RegistryGroups: &on, RegistrySync: true})
	b, err = Collect(true, "test")
	if err != nil {
		t.Fatal(err)
	}
	got := b.Settings
	if !got.RegistryOn || got.RegistryEffort != "max" || strings.Join(got.RegistryBuckets, ",") != "magpie-openai,magpie-gemini" || !got.RegistrySync {
		t.Fatalf("an on machine's bundle lost the config: %+v", got)
	}
	if got.RegistryGroups == nil || !*got.RegistryGroups {
		t.Fatalf("an on machine's bundle lost its groups switch: %v", got.RegistryGroups)
	}
}

// restore lands the bundle's registry config only when the sender carried
// it whole — the wires and the groups switch both — and this machine asked
// for it too; either switch off, or a half bundle (a legacy peer's, with no
// groups switch), keeps this machine's own, and the flag is every
// machine's own either way. A bundle from a magpie before the registry, and
// one carrying the flag with no config, keep this machine's own too.
func TestRestoreRegistryLanding(t *testing.T) {
	on, off := true, false
	cur := settings.Settings{RegistryOn: true, RegistryEffort: "low", RegistryBuckets: []string{"magpie-openai", "magpie-anthropic"}, RegistryGroups: &off, RegistrySync: true}
	whole := settings.Settings{RegistryOn: true, RegistryEffort: "max", RegistryBuckets: []string{"magpie-openai", "magpie-gemini"}, RegistryGroups: &on, RegistrySync: true}
	for _, tc := range []struct {
		name      string
		current   settings.Settings
		incoming  *settings.Settings
		keep      settings.Settings // what stays on this machine after
		flagAfter bool
	}{
		{
			name:      "off sender onto off receiver",
			current:   settings.Settings{RegistryOn: false, RegistryEffort: "", RegistryBuckets: []string{"magpie-openai"}, RegistryGroups: &off, RegistrySync: false},
			incoming:  &settings.Settings{RegistrySync: false},
			keep:      settings.Settings{RegistryOn: false, RegistryEffort: "", RegistryBuckets: []string{"magpie-openai"}, RegistryGroups: &off, RegistrySync: false},
			flagAfter: false,
		},
		{
			name:      "off sender onto on receiver",
			current:   cur,
			incoming:  &settings.Settings{RegistrySync: false},
			keep:      cur,
			flagAfter: true,
		},
		{
			name:      "on sender onto off receiver",
			current:   settings.Settings{RegistryOn: false, RegistryEffort: "low", RegistryBuckets: []string{"magpie-anthropic"}, RegistryGroups: &on, RegistrySync: false},
			incoming:  &whole,
			keep:      settings.Settings{RegistryOn: false, RegistryEffort: "low", RegistryBuckets: []string{"magpie-anthropic"}, RegistryGroups: &on, RegistrySync: false},
			flagAfter: false,
		},
		{
			name:      "on sender onto on receiver",
			current:   cur,
			incoming:  &whole,
			keep:      whole,
			flagAfter: true,
		},
		{
			// the sender's switch is on but its bundle carried no config
			// (a stripped one, or a hand-made bundle): riding needs the
			// whole state, so this machine keeps its own
			name:      "flag without config",
			current:   cur,
			incoming:  &settings.Settings{RegistrySync: true},
			keep:      cur,
			flagAfter: true,
		},
		{
			// a pre-split peer's bundle: the group tick still inside the
			// wire list, no groups switch. The half never rides (I5), so
			// the legacy id never reaches Save's refusal — this machine
			// keeps its own whole config until the peer upgrades
			name:      "legacy half never rides",
			current:   cur,
			incoming:  &settings.Settings{RegistryOn: true, RegistryEffort: "max", RegistryBuckets: []string{"magpie-openai", "magpie-group"}, RegistrySync: true},
			keep:      cur,
			flagAfter: true,
		},
		{
			// a magpie from before the registry: the whole settings part is
			// the older shape, and it lands as one
			name:      "legacy bundle",
			current:   cur,
			incoming:  &settings.Settings{Theme: "light"},
			keep:      cur,
			flagAfter: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home(t)
			setUp(t)
			regSettings(t, tc.current)
			b := Bundle{Version: 1, Settings: tc.incoming}
			if _, err := Restore(b, Parts{Settings: true}); err != nil {
				t.Fatal(err)
			}
			s := settings.Load()
			if s.RegistrySync != tc.flagAfter {
				t.Errorf("flag after: %v, want %v", s.RegistrySync, tc.flagAfter)
			}
			if s.RegistryOn != tc.keep.RegistryOn || s.RegistryEffort != tc.keep.RegistryEffort {
				t.Errorf("served/effort kept %v/%q, want %v/%q", s.RegistryOn, s.RegistryEffort, tc.keep.RegistryOn, tc.keep.RegistryEffort)
			}
			if got, want := strings.Join(s.RegistryBuckets, ","), strings.Join(tc.keep.RegistryBuckets, ","); got != want {
				t.Errorf("buckets %q, want %q", got, want)
			}
			if s.RegistryGroups == nil || tc.keep.RegistryGroups == nil || *s.RegistryGroups != *tc.keep.RegistryGroups {
				t.Errorf("groups kept %v, want %v", s.RegistryGroups, tc.keep.RegistryGroups)
			}
			if b.Settings.Theme != "" && s.Theme != b.Settings.Theme {
				t.Errorf("theme %q: the incoming part should land whole", s.Theme)
			}
			raw, err := os.ReadFile(settings.Path())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "magpie-group") {
				t.Errorf("the legacy id reached the stored file: %s", raw)
			}
		})
	}
}

// The landing is a pure transform of the bundle: putting the same one in
// again changes nothing the second time.
func TestRestoreRegistryIdempotent(t *testing.T) {
	on, off := true, false
	home(t)
	setUp(t)
	regSettings(t, settings.Settings{RegistryOn: false, RegistryEffort: "", RegistryBuckets: []string{"magpie-openai", "magpie-anthropic"}, RegistryGroups: &on, RegistrySync: true})
	b := Bundle{Version: 1, Settings: &settings.Settings{RegistryOn: true, RegistryEffort: "max", RegistryBuckets: []string{"magpie-openai", "magpie-gemini"}, RegistryGroups: &off, RegistrySync: true}}
	for range 2 {
		if _, err := Restore(b, Parts{Settings: true}); err != nil {
			t.Fatal(err)
		}
		s := settings.Load()
		if !s.RegistrySync || !s.RegistryOn || s.RegistryEffort != "max" || strings.Join(s.RegistryBuckets, ",") != "magpie-openai,magpie-gemini" {
			t.Fatalf("not the landed config: %+v", s)
		}
		if s.RegistryGroups == nil || *s.RegistryGroups {
			t.Fatalf("not the landed groups switch: %v", s.RegistryGroups)
		}
	}
}

// A machine whose stored file is still the pre-split shape — the group tick
// inside the wire list, no groups switch — migrates at load: the landing
// reads its cur through settings.Load, the one migration site, so the file
// moves onto the new shape there and a whole bundle lands over it, with
// saveOwn never seeing the legacy id to refuse.
func TestRestoreMigratesLegacyFileAtLoad(t *testing.T) {
	on := true
	home(t)
	setUp(t)
	if err := os.MkdirAll(filepath.Dir(settings.Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"theme":"dark","registryOn":true,"registryEffort":"max","registryBuckets":["magpie-openai","magpie-group"],"registrySync":true}` + "\n"
	if err := os.WriteFile(settings.Path(), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	b := Bundle{Version: 1, Settings: &settings.Settings{Theme: "light", RegistryOn: true, RegistryEffort: "low",
		RegistryBuckets: []string{"magpie-anthropic"}, RegistryGroups: &on, RegistrySync: true}}
	if _, err := Restore(b, Parts{Settings: true}); err != nil {
		t.Fatalf("restoring over a legacy file was refused: %v", err)
	}
	s := settings.Load()
	// the tick became the groups switch at load, and the whole bundle then
	// landed over it
	if s.Theme != "light" {
		t.Fatalf("theme %q: the incoming part should land whole", s.Theme)
	}
	if !s.RegistryOn || s.RegistryEffort != "low" || strings.Join(s.RegistryBuckets, ",") != "magpie-anthropic" || s.RegistryGroups == nil || !*s.RegistryGroups {
		t.Fatalf("the whole config didn't land over the migrated file: %+v", s)
	}
	after, err := os.ReadFile(settings.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "magpie-group") {
		t.Fatalf("the legacy id stayed in the stored file: %s", after)
	}
}
