package provider

import "slices"

// endpointOf is the gateway path a protocol is asked at, for the three
// wires magpie translates between. Gemini's path is per model, so it is
// pathOf's to format, not an entry here.
var endpointOf = map[Protocol]string{Chat: "/v1/chat/completions", Responses: "/v1/responses", Anthropic: "/v1/messages"}

// NativeProtocols are the APIs a request for the model is relayed on to
// its provider as it is: relayed's three (the shared native-vs-translated
// decision /v1/models, an agent plugin's connect, GroupAPI and cursor's
// api_types go by) plus Gemini, when the provider serves the model on
// generateContent itself (speaksGemini). nil for a routing group, whose
// wires are its members' common translated ones (GroupEndpoints).
func NativeProtocols(e Entry) []Protocol {
	if e.Group != "" {
		return nil
	}
	out := relayed(e.Provider, e.Model)
	if speaksGemini(e.Provider, e.Model) {
		out = append(out, Gemini)
	}
	return out
}

// NativeEndpoints are the paths a request for the model is relayed on to
// its provider as it is (/v1/models' and an agent plugin's connect's
// "native_endpoints"): the APIs the provider serves it on. Gemini's
// generateContent path is keyed by the id the client sends (Entry.ID,
// what the agent sends magpie), as the gateway's Gemini route resolves
// the model in its path as a catalog id — never the vendor-facing Model.
// A routing group's are GroupEndpoints, which need its members; none
// here. None either for a model every request to is translated anyway (a
// subscription served through its agent's own API).
func NativeEndpoints(e Entry) []string {
	return endpointsOf(NativeProtocols(e), e.ID)
}

// relayed are the APIs p takes a request for model on as it is, in the
// order p speaks them: none for a model every request to is translated
// anyway.
func relayed(p Provider, model string) []Protocol {
	if p.Native(model) == "" {
		return nil
	}
	apis := p.APIs(model)
	var out []Protocol
	for _, pr := range p.Speaks() {
		if slices.Contains(Protocols, pr) && (apis == nil || slices.Contains(apis, pr)) {
			out = append(out, pr)
		}
	}
	return out
}

// speaksGemini is whether p serves model on Gemini's generateContent
// itself: p speaks Gemini — not a backend every request to is translated
// anyway (a Claude Code or Devin sign-in translates generateContent too;
// Code Assist never speaks Gemini) — and the model's known APIs, when its
// vendor said which serve it, name Gemini. Not relayed's to answer:
// relayed gates on Native, which picks among the three wires magpie
// translates between (Protocols), and a model served on generateContent
// alone has no native one of those — Gemini joins the native set as its
// own term. catalog.EndpointAPIs, which reads a vendor list's
// supported_endpoints into APIs, stays translate-once: no consumer asks
// generateContent there today, and a list that named only that leaves
// APIs nil, which already lets this term through by Speaks.
func speaksGemini(p Provider, model string) bool {
	if p.Account != nil {
		switch p.Account.Agent {
		case "claude", "devin":
			return false
		}
	}
	if !slices.Contains(p.Speaks(), Gemini) {
		return false
	}
	apis := p.APIs(model)
	return apis == nil || slices.Contains(apis, Gemini)
}

func endpointsOf(prs []Protocol, id string) []string {
	var out []string
	for _, pr := range prs {
		out = append(out, pathOf(pr, id))
	}
	return out
}

// pathOf is the gateway path a protocol is asked at, id the model's
// client-facing id (Entry.ID): Gemini's generateContent names the model
// in its path, as the gateway's Gemini route takes it.
func pathOf(pr Protocol, id string) string {
	if pr == Gemini {
		return "/v1beta/models/" + id + ":generateContent"
	}
	return endpointOf[pr]
}

// GroupEndpoints are the paths a request for a routing group is relayed
// on as it is to whichever of its members ms answers it: the APIs every
// member takes it on. None when a member translates every request anyway,
// or the members share no API. Groups ride the OpenAI-compatible wires:
// whatever the members share, only the three magpie translates between
// are pathed (an all-Gemini group yields no path), never Gemini's
// per-model one.
func GroupEndpoints(ms []Member) []string {
	var common []Protocol
	for i, m := range ms {
		r := relayed(m.Provider, m.Model)
		if i == 0 {
			common = r
			continue
		}
		common = slices.DeleteFunc(common, func(pr Protocol) bool { return !slices.Contains(r, pr) })
	}
	common = slices.DeleteFunc(common, func(pr Protocol) bool { return !slices.Contains(Protocols, pr) })
	// no id: only the fixed paths are left, which take none
	return endpointsOf(common, "")
}

// GroupAPI is the API an agent that asks each model on one API is wired
// to ask a routing group on, "" for Chat. It is Responses when a member
// is served best there (a ChatGPT sign-in, GPT on OpenAI's API or
// Copilot's, a relay given as a Responses URL) and every other member
// takes a Responses request as it is too, or translates every request
// anyway (a Claude Code subscription): no member is then translated that
// Chat would have relayed, and the ones best on Responses are relayed
// instead of translated from Chat (zyh6036 on X: a group of
// subscriptions that all serve Responses was asked on Chat, as only a
// group whose members were all best there went on Responses, #1594). A
// group with a member Responses doesn't reach (a Chat-only key, a
// Messages-only Claude) stays on Chat, so that member keeps being
// relayed; a Messages-only group stays on Chat too, as the thinking a
// Claude member takes is known by its own model, not by the group's id.
func GroupAPI(ms []Member) Protocol {
	best := false
	for _, m := range ms {
		n := m.Provider.Native(m.Model)
		if n == "" {
			continue
		}
		if !slices.Contains(relayed(m.Provider, m.Model), Responses) {
			return ""
		}
		best = best || n == Responses
	}
	if best {
		return Responses
	}
	return ""
}
