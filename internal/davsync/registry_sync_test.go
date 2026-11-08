package davsync

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/yetone/magpie/internal/backup"
	"github.com/yetone/magpie/internal/settings"
)

// The registry's sync is a four-site protocol (design: registry-settings):
// collect strips an off machine's config out of its bundle, the hash sites
// blind all four registry fields when this machine's switch is off, the
// merge site puts the server's registry fields back over a stripped part an
// off machine pushes, and the restore landing lets the config ride only
// when the bundle carried it whole and the receiver opted in. These tests
// hold each site and the state table that joins them.

// saveReg changes this computer's settings through the same save the window
// makes.
func saveReg(t *testing.T, change func(*settings.Settings)) {
	t.Helper()
	s := settings.Load()
	change(&s)
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
}

// regJSON is a bundle's settings part as the raw JSON that travels.
func regJSON(t *testing.T, s *settings.Settings) map[string]any {
	t.Helper()
	out := map[string]any{}
	if s == nil {
		return out
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// An off machine's bundle never carries the registry's config, whichever
// way the bundle is filled: from the settings file, and from the
// settings.Load() fallback a machine with no settings file yet takes —
// Load is normalized, and would carry all four buckets with the switch
// off, half a state. An on machine's bundle carries it whole.
func TestRegistrySyncStripsAtCollect(t *testing.T) {
	newComputer(t).use(t)
	saveReg(t, func(s *settings.Settings) {
		s.Theme, s.RegistryOn, s.RegistryEffort = "dark", true, "max"
		s.RegistryBuckets, s.RegistrySync = []string{"magpie-openai", "magpie-group"}, false
	})
	b, err := collect(Config{Keys: true})
	if err != nil {
		t.Fatal(err)
	}
	m := regJSON(t, b.Settings)
	for _, k := range []string{"registryOn", "registryEffort", "registryBuckets", "registrySync"} {
		if _, ok := m[k]; ok {
			t.Fatalf("an off machine's bundle carries %s: %v", k, m)
		}
	}
	if b.Settings.RegistryBuckets != nil {
		t.Fatalf("the stripped marker: %v", b.Settings.RegistryBuckets)
	}

	saveReg(t, func(s *settings.Settings) { s.RegistrySync = true })
	if b, err = collect(Config{Keys: true}); err != nil {
		t.Fatal(err)
	}
	if m = regJSON(t, b.Settings); m["registryOn"] != true || m["registryEffort"] != "max" || m["registrySync"] != true {
		t.Fatalf("an on machine's bundle lost the config: %v", m)
	}

	// the fallback: no settings file at all (an empty HOME), so Load's
	// normalized all-four buckets would travel with the switch off
	empty := newComputer(t)
	empty.use(t)
	b, err = collect(Config{Keys: true})
	if err != nil {
		t.Fatal(err)
	}
	if b.Settings == nil {
		t.Fatal("the fallback filled nothing")
	}
	// the fallback machine has no settings file, so its switch reads off:
	// its bundle — filled from Load(), which is normalized (T1's
	// TestRegistryBucketsNilFillsAndEmptyStays holds that Load fills all
	// four buckets) — must come back without the config. That the strip is
	// what takes it out is this test's negative control: without the
	// fallback's OwnRegistry the bundle carries registryBuckets.
	if m := regJSON(t, b.Settings); m["registryBuckets"] != nil || m["registryOn"] != nil {
		t.Fatalf("the fallback path carries the config: %v", m)
	}
	saveReg(t, func(s *settings.Settings) {
		s.RegistryOn, s.RegistryEffort = true, "max"
		s.RegistryBuckets, s.RegistrySync = []string{"magpie-openai"}, false
	})
	if b, err = collect(Config{Keys: true}); err != nil {
		t.Fatal(err)
	}
	if m = regJSON(t, b.Settings); m["registryBuckets"] != nil || m["registryOn"] != nil {
		t.Fatalf("a set config on the fallback machine travels: %v", m)
	}
}

// A machine whose switch is off hashes every bundle with all four registry
// fields zeroed: a registry-only change — anyone's, including its own flag
// flipping — is no change to it at all. A machine whose switch is on sees
// the same change. hashes itself is unchanged: an on machine hashes the
// bundle as it is.
func TestRegistrySyncHashBlindWhenOff(t *testing.T) {
	bundle := func(flag, on bool, effort string, buckets []string) backup.Bundle {
		return backup.Bundle{Settings: &settings.Settings{Theme: "dark", RegistryOn: on, RegistryEffort: effort, RegistryBuckets: buckets, RegistrySync: flag}}
	}
	same := func(a, b backup.Bundle) bool { return hashes(a)["settings"] == hashes(b)["settings"] }
	withConfig := bundle(false, true, "max", []string{"magpie-openai"})
	without := bundle(false, false, "", nil)
	if !same(blindRegistry(withConfig, true), blindRegistry(without, true)) {
		t.Fatal("an off machine saw a registry-only change")
	}
	flipped := bundle(true, true, "max", []string{"magpie-openai"})
	if !same(blindRegistry(withConfig, true), blindRegistry(flipped, true)) {
		t.Fatal("an off machine saw the switch itself")
	}
	if same(blindRegistry(withConfig, false), blindRegistry(without, false)) {
		t.Fatal("an on machine missed a registry-only change")
	}
	if same(blindRegistry(withConfig, false), blindRegistry(flipped, false)) {
		t.Fatal("an on machine missed the switch flipping")
	}
	// a bundle from before the settings part at all passes through
	if got := blindRegistry(backup.Bundle{}, true).Settings; got != nil {
		t.Fatalf("a nil settings part became %v", got)
	}
}

// The merge site: an off machine winning a settings conflict pushes its
// part with the server's registry fields re-installed — its own change
// (the theme) goes up, the peer's config and the group's switch survive,
// and the off machine's own settings are untouched. Without the rule the
// push would erase the config from the server for good: the on peer's next
// sync sees no difference and never puts it back.
func TestRegistrySyncMergeKeepsServersConfig(t *testing.T) {
	fake := &fakeDAV{files: map[string][]byte{}, etags: map[string]string{}, dirs: map[string]bool{"/dav": true}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	ctx := context.Background()
	cfg := Config{URL: srv.URL + "/dav/", User: "me", Password: "pw", Passphrase: "correct horse", Keys: true, Agents: true}

	a := newComputer(t)
	a.use(t)
	saveReg(t, func(s *settings.Settings) {
		s.Theme = "dark"
		s.RegistryOn, s.RegistryEffort = true, "max"
		s.RegistryBuckets, s.RegistrySync = []string{"magpie-openai", "magpie-group"}, true
	})
	if err := Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Now(ctx); err != nil {
		t.Fatal(err)
	}

	b := newComputer(t)
	b.use(t)
	if err := Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Now(ctx); err != nil { // joins: a's settings come in, b keeps its own registry (none) and its off switch
		t.Fatal(err)
	}
	if s := settings.Load(); s.RegistrySync || s.RegistryOn {
		t.Fatalf("joining gave b the registry: %+v", s)
	}
	saveReg(t, func(s *settings.Settings) { s.Theme = "light" }) // b's own change, nothing of the registry's
	puts := fake.puts
	if err := Now(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.puts != puts+1 {
		t.Fatalf("b's change pushed %d times, want once", fake.puts-puts)
	}
	remote, err := backup.Open(fake.files["/dav/magpie/magpie.magpie-backup"], "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	m := regJSON(t, remote.Settings)
	if m["theme"] != "light" {
		t.Fatalf("b's change didn't go up: %v", m)
	}
	if m["registryOn"] != true || m["registryEffort"] != "max" || m["registrySync"] != true {
		t.Fatalf("the server's registry fields didn't survive b's push: %v", m)
	}
	if got, _ := json.Marshal(m["registryBuckets"]); string(got) != `["magpie-openai","magpie-group"]` {
		t.Fatalf("the server's buckets didn't survive b's push: %s", got)
	}
	if s := settings.Load(); s.Theme != "light" || s.RegistrySync || s.RegistryOn {
		t.Fatalf("b's own settings after its push: %+v", s)
	}
}

// The state table, one cell per row, both ends through the sync the
// package's own tests use: the config lands only when the sender's bundle
// carried it and the receiver opted in; either switch off keeps the
// receiver's own config; the switch is every machine's own. The settings
// part itself travels in every row (the theme lands); what the table rules
// on is the registry's four fields.
func TestRegistrySyncStateTable(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		senderSync, receiverSync bool
		receiverBuckets          string // what the receiver ends with, joined
	}{
		{"off onto off", false, false, `["magpie-anthropic","magpie-openai-responses"]`},
		{"off onto on", false, true, `["magpie-anthropic","magpie-openai-responses"]`},
		{"on onto off", true, false, `["magpie-anthropic","magpie-openai-responses"]`},
		{"on onto on", true, true, `["magpie-openai","magpie-group"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeDAV{files: map[string][]byte{}, etags: map[string]string{}, dirs: map[string]bool{"/dav": true}}
			srv := httptest.NewServer(fake)
			defer srv.Close()
			ctx := context.Background()
			cfg := Config{URL: srv.URL + "/dav/", User: "me", Password: "pw", Passphrase: "correct horse", Keys: true, Agents: true}

			a := newComputer(t)
			a.use(t)
			saveReg(t, func(s *settings.Settings) {
				s.Theme = "dark"
				s.RegistryOn, s.RegistryEffort = true, "max"
				s.RegistryBuckets = []string{"magpie-openai", "magpie-group"}
				s.RegistrySync = tc.senderSync
			})
			if err := Configure(cfg); err != nil {
				t.Fatal(err)
			}
			if err := Now(ctx); err != nil {
				t.Fatal(err)
			}
			if tc.senderSync {
				if remote, err := backup.Open(fake.files["/dav/magpie/magpie.magpie-backup"], "correct horse"); err != nil {
					t.Fatal(err)
				} else if m := regJSON(t, remote.Settings); m["registryOn"] != true {
					t.Fatalf("the sender's bundle doesn't carry the config: %v", m)
				}
			}

			b := newComputer(t)
			b.use(t)
			saveReg(t, func(s *settings.Settings) {
				s.Theme = "light"
				s.RegistryOn, s.RegistryEffort = false, "low"
				s.RegistryBuckets = []string{"magpie-anthropic", "magpie-openai-responses"}
				s.RegistrySync = tc.receiverSync
			})
			if err := Configure(cfg); err != nil {
				t.Fatal(err)
			}
			if err := Now(ctx); err != nil {
				t.Fatal(err)
			}
			s := settings.Load()
			if s.Theme != "dark" {
				t.Errorf("the settings part didn't land: theme %q", s.Theme)
			}
			if s.RegistrySync != tc.receiverSync {
				t.Errorf("flag after: %v, want the receiver's own %v", s.RegistrySync, tc.receiverSync)
			}
			if got := join(s.RegistryBuckets); got != tc.receiverBuckets {
				t.Errorf("buckets after %s, want %s", got, tc.receiverBuckets)
			}
			if want := effortOf(tc.receiverBuckets); s.RegistryEffort != want {
				t.Errorf("effort after %q, want %q", s.RegistryEffort, want)
			}
			if want := tc.receiverBuckets == `["magpie-openai","magpie-group"]`; s.RegistryOn != want {
				t.Errorf("served after %v, want %v", s.RegistryOn, want)
			}
		})
	}
}

func join(xs []string) string {
	b, _ := json.Marshal(xs)
	return string(b)
}

func effortOf(buckets string) string {
	if buckets == `["magpie-openai","magpie-group"]` { // the sender's whole config landed
		return "max"
	}
	return "low" // the receiver kept its own
}

// The mixed pair reaches a fixed point: the off machine is blind to the
// registry's account (a registry-only change on the server is no change to
// it — nothing brought in, nothing pushed), and the on machine's push is
// what carried the config up. No push/pull loop on either side.
func TestRegistrySyncMixedPairFixedPoint(t *testing.T) {
	fake := &fakeDAV{files: map[string][]byte{}, etags: map[string]string{}, dirs: map[string]bool{"/dav": true}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	ctx := context.Background()
	cfg := Config{URL: srv.URL + "/dav/", User: "me", Password: "pw", Passphrase: "correct horse", Keys: true, Agents: true}

	a := newComputer(t) // off: serves its own registry, never syncs it
	a.use(t)
	saveReg(t, func(s *settings.Settings) {
		s.Theme, s.RegistrySync = "dark", false
	})
	if err := Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Now(ctx); err != nil {
		t.Fatal(err)
	}

	b := newComputer(t) // on: joins, then sets the shared config
	b.use(t)
	if err := Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Now(ctx); err != nil {
		t.Fatal(err)
	}
	saveReg(t, func(s *settings.Settings) {
		s.RegistryOn, s.RegistryEffort = true, "max"
		s.RegistryBuckets, s.RegistrySync = []string{"magpie-openai", "magpie-group"}, true
	})
	if err := Now(ctx); err != nil { // b's own change: its whole settings, config included, go up
		t.Fatal(err)
	}
	remote, err := backup.Open(fake.files["/dav/magpie/magpie.magpie-backup"], "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if m := regJSON(t, remote.Settings); m["registryOn"] != true || m["registrySync"] != true {
		t.Fatalf("the on machine's config isn't on the server: %v", m)
	}
	puts := fake.puts
	if err := Now(ctx); err != nil { // b again: a fixed point
		t.Fatal(err)
	}
	if fake.puts != puts {
		t.Fatalf("b pushed again: %d writes", fake.puts-puts)
	}

	// a: the registry-only change on the server is invisible to it
	a.use(t)
	puts = fake.puts
	if err := Now(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.puts != puts {
		t.Fatalf("the off machine pushed on the registry's account: %d writes", fake.puts-puts)
	}
	if v := Status(); v.Notice != nil {
		t.Fatalf("the off machine brought the registry in: %+v", v.Notice)
	}
	s := settings.Load()
	if s.RegistrySync || s.RegistryOn || s.RegistryEffort != "" {
		t.Fatalf("the off machine took the config: %+v", s)
	}
}
