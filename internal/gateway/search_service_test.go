package gateway

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

// The service-searcher seam: one answer, at every place that treats one
// specially, to whether a provider searches through its own keyed service
// rather than a model. The Kimi Code plan is the one service today; a
// provider on it is a service choice with no model, is named with no
// model, passes the spent check, searches by its service when asked, and
// is named only by hand. A plain keyed provider does none of that.
func TestServiceSearcherSeam(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	setHome(t, t.TempDir()) // no signed-in agent searches
	kimi := newKimiPlan(t)
	if err := provider.Save(provider.Provider{ID: "kimi", Name: "Kimi Code", Preset: "kimi-code-cn", Key: "sk-kimi",
		Chat: kimi.URL + "/coding/v1", Models: []string{"kimi-for-coding"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "oai", Name: "OpenAI", Key: "sk-oai",
		Chat: "http://127.0.0.1:9/v1", Models: []string{"gpt-5-mini"}}); err != nil {
		t.Fatal(err)
	}
	of := func(id string) provider.Provider {
		i := slices.IndexFunc(provider.All(), func(p provider.Provider) bool { return p.ID == id })
		if i < 0 {
			t.Fatalf("no %s saved", id)
		}
		return provider.All()[i]
	}
	kimiP, oaiP := of("kimi"), of("oai")

	// the searcher list offers the plan as a service choice, with no model
	cs := Searchers()
	if len(cs) != 1 || cs[0].Provider.ID != "kimi" || !cs[0].Service || !cs[0].ManualOnly || cs[0].Small != "" || len(cs[0].Models) != 0 {
		t.Fatalf("choices %+v", cs)
	}

	// named in Settings, it searches with no model
	st := settings.Load()
	st.Searcher = "kimi"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	cp, model, why := chosenSearcher()
	if cp == nil || cp.ID != "kimi" || model != "" || why != "" {
		t.Fatalf("named %v %q %q", cp, model, why)
	}

	// the spent check passes it whatever model is named, while a plain
	// provider's key out of its allowance is held
	for _, id := range []string{"kimi", "oai"} {
		restingUntil.m[id] = time.Now().Add(time.Hour)
		restingUntil.note[id] = Rest{Why: failQuota}
	}
	t.Cleanup(func() {
		for _, id := range []string{"kimi", "oai"} {
			delete(restingUntil.m, id)
			delete(restingUntil.note, id)
		}
	})
	if err := searcherSpent(kimiP, "kimi-for-coding"); err != nil {
		t.Fatalf("the service held: %v", err)
	}
	if searcherSpent(kimiP, "") != nil {
		t.Fatal("the service held with no model")
	}
	if searcherSpent(oaiP, "gpt-5-mini") == nil {
		t.Fatal("a spent key not held")
	}

	// asked to search, it asks its service, not a model
	said, hits, err := New().searchBy(context.Background(), kimiP, "", "latest go", "")
	if err != nil || len(hits) != 1 || hits[0].URL != "https://go.dev/dl/" || !strings.Contains(said, "go1.27.1") {
		t.Fatalf("%q %+v %v", said, hits, err)
	}
	if len(kimi.searched) != 1 || !strings.HasPrefix(kimi.searched[0], "latest go | ") {
		t.Fatalf("searched %q", kimi.searched)
	}

	// and it comes last, named only by hand
	if r, manual := namedSearcherRank(kimiP); r != manualRelayRank || !manual {
		t.Fatalf("rank %d, manual %v", r, manual)
	}
	if r, manual := namedSearcherRank(oaiP); r != -1 || manual {
		t.Fatalf("a plain provider's rank %d, manual %v", r, manual)
	}
}
