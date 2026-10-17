package gui

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

// The Web search section's "Fetch pages with" (the settings' search state):
// the fetch value beside the searcher's, empty for magpie's own downloader,
// the eligible keyed DeepSeek providers as the picker's choices, and why a
// named one isn't used — gone, off or keyless — so the page can say
// magpie's own downloader reads in its place.
func TestFetchSelectorDefaultsOwn(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))

	// nothing set up: fetch names magpie's own downloader, and no provider
	// is offered
	state := func() settingsJSON {
		var s settingsJSON
		s.Settings = settings.Load()
		searchState(&s)
		return s
	}
	s := state()
	if s.Fetch != "" || len(s.FetchChoices) != 0 || s.FetchUnused != "" {
		t.Fatalf("empty setup: %q %+v %q", s.Fetch, s.FetchChoices, s.FetchUnused)
	}

	// a keyed DeepSeek provider is offered by its name; another vendor's
	// provider, and a keyless DeepSeek, are not
	if err := provider.Save(provider.Provider{ID: "other", Name: "Other", Key: "k",
		Chat: "https://api.other.example/v1", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "ds", Name: "DeepSeek", Preset: "deepseek", Key: "sk-ds",
		Chat: "https://api.deepseek.com/v1", Models: []string{"deepseek-chat"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "nk", Name: "DeepSeek, no key", Preset: "deepseek",
		Chat: "http://127.0.0.1:9/v1", Models: []string{"deepseek-chat"}}); err != nil {
		t.Fatal(err)
	}
	s = state()
	if len(s.FetchChoices) != 1 || s.FetchChoices[0].ID != "ds" || s.FetchChoices[0].Name != "DeepSeek" {
		t.Fatalf("choices %+v", s.FetchChoices)
	}

	// naming it is the value the page saves and reads back, with no warning
	set := func(v string) {
		st := settings.Load()
		st.Fetch = v
		if err := settings.Save(st); err != nil {
			t.Fatal(err)
		}
	}
	set("ds")
	s = state()
	if s.Fetch != "ds" || s.FetchUnused != "" {
		t.Fatalf("named: %q %q", s.Fetch, s.FetchUnused)
	}
	b, _ := json.Marshal(s)
	if !strings.Contains(string(b), `"fetch":"ds"`) || !strings.Contains(string(b), `"fetchChoices":[{"id":"ds"`) {
		t.Fatalf("the search state's JSON: %s", b)
	}

	// gone: the search state says why the named one isn't used
	set("vanished")
	s = state()
	if s.FetchUnused != gateway.SearcherGone {
		t.Fatalf("gone: unused %q", s.FetchUnused)
	}

	// off
	if err := provider.Save(provider.Provider{ID: "ds", Name: "DeepSeek", Preset: "deepseek", Key: "sk-ds",
		Chat: "https://api.deepseek.com/v1", Models: []string{"deepseek-chat"}, Off: true}); err != nil {
		t.Fatal(err)
	}
	set("ds")
	s = state()
	if s.FetchUnused != gateway.SearcherOff {
		t.Fatalf("off: unused %q", s.FetchUnused)
	}

	// keyless, and not offered
	set("nk")
	s = state()
	if s.FetchUnused != gateway.SearcherCant {
		t.Fatalf("keyless: unused %q", s.FetchUnused)
	}
	// with the keyed one still off, nobody is offered
	if len(s.FetchChoices) != 0 {
		t.Fatalf("choices %+v", s.FetchChoices)
	}

	// the provider back on and named: used again
	if err := provider.Save(provider.Provider{ID: "ds", Name: "DeepSeek", Preset: "deepseek", Key: "sk-ds",
		Chat: "https://api.deepseek.com/v1", Models: []string{"deepseek-chat"}}); err != nil {
		t.Fatal(err)
	}
	set("ds")
	s = state()
	if s.FetchUnused != "" || len(s.FetchChoices) != 1 {
		t.Fatalf("used again: %q %+v", s.FetchUnused, s.FetchChoices)
	}
}
