package gateway

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/budget"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/usage"
)

// A keyed service searches the web with a provider's own key rather than
// through a model: no model request is made and no model list is read. It
// appears in the searcher list as a service choice, is named in Settings
// with no model, passes the spent check, searches when asked, and is never
// picked by itself — only named. Where a provider is on one is decided
// here alone: a new keyed service joins serviceSearchers, and the sites
// that treat one specially ask the seam (search_kimi.go holds the Kimi
// Code plan, the one service today).

// serviceSearcher is a keyed service: where a provider on it searches,
// and how the service is asked.
type serviceSearcher struct {
	url    func(provider.Provider) string
	search func(*Server, context.Context, provider.Provider, string) (string, []Hit, error)
}

// serviceSearchers are the keyed services. A Kimi Code plan's search
// service, a DeepSeek provider's tools API and a Remote magpie's web_search
// are the three.
var serviceSearchers = []serviceSearcher{
	{url: provider.KimiCodeSearch, search: (*Server).kimiSearch},
	{url: DeepSeekSearch, search: (*Server).deepSeekSearch},
	{url: remoteMagpieService, search: (*Server).remoteMagpieSearch},
}

// serviceSearch is where p searches through its keyed service rather than
// a model: that service's endpoint, "" when p searches through a model.
func serviceSearch(p provider.Provider) string {
	for _, svc := range serviceSearchers {
		if u := svc.url(p); u != "" {
			return u
		}
	}
	return ""
}

// serviceSearchBy asks p's keyed service (serviceSearch) to search.
func (s *Server) serviceSearchBy(ctx context.Context, p provider.Provider, query string) (string, []Hit, error) {
	for _, svc := range serviceSearchers {
		if svc.url(p) != "" {
			return svc.search(s, ctx, p, query)
		}
	}
	return "", nil, fmt.Errorf("%s: no search service", p.Name)
}

// recordServiceSearch records the call a keyed service search made: one row
// for the caller's key, the provider and the key used, no tokens, the
// service's price on OwnCost — nil for a pass-through, which costs nothing
// here, the remote's own row carrying its spend. A failure or an empty
// result never gets here.
func (s *Server) recordServiceSearch(ctx context.Context, p provider.Provider, key string, cost *float64) {
	rec := usage.Record{Time: time.Now(), Kind: "web_search_api", Provider: p.ID, Host: p.Where(),
		ProviderKeyID: provider.KeyID(key), Status: http.StatusOK, OwnCost: cost}
	if f := searchFor(ctx); f != nil {
		rec.Agent = f.Agent
	}
	if who := access.Caller(ctx); who.KeyID != "" {
		rec.CallerKeyID, rec.CallerKeyName = who.KeyID, who.KeyName
	}
	budget.Append(rec)
}
