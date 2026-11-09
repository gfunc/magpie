package settings

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"testing"
)

// The wire table the registry lists: three native wires and Gemini, with
// the routing groups' entry outside it. The gateway's parity test holds
// its own list to this one; this pins the table itself.
func TestRegistryWireIDsAndGroupID(t *testing.T) {
	if want := []string{"magpie-openai", "magpie-anthropic", "magpie-openai-responses", "magpie-gemini"}; !slices.Equal(RegistryWireIDs, want) {
		t.Fatalf("RegistryWireIDs %v, want %v", RegistryWireIDs, want)
	}
	if RegistryGroupID != "magpie-group" {
		t.Fatalf("RegistryGroupID %q, want magpie-group", RegistryGroupID)
	}
}

// The registry's two lists under normal: nothing said is all four wires
// and the groups on (absent means on for a raw Settings too), and an
// empty list stays empty — it is every wire turned off, which only Save
// refuses, and filling it would mask that refusal.
func TestRegistryNormalFillsWiresAndGroups(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := (Settings{}).normal()
	if !slices.Equal(s.RegistryBuckets, RegistryWireIDs) {
		t.Fatalf("nil buckets became %v, want %v", s.RegistryBuckets, RegistryWireIDs)
	}
	if s.RegistryGroups == nil || !*s.RegistryGroups {
		t.Fatalf("nil groups became %v, want on", s.RegistryGroups)
	}
	if !RegistryGroupsOn(Settings{}) {
		t.Fatal("a raw Settings with no groups switch must read as on")
	}
	off := false
	if RegistryGroupsOn(Settings{RegistryGroups: &off}) {
		t.Fatal("groups explicitly off read as on")
	}
	if got := (Settings{RegistryBuckets: []string{}}).normal().RegistryBuckets; len(got) != 0 {
		t.Fatalf("an empty list became %v, want it to stay empty", got)
	}
	fresh := Load()
	if !slices.Equal(fresh.RegistryBuckets, RegistryWireIDs) || !RegistryGroupsOn(fresh) {
		t.Fatalf("a fresh install: buckets %v groups %v, want all four wires and the groups on", fresh.RegistryBuckets, fresh.RegistryGroups)
	}
}

// A fresh install's first save of anything leaves the groups on: the
// nil-fill writes registryGroups true, so the saved file is no longer
// legacy-shaped and the next load's migration cannot read the missing
// switch as the tick left unchecked.
func TestRegistryFirstSaveLeavesGroupsOn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(Load()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"registryGroups": true`)) {
		t.Fatalf("the first save left no groups switch on disk: %s", b)
	}
	if s := Load(); !RegistryGroupsOn(s) {
		t.Fatalf("the groups flipped off across the first save: %v", s.RegistryGroups)
	}
}

// The one-time migration, both directions through the public load path:
// a pre-split file's bucket list carried (or lacked) the group tick, and
// that becomes the groups switch with the id stripped — a magpie-group
// list of its own is the legal groups-only []. Saving what loaded writes
// the switch out, and loading the saved file lands on the same config:
// idempotent.
func TestRegistryMigrationThroughLoad(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		file        string
		wantBuckets []string
		wantGroups  bool
	}{
		{"group tick carried", `{"registryBuckets":["magpie-group","magpie-openai"]}`, []string{"magpie-openai"}, true},
		{"group tick absent", `{"registryBuckets":["magpie-anthropic"]}`, []string{"magpie-anthropic"}, false},
		{"groups only", `{"registryBuckets":["magpie-group"]}`, []string{}, true},
		{"no list, no switch", `{"theme":"dark"}`, RegistryWireIDs, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(Path(), []byte(tc.file), 0o600); err != nil {
				t.Fatal(err)
			}
			s := Load()
			if !slices.Equal(s.RegistryBuckets, tc.wantBuckets) {
				t.Fatalf("buckets after load: %v, want %v", s.RegistryBuckets, tc.wantBuckets)
			}
			if s.RegistryGroups == nil || *s.RegistryGroups != tc.wantGroups {
				t.Fatalf("groups after load: %v, want %v", s.RegistryGroups, tc.wantGroups)
			}
			if err := Save(s); err != nil {
				t.Fatal(err)
			}
			again := Load()
			if !slices.Equal(again.RegistryBuckets, tc.wantBuckets) || again.RegistryGroups == nil || *again.RegistryGroups != tc.wantGroups {
				t.Fatalf("the config moved across a save and a load: buckets %v groups %v, want %v %v", again.RegistryBuckets, again.RegistryGroups, tc.wantBuckets, tc.wantGroups)
			}
		})
	}
}

// A save the registry refuses changes nothing on disk: no wire and the
// groups off (groups-only is the legal half of that), an id outside
// RegistryWireIDs — the whole save is refused and the bytes stay, so an
// old peer's half-understood bundle never lands — the legacy group id
// once the switch exists, and an effort outside the five.
func TestRegistrySaveRefusalsWriteNothing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"theme":"dark"}`)
	if err := os.WriteFile(Path(), original, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		set  func(*Settings)
		want error
	}{
		{"no wire and groups off", func(s *Settings) {
			s.RegistryBuckets = []string{}
			off := false
			s.RegistryGroups = &off
		}, ErrRegistryEmpty},
		{"unknown wire", func(s *Settings) { s.RegistryBuckets = []string{"magpie-openai", "magpie-github"} }, nil},
		{"legacy group id", func(s *Settings) { s.RegistryBuckets = []string{"magpie-group"} }, nil},
		{"effort ultra", func(s *Settings) { s.RegistryEffort = "ultra" }, nil},
		{"effort none", func(s *Settings) { s.RegistryEffort = "none" }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Load()
			tc.set(&s)
			err := Save(s)
			if err == nil {
				t.Fatal("refused value accepted")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("Save returned %v, want %v", err, tc.want)
			}
			after, rerr := os.ReadFile(Path())
			if rerr != nil {
				t.Fatal(rerr)
			}
			if !bytes.Equal(after, original) {
				t.Errorf("a refused save changed the file: %s", after)
			}
		})
	}
}

// Groups-only is a whole config: an empty bucket list with the groups on
// saves, round-trips as [] on disk (no omitempty on registryBuckets — an
// omitted empty list would read back as nil and refill all four), and
// loads back groups-only.
func TestRegistryGroupsOnlyRoundTripsEmptyList(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	on := true
	if err := Save(Settings{RegistryBuckets: []string{}, RegistryGroups: &on}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"registryBuckets": []`)) {
		t.Fatalf("the empty list did not round-trip on disk: %s", b)
	}
	s := Load()
	if s.RegistryBuckets == nil || len(s.RegistryBuckets) != 0 {
		t.Fatalf("groups-only came back as %v, want []", s.RegistryBuckets)
	}
	if !RegistryGroupsOn(s) {
		t.Fatalf("groups-only lost its switch: %v", s.RegistryGroups)
	}
}

// A wire list in any order, with a repeat, is written in RegistryWireIDs'
// order, once each; the effort is kept as it is read, lower-cased and
// trimmed.
func TestRegistryWiresCanonicalOrder(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := Settings{RegistryBuckets: []string{"magpie-gemini", "magpie-anthropic", "magpie-openai", "magpie-anthropic"}, RegistryEffort: " Max "}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if want := []string{"magpie-openai", "magpie-anthropic", "magpie-gemini"}; !slices.Equal(got.RegistryBuckets, want) {
		t.Fatalf("wires saved as %v, want %v", got.RegistryBuckets, want)
	}
	if got.RegistryEffort != "max" {
		t.Fatalf("effort saved as %q, want max", got.RegistryEffort)
	}
}

// CopyRegistryConfig owns the registry's whole field set — the five move
// together, nothing else does, and not-carried is nil groups, never
// false. The config leaves a bundle only while its switch is off, and
// taking it out twice changes nothing; a bundle whose switch is on keeps
// it whole.
func TestRegistryCopyAndStrip(t *testing.T) {
	on := true
	from := Settings{RegistryOn: true, RegistryEffort: "high", RegistryBuckets: []string{"magpie-openai"}, RegistryGroups: &on, RegistrySync: true, Theme: "dark"}
	s := Settings{Theme: "dark"}
	s.CopyRegistryConfig(from)
	if !s.RegistryOn || s.RegistryEffort != "high" || !slices.Equal(s.RegistryBuckets, []string{"magpie-openai"}) || s.RegistryGroups == nil || !*s.RegistryGroups || !s.RegistrySync {
		t.Fatalf("the five fields did not move: %+v", s)
	}
	if s.Theme != "dark" {
		t.Fatalf("a field outside the set moved: %+v", s)
	}
	s.CopyRegistryConfig(Settings{})
	if s.RegistryOn || s.RegistryEffort != "" || s.RegistryBuckets != nil || s.RegistryGroups != nil || s.RegistrySync {
		t.Fatalf("a zero copy left something behind: %+v", s)
	}
	b := Settings{RegistryOn: true, RegistryEffort: "high", RegistryBuckets: []string{"magpie-openai"}, RegistryGroups: &on, RegistrySync: false}
	(&b).OwnRegistry()
	if b.RegistryOn || b.RegistryEffort != "" || b.RegistryBuckets != nil || b.RegistryGroups != nil || b.RegistrySync {
		t.Fatalf("an off machine's bundle still carries the registry: %+v", b)
	}
	(&b).OwnRegistry()
	if b.RegistryOn || b.RegistryEffort != "" || b.RegistryBuckets != nil || b.RegistryGroups != nil {
		t.Fatalf("stripping twice changed something: %+v", b)
	}
	keep := Settings{RegistryOn: true, RegistryEffort: "high", RegistryBuckets: []string{"magpie-openai"}, RegistryGroups: &on, RegistrySync: true}
	(&keep).OwnRegistry()
	if !keep.RegistryOn || keep.RegistryEffort != "high" || !slices.Equal(keep.RegistryBuckets, []string{"magpie-openai"}) || keep.RegistryGroups == nil || !*keep.RegistryGroups {
		t.Fatalf("an on machine's bundle lost the registry: %+v", keep)
	}
}
