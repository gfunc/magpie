package gateway

import (
	"context"
	"fmt"

	"github.com/yetone/magpie/internal/provider"
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
// service is the one; a DeepSeek provider's tools API and a Remote
// magpie's web_search are to join here.
var serviceSearchers = []serviceSearcher{
	{url: provider.KimiCodeSearch, search: (*Server).kimiSearch},
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
