package settings

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"testing"
)

// The registry's buckets: nothing said is all four, an empty list stays
// empty (it is every one turned off, which only Save refuses — filling it
// here would mask that refusal).
func TestRegistryBucketsNilFillsAndEmptyStays(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := (Settings{}).normal().RegistryBuckets; !slices.Equal(got, RegistryBucketIDs) {
		t.Fatalf("nil buckets became %v, want %v", got, RegistryBucketIDs)
	}
	if got := (Settings{RegistryBuckets: []string{}}).normal().RegistryBuckets; len(got) != 0 {
		t.Fatalf("an empty list became %v, want it to stay empty", got)
	}
	if got := Load().RegistryBuckets; !slices.Equal(got, RegistryBucketIDs) {
		t.Fatalf("a fresh install's buckets: %v, want all four", got)
	}
}

// A save the registry refuses changes nothing on disk: every bucket turned
// off, an unknown bucket id, an effort outside the five (the empty effort
// is the model's own middle and fine).
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
		{"all buckets off", func(s *Settings) { s.RegistryBuckets = []string{} }, ErrRegistryBucketsOff},
		{"unknown bucket", func(s *Settings) { s.RegistryBuckets = []string{"magpie-openai", "magpie-github"} }, nil},
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

// A bucket list in any order, with a repeat, is written in
// RegistryBucketIDs' order, once each; the effort is kept as it is read,
// lower-cased and trimmed.
func TestRegistryBucketsCanonicalOrder(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := Settings{RegistryBuckets: []string{"magpie-group", "magpie-anthropic", "magpie-openai", "magpie-group"}, RegistryEffort: " Max "}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if want := []string{"magpie-openai", "magpie-anthropic", "magpie-group"}; !slices.Equal(got.RegistryBuckets, want) {
		t.Fatalf("buckets saved as %v, want %v", got.RegistryBuckets, want)
	}
	if got.RegistryEffort != "max" {
		t.Fatalf("effort saved as %q, want max", got.RegistryEffort)
	}
}

// The config leaves a bundle only while its switch is off, and taking it
// out twice changes nothing; a bundle whose switch is on keeps it whole.
func TestOwnRegistryStripsWhenSyncOff(t *testing.T) {
	s := Settings{RegistryOn: true, RegistryEffort: "high", RegistryBuckets: []string{"magpie-openai"}, RegistrySync: false}
	(&s).OwnRegistry()
	if s.RegistryOn || s.RegistryEffort != "" || s.RegistryBuckets != nil {
		t.Fatalf("an off machine's bundle still carries the registry: %+v", s)
	}
	if s.RegistrySync {
		t.Fatal("the switch itself must stay as it is")
	}
	(&s).OwnRegistry()
	if s.RegistryOn || s.RegistryEffort != "" || s.RegistryBuckets != nil {
		t.Fatalf("stripping twice changed something: %+v", s)
	}
	on := Settings{RegistryOn: true, RegistryEffort: "high", RegistryBuckets: []string{"magpie-openai"}, RegistrySync: true}
	(&on).OwnRegistry()
	if !on.RegistryOn || on.RegistryEffort != "high" || !slices.Equal(on.RegistryBuckets, []string{"magpie-openai"}) {
		t.Fatalf("an on machine's bundle lost the registry: %+v", on)
	}
}
