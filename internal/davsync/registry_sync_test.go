package davsync

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/backup"
	"github.com/yetone/magpie/internal/settings"
)

// The registry's sync is a four-site protocol (design: registry-wires): the
// field set is five — On, Effort, Buckets, Groups, Sync — and moves only
// through CopyRegistryConfig at the strip, blind and merge sites, while the
// restore's landing rule names the fields for its per-machine Sync copy.
// Collect strips an off machine's config out of its bundle, the hash sites
// blind the whole set when this machine's switch is off, the merge site
// re-installs the server's fields verbatim (a legacy half stays a half: the
// merge never Saves, so nothing pre-migrates it), and the landing lets the
// config ride only when the bundle carried it whole — buckets and groups
// both — and the receiver opted in. These tests hold each site and the
// state table that joins them.

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

// regAbsent asserts none of the registry's keys is carried: not-carried is
// the absent key — groups absent, never a false — and the bucket list
// null-or-absent, never refilled.
func regAbsent(t *testing.T, s *settings.Settings) {
	t.Helper()
	m := regJSON(t, s)
	for _, k := range []string{"registryOn", "registryEffort", "registryGroups", "registrySync"} {
		if _, ok := m[k]; ok {
			t.Fatalf("a not-carried config has %s: %v", k, m)
		}
	}
	if m["registryBuckets"] != nil {
		t.Fatalf("a not-carried config has registryBuckets: %v", m)
	}
	if s.RegistryGroups != nil {
		t.Fatalf("not carried spelled groups as %v, want nil", *s.RegistryGroups)
	}
}

// An off machine's bundle never carries the registry's config — any of the
// five, the groups switch included — whichever way the bundle is filled:
// from the settings file, and from the settings.Load() fallback a machine
// with no settings file yet takes, Load being normalized and so carrying a
// whole config an off machine must not send. An on machine's carries it
// whole.
func TestRegistrySyncStripsAtCollect(t *testing.T) {
	off := false
	newComputer(t).use(t)
	saveReg(t, func(s *settings.Settings) {
		s.Theme, s.RegistryOn, s.RegistryEffort = "dark", true, "max"
		s.RegistryBuckets, s.RegistryGroups, s.RegistrySync = []string{"magpie-openai", "magpie-gemini"}, &off, false
	})
	b, err := collect(Config{Keys: true})
	if err != nil {
		t.Fatal(err)
	}
	regAbsent(t, b.Settings)

	saveReg(t, func(s *settings.Settings) { s.RegistrySync = true })
	if b, err = collect(Config{Keys: true}); err != nil {
		t.Fatal(err)
	}
	m := regJSON(t, b.Settings)
	if m["registryOn"] != true || m["registryEffort"] != "max" || m["registrySync"] != true {
		t.Fatalf("an on machine's bundle lost the config: %v", m)
	}
	if got, _ := json.Marshal(m["registryBuckets"]); string(got) != `["magpie-openai","magpie-gemini"]` {
		t.Fatalf("an on machine's bundle lost its wires: %s", got)
	}
	if m["registryGroups"] != false {
		t.Fatalf("an on machine's bundle lost the groups switch: %v", m)
	}

	// the fallback: no settings file at all (an empty HOME), so Load's
	// normalized whole config would travel with the switch off — the strip
	// a whole collect makes applies here too
	empty := newComputer(t)
	empty.use(t)
	if b, err = collect(Config{Keys: true}); err != nil {
		t.Fatal(err)
	}
	if b.Settings == nil {
		t.Fatal("the fallback filled nothing")
	}
	regAbsent(t, b.Settings)
	saveReg(t, func(s *settings.Settings) {
		s.RegistryOn, s.RegistryEffort = true, "max"
		s.RegistryBuckets, s.RegistryGroups, s.RegistrySync = []string{"magpie-openai"}, &off, false
	})
	if b, err = collect(Config{Keys: true}); err != nil {
		t.Fatal(err)
	}
	regAbsent(t, b.Settings)
}

// A machine whose switch is off hashes every bundle with the registry's
// whole field set zeroed: a registry-only change — anyone's, including its
// own flag flipping or the groups switch — is no change to it at all, and
// what its blinded copies carry is nil groups, never false, so a peer's nil
// and a spelled false hash the same to it (a false in a blinded copy would
// hash-differ against a peer's nil and phantom-restore). A machine whose
// switch is on sees every one of those changes. hashes itself is unchanged:
// an on machine hashes the bundle as it is.
func TestRegistrySyncHashBlindWhenOff(t *testing.T) {
	off := false
	bundle := func(flag bool, groups *bool) backup.Bundle {
		return backup.Bundle{Settings: &settings.Settings{Theme: "dark", RegistryOn: true, RegistryEffort: "max",
			RegistryBuckets: []string{"magpie-openai"}, RegistryGroups: groups, RegistrySync: flag}}
	}
	same := func(a, b backup.Bundle) bool { return hashes(a)["settings"] == hashes(b)["settings"] }
	groupsOff := bundle(false, &off)
	nilGroups := bundle(false, nil) // a legacy half: no groups switch said
	if !same(blindRegistry(groupsOff, true), blindRegistry(nilGroups, true)) {
		t.Fatal("an off machine saw the groups switch")
	}
	// the blinded copy is absent, not false — the raw JSON says so
	regAbsent(t, blindRegistry(groupsOff, true).Settings)
	if same(blindRegistry(groupsOff, false), blindRegistry(nilGroups, false)) {
		t.Fatal("an on machine missed the groups half-state")
	}
	flipped := bundle(true, &off)
	if !same(blindRegistry(groupsOff, true), blindRegistry(flipped, true)) {
		t.Fatal("an off machine saw the switch itself")
	}
	if same(blindRegistry(groupsOff, false), blindRegistry(flipped, false)) {
		t.Fatal("an on machine missed the switch flipping")
	}
	// a bundle from before the settings part at all passes through
	if got := blindRegistry(backup.Bundle{}, true).Settings; got != nil {
		t.Fatalf("a nil settings part became %v", got)
	}
}

// The strip is terminal: no code path re-normalizes an in-flight bundle
// before it is hashed or stored. A normal() after OwnRegistry would refill
// the buckets with all four wires and flip the groups on — un-strip it — so
// the next collect would hash differently from the copy the last sync
// stored, a difference with no reason behind it.
func TestRegistrySyncStripIsTerminal(t *testing.T) {
	off := false
	newComputer(t).use(t)
	saveReg(t, func(s *settings.Settings) {
		s.Theme, s.RegistryOn, s.RegistryEffort = "dark", true, "max"
		s.RegistryBuckets, s.RegistryGroups, s.RegistrySync = []string{"magpie-openai"}, &off, false
	})
	one, err := collect(Config{Keys: true})
	if err != nil {
		t.Fatal(err)
	}
	h := hashesOwn(blindRegistry(one, true), nil)
	two, err := collect(Config{Keys: true})
	if err != nil {
		t.Fatal(err)
	}
	if again := hashesOwn(blindRegistry(two, true), nil); again["settings"] != h["settings"] {
		t.Fatalf("collect isn't terminal: the same machine hashed %s then %s", h["settings"], again["settings"])
	}
	regAbsent(t, two.Settings) // still the stripped one: no refill before the hash
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

	on := true
	a := newComputer(t)
	a.use(t)
	saveReg(t, func(s *settings.Settings) {
		s.Theme = "dark"
		s.RegistryOn, s.RegistryEffort = true, "max"
		s.RegistryBuckets, s.RegistryGroups, s.RegistrySync = []string{"magpie-openai", "magpie-gemini"}, &on, true
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
	if m["registryOn"] != true || m["registryEffort"] != "max" || m["registrySync"] != true || m["registryGroups"] != true {
		t.Fatalf("the server's registry fields didn't survive b's push: %v", m)
	}
	if got, _ := json.Marshal(m["registryBuckets"]); string(got) != `["magpie-openai","magpie-gemini"]` {
		t.Fatalf("the server's wires didn't survive b's push: %s", got)
	}
	if s := settings.Load(); s.Theme != "light" || s.RegistrySync || s.RegistryOn {
		t.Fatalf("b's own settings after its push: %+v", s)
	}
}

// keepServersRegistry re-installs the server's fields verbatim: a legacy
// remote's half — no groups switch — stays a half. The merge hashes and
// pushes but never Saves, so no normal() runs on it: nothing on the sync
// path pre-migrates a bundle.
func TestRegistrySyncMergeCopiesLegacyHalfVerbatim(t *testing.T) {
	legacy := backup.Bundle{Settings: &settings.Settings{Theme: "light", RegistryOn: true, RegistryEffort: "max",
		RegistryBuckets: []string{"magpie-openai", "magpie-group"}, RegistrySync: true}}
	merged := backup.Bundle{Settings: &settings.Settings{Theme: "dark"}}
	keepServersRegistry(&merged, legacy, backup.Bundle{Settings: &settings.Settings{Theme: "dark", RegistrySync: false}})
	if merged.Settings.RegistryGroups != nil {
		t.Fatalf("the merge filled the groups in as %v; a legacy half stays verbatim", *merged.Settings.RegistryGroups)
	}
	if !merged.Settings.RegistryOn || merged.Settings.RegistryEffort != "max" || !merged.Settings.RegistrySync {
		t.Fatalf("the merge lost the server's fields: %+v", merged.Settings)
	}
	if got, _ := json.Marshal(merged.Settings.RegistryBuckets); string(got) != `["magpie-openai","magpie-group"]` {
		t.Fatalf("the merge touched the legacy id: %s", got)
	}
	m := regJSON(t, merged.Settings)
	if _, ok := m["registryGroups"]; ok {
		t.Fatalf("the merge wrote a groups switch into a legacy half: %v", m)
	}
}

// The state table, one cell per row, both ends through the sync the
// package's own tests use: the config lands only when the sender's bundle
// carried it whole and the receiver opted in; either switch off keeps the
// receiver's own config; the switch is every machine's own. The settings
// part itself travels in every row (the theme lands); what the table rules
// on is the registry's five fields.
func TestRegistrySyncStateTable(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		name                     string
		senderSync, receiverSync bool
		receiverBuckets          string // what the receiver ends with, joined
		receiverGroups           any    // the groups switch the receiver ends with
	}{
		{"off onto off", false, false, `["magpie-anthropic","magpie-openai-responses"]`, false},
		{"off onto on", false, true, `["magpie-anthropic","magpie-openai-responses"]`, false},
		{"on onto off", true, false, `["magpie-anthropic","magpie-openai-responses"]`, false},
		{"on onto on", true, true, `["magpie-openai","magpie-gemini"]`, true},
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
				s.RegistryBuckets, s.RegistryGroups = []string{"magpie-openai", "magpie-gemini"}, &on
				s.RegistrySync = tc.senderSync
			})
			if err := Configure(cfg); err != nil {
				t.Fatal(err)
			}
			if err := Now(ctx); err != nil {
				t.Fatal(err)
			}
			if tc.senderSync {
				remote, err := backup.Open(fake.files["/dav/magpie/magpie.magpie-backup"], "correct horse")
				if err != nil {
					t.Fatal(err)
				}
				if m := regJSON(t, remote.Settings); m["registryOn"] != true || m["registryGroups"] != true {
					t.Fatalf("the sender's bundle doesn't carry the whole config: %v", m)
				}
			}

			b := newComputer(t)
			b.use(t)
			saveReg(t, func(s *settings.Settings) {
				s.Theme = "light"
				s.RegistryOn, s.RegistryEffort = false, "low"
				s.RegistryBuckets, s.RegistryGroups = []string{"magpie-anthropic", "magpie-openai-responses"}, &off
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
			if s.RegistryGroups == nil || *s.RegistryGroups != (tc.receiverGroups == true) {
				t.Errorf("groups after %v, want %v", s.RegistryGroups, tc.receiverGroups)
			}
			landed := tc.receiverBuckets == `["magpie-openai","magpie-gemini"]`
			if want := effortOf(landed); s.RegistryEffort != want {
				t.Errorf("effort after %q, want %q", s.RegistryEffort, want)
			}
			if s.RegistryOn != landed {
				t.Errorf("served after %v, want %v", s.RegistryOn, landed)
			}
		})
	}

	// row four's groups-only sender: an empty wire list with the groups on
	// is a whole config, and it lands as one — `registryBuckets: []` on
	// the wire, the omitempty obligation
	t.Run("on onto on, groups-only", func(t *testing.T) {
		fake := &fakeDAV{files: map[string][]byte{}, etags: map[string]string{}, dirs: map[string]bool{"/dav": true}}
		srv := httptest.NewServer(fake)
		defer srv.Close()
		ctx := context.Background()
		cfg := Config{URL: srv.URL + "/dav/", User: "me", Password: "pw", Passphrase: "correct horse", Keys: true, Agents: true}

		a := newComputer(t)
		a.use(t)
		saveReg(t, func(s *settings.Settings) {
			s.Theme, s.RegistryOn, s.RegistryEffort = "dark", true, "max"
			s.RegistryBuckets, s.RegistryGroups, s.RegistrySync = []string{}, &on, true
		})
		if err := Configure(cfg); err != nil {
			t.Fatal(err)
		}
		if err := Now(ctx); err != nil {
			t.Fatal(err)
		}
		remote, err := backup.Open(fake.files["/dav/magpie/magpie.magpie-backup"], "correct horse")
		if err != nil {
			t.Fatal(err)
		}
		m := regJSON(t, remote.Settings)
		if got, _ := json.Marshal(m["registryBuckets"]); string(got) != `[]` {
			t.Fatalf("the groups-only sender's bundle lost its empty list: %s", got)
		}

		b := newComputer(t)
		b.use(t)
		saveReg(t, func(s *settings.Settings) {
			s.Theme = "light"
			s.RegistryOn, s.RegistryEffort = false, "low"
			s.RegistryBuckets, s.RegistryGroups, s.RegistrySync = []string{"magpie-anthropic"}, &off, true
		})
		if err := Configure(cfg); err != nil {
			t.Fatal(err)
		}
		if err := Now(ctx); err != nil {
			t.Fatal(err)
		}
		s := settings.Load()
		if got := join(s.RegistryBuckets); got != `[]` {
			t.Fatalf("buckets after %s, want the sender's empty list", got)
		}
		if s.RegistryGroups == nil || !*s.RegistryGroups {
			t.Fatalf("groups after %v, want the sender's on", s.RegistryGroups)
		}
	})
}

func join(xs []string) string {
	b, _ := json.Marshal(xs)
	return string(b)
}

func effortOf(landed bool) string {
	if landed { // the sender's whole config landed
		return "max"
	}
	return "low" // the receiver kept its own
}

// A legacy peer's bundle — the registry config as the pre-split shape
// carried it, the group tick inside the wire list and no groups switch —
// never lands (I5: ride is a whole-state test), and the copy the merge
// leaves on the server stays that half until the first upgraded peer's
// whole push overwrites it: convergence by overwrite, not by migration.
func TestRegistrySyncLegacyHalfNeverLandsUntilOverwritten(t *testing.T) {
	fake := &fakeDAV{files: map[string][]byte{}, etags: map[string]string{}, dirs: map[string]bool{"/dav": true}}
	sealed, err := backup.Seal(backup.Bundle{Version: backup.BundleVersion, Settings: &settings.Settings{
		Theme: "light", RegistryOn: true, RegistryEffort: "max",
		RegistryBuckets: []string{"magpie-openai", "magpie-group"}, RegistrySync: true}}, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	fake.files["/dav/magpie/magpie.magpie-backup"] = sealed
	srv := httptest.NewServer(fake)
	defer srv.Close()
	ctx := context.Background()
	cfg := Config{URL: srv.URL + "/dav/", User: "me", Password: "pw", Passphrase: "correct horse", Keys: true, Agents: true}

	off := false
	b := newComputer(t) // on, its own whole config
	b.use(t)
	saveReg(t, func(s *settings.Settings) {
		s.Theme = "dark"
		s.RegistryOn, s.RegistryEffort = true, "high"
		s.RegistryBuckets, s.RegistryGroups, s.RegistrySync = []string{"magpie-anthropic", "magpie-gemini"}, &off, true
	})
	if err := Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Now(ctx); err != nil { // joins over the half: the settings part lands, the registry never rides
		t.Fatal(err)
	}
	s := settings.Load()
	if s.Theme != "light" {
		t.Fatalf("the settings part didn't land: theme %q", s.Theme)
	}
	if !s.RegistryOn || s.RegistryEffort != "high" || join(s.RegistryBuckets) != `["magpie-anthropic","magpie-gemini"]` || s.RegistryGroups == nil || *s.RegistryGroups {
		t.Fatalf("the legacy half landed: %+v", s)
	}
	if fake.puts != 0 {
		t.Fatalf("the join pushed %d times over the legacy copy", fake.puts)
	}
	remote, err := backup.Open(fake.files["/dav/magpie/magpie.magpie-backup"], "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if m := regJSON(t, remote.Settings); m["theme"] != "light" {
		t.Fatalf("the server's copy changed on its own: %v", m)
	}

	// the first whole push overwrites the half, and only that push does
	saveReg(t, func(s *settings.Settings) { s.Theme = "dark" })
	if err := Now(ctx); err != nil {
		t.Fatal(err)
	}
	remote, err = backup.Open(fake.files["/dav/magpie/magpie.magpie-backup"], "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	m := regJSON(t, remote.Settings)
	if m["theme"] != "dark" {
		t.Fatalf("b's change didn't go up: %v", m)
	}
	if got, _ := json.Marshal(m["registryBuckets"]); string(got) != `["magpie-anthropic","magpie-gemini"]` {
		t.Fatalf("the whole push didn't overwrite the legacy id: %s", got)
	}
	if m["registryGroups"] != false {
		t.Fatalf("the whole push didn't carry the groups switch: %v", m)
	}

	// and from there the whole config rides: another on machine lands it
	c := newComputer(t)
	c.use(t)
	saveReg(t, func(s *settings.Settings) { s.RegistrySync = true })
	if err := Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Now(ctx); err != nil {
		t.Fatal(err)
	}
	s = settings.Load()
	if !s.RegistryOn || s.RegistryEffort != "high" || join(s.RegistryBuckets) != `["magpie-anthropic","magpie-gemini"]` || s.RegistryGroups == nil || *s.RegistryGroups {
		t.Fatalf("the overwritten config didn't land whole: %+v", s)
	}
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

	on := true
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
		s.RegistryBuckets, s.RegistryGroups, s.RegistrySync = []string{"magpie-openai", "magpie-gemini"}, &on, true
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

// The one-time hash reconvergence after the shape change: the reshaped
// fields (registryBuckets written with no omitempty, the new
// registryGroups) change the marshaled settings once at upgrade, and the
// sync that follows pushes once — accepted. From there the hash is stable:
// a save that changes nothing writes the same bytes, and the syncs after
// it push nothing, forever.
func TestRegistrySyncHashSettlesAfterTheShapeChange(t *testing.T) {
	fake := &fakeDAV{files: map[string][]byte{}, etags: map[string]string{}, dirs: map[string]bool{"/dav": true}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	ctx := context.Background()
	cfg := Config{URL: srv.URL + "/dav/", User: "me", Password: "pw", Passphrase: "correct horse", Keys: true, Agents: true}

	a := newComputer(t)
	a.use(t)
	// the stored file as an older magpie wrote it: the group tick inside
	// the wire list, no groups switch, buckets written with omitempty
	if err := os.MkdirAll(filepath.Dir(settings.Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"theme":"dark","registryOn":true,"registryEffort":"max","registryBuckets":["magpie-openai","magpie-group"],"registrySync":true}` + "\n"
	if err := os.WriteFile(settings.Path(), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Configure(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Now(ctx); err != nil {
		t.Fatal(err)
	}
	remote, err := backup.Open(fake.files["/dav/magpie/magpie.magpie-backup"], "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	pushed := remote.Settings

	// a save that changes nothing: load (the one migration) then save
	saveReg(t, func(s *settings.Settings) {})
	after, err := os.ReadFile(settings.Path())
	if err != nil {
		t.Fatal(err)
	}
	if s := settings.Load(); !s.RegistryOn || s.RegistryEffort != "max" || join(s.RegistryBuckets) != `["magpie-openai"]` || s.RegistryGroups == nil || !*s.RegistryGroups {
		t.Fatalf("the old shape didn't move onto the new one at load: %+v", s)
	}
	// what the push carried is what the settled file says: one shape, not two
	if got, _ := json.Marshal(regJSON(t, pushed)["registryBuckets"]); string(got) != `["magpie-openai"]` {
		t.Fatalf("the push carried the un-migrated id: %s", got)
	}
	puts := fake.puts
	for range 2 {
		saveReg(t, func(s *settings.Settings) {})
		again, err := os.ReadFile(settings.Path())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, again) {
			t.Fatal("a save that changes nothing rewrote the file: the hash never settles")
		}
		if err := Now(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if fake.puts != puts {
		t.Fatalf("a no-op save pushed %d times: the hash never settled", fake.puts-puts)
	}
}
