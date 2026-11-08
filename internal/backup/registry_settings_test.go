package backup

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/settings"
)

// regSettings is the registry config as the four fields come and go in a
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
	for _, k := range []string{"registryOn", "registryEffort", "registryBuckets", "registrySync"} {
		if _, ok := m[k]; ok {
			out[k] = true
		}
	}
	return out
}

// An off machine's bundle carries none of the registry's config, whichever
// way the bundle is made — a whole collect and the automatic backup's —
// while the switch itself still rides; an on machine's carries it whole
// (criterion 7).
func TestCollectStripsRegistryWhenSyncOff(t *testing.T) {
	home(t)
	setUp(t)
	s := settings.Load()
	s.RegistryOn, s.RegistryEffort, s.RegistryBuckets = true, "max", []string{"magpie-openai", "magpie-group"}
	s.RegistrySync = false
	s = regSettings(t, s)
	b, err := Collect(true, "test")
	if err != nil {
		t.Fatal(err)
	}
	f := regFields(t, b.Settings)
	if f["registryOn"] || f["registryEffort"] || f["registryBuckets"] {
		t.Fatalf("an off machine's bundle carries the config: %v", f)
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
	s = regSettings(t, settings.Settings{RegistryOn: true, RegistryEffort: "max", RegistryBuckets: []string{"magpie-openai"}, RegistrySync: false})
	b, err = Collect(false, "test")
	if err != nil {
		t.Fatal(err)
	}
	if f := regFields(t, b.Settings); f["registryOn"] || f["registryEffort"] || f["registryBuckets"] {
		t.Fatalf("a keyless collect carries the config: %v", f)
	}

	// on: whole
	home(t)
	setUp(t)
	s = regSettings(t, settings.Settings{RegistryOn: true, RegistryEffort: "max", RegistryBuckets: []string{"magpie-openai", "magpie-group"}, RegistrySync: true})
	b, err = Collect(true, "test")
	if err != nil {
		t.Fatal(err)
	}
	got := b.Settings
	if !got.RegistryOn || got.RegistryEffort != "max" || strings.Join(got.RegistryBuckets, ",") != "magpie-openai,magpie-group" || !got.RegistrySync {
		t.Fatalf("an on machine's bundle lost the config: %+v", got)
	}
}

// restore lands the bundle's registry config only when the sender carried
// it (its switch was on) and this machine asked for it too; either switch
// off keeps this machine's own, and the flag is every machine's own either
// way. A bundle from a magpie before the registry, and one carrying the
// flag with no config, keep this machine's own too.
func TestRestoreRegistryLanding(t *testing.T) {
	on := settings.Settings{RegistryOn: true, RegistryEffort: "low", RegistryBuckets: []string{"magpie-openai", "magpie-anthropic"}, RegistrySync: true}
	off := settings.Settings{RegistryOn: false, RegistryEffort: "", RegistryBuckets: []string{"magpie-openai", "magpie-anthropic", "magpie-openai-responses"}, RegistrySync: false}
	for _, tc := range []struct {
		name      string
		current   settings.Settings
		incoming  *settings.Settings
		keep      settings.Settings // what stays on this machine after
		flagAfter bool
	}{
		{
			name:      "off sender onto off receiver",
			current:   off,
			incoming:  &settings.Settings{RegistrySync: false},
			keep:      off,
			flagAfter: false,
		},
		{
			name:      "off sender onto on receiver",
			current:   on,
			incoming:  &settings.Settings{RegistrySync: false},
			keep:      on,
			flagAfter: true,
		},
		{
			name:      "on sender onto off receiver",
			current:   off,
			incoming:  &settings.Settings{RegistryOn: true, RegistryEffort: "max", RegistryBuckets: []string{"magpie-openai", "magpie-group", "magpie-openai-responses", "magpie-anthropic"}, RegistrySync: true},
			keep:      off,
			flagAfter: false,
		},
		{
			name:      "on sender onto on receiver",
			current:   on,
			incoming:  &settings.Settings{RegistryOn: true, RegistryEffort: "max", RegistryBuckets: []string{"magpie-openai", "magpie-group"}, RegistrySync: true},
			keep:      settings.Settings{RegistryOn: true, RegistryEffort: "max", RegistryBuckets: []string{"magpie-openai", "magpie-group"}, RegistrySync: true},
			flagAfter: true,
		},
		{
			// the bundle's switch is on but its maker carried no config (a
			// stripped one, or a hand-made bundle): riding needs the whole
			// marker, so this machine keeps its own
			name:      "flag without config",
			current:   on,
			incoming:  &settings.Settings{RegistrySync: true},
			keep:      on,
			flagAfter: true,
		},
		{
			// a magpie from before the registry: the whole settings part is
			// the older shape, and it lands as one
			name:      "legacy bundle",
			current:   on,
			incoming:  &settings.Settings{Theme: "light"},
			keep:      on,
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
			if b.Settings.Theme != "" && s.Theme != b.Settings.Theme {
				t.Errorf("theme %q: the incoming part should land whole", s.Theme)
			}
		})
	}
}

// The landing is a pure transform of the bundle: putting the same one in
// again changes nothing the second time.
func TestRestoreRegistryIdempotent(t *testing.T) {
	home(t)
	setUp(t)
	regSettings(t, settings.Settings{RegistryOn: false, RegistryEffort: "", RegistryBuckets: []string{"magpie-openai", "magpie-anthropic"}, RegistrySync: true})
	b := Bundle{Version: 1, Settings: &settings.Settings{RegistryOn: true, RegistryEffort: "max", RegistryBuckets: []string{"magpie-openai", "magpie-group"}, RegistrySync: true}}
	for range 2 {
		if _, err := Restore(b, Parts{Settings: true}); err != nil {
			t.Fatal(err)
		}
		s := settings.Load()
		if !s.RegistrySync || !s.RegistryOn || s.RegistryEffort != "max" || strings.Join(s.RegistryBuckets, ",") != "magpie-openai,magpie-group" {
			t.Fatalf("not the landed config: %+v", s)
		}
	}
}
