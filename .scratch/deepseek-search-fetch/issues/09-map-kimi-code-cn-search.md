# Map Kimi Code (China)'s search service on the 10.2.1.21 deployment

Status: resolved
Type: research

## Question

How does Kimi Code (China)'s web search work in magpie, and how is it configured on the remote deployment at `10.2.1.21`?

Code path:

- `provider.KimiCodeSearch` and `internal/gateway/search_kimi.go`: which endpoint, headers, auth, query and result shape, errors, quotas.
- `ownSearcher` / `searchesOwn`: when a Kimi Code plan searches for its own models versus for other models, and how Settings' Searcher names it as a "service" choice with no model.
- Is it model-free (no model tokens)? Is search included in the plan or billed separately?

Deployment `10.2.1.21`:

- Read-only: query `http://10.2.1.21:3425/v1/models` using the key in `~/.config/magpie/providers.json` (provider `bi-dev`). List the `kimi-code-cn` models and any search-related metadata.
- What does this deployment serve for `kimi-code-cn`? Does it expose a search service the same way?
- Never print the key and never mutate the deployment.

Comparison:

- Which shape would DeepSeek's `/v1/tools/search` need to match to be "provider-linked like Kimi Code" (a service searcher, no model), and which parts do not transfer?

## Answer

Kimi Code (China)'s search is a **service searcher**, not a model: a keyed plan sends `POST https://api.kimi.com/coding/v1/search` with `{"text_query","limit":6,"enable_page_crawling":false,"timeout_seconds":30}`, `Authorization: Bearer <plan key>`, `User-Agent: KimiCLI/1.52.0`, and optional `X-Msh-Tool-Call-Id`. It spends no model tokens and magpie records no cost; the vendor docs put it under the membership's shared quota. It runs first for the plan's own models (`ownSearcher`/`searchesOwn`), and for other models only when named in Settings, where it appears as `Service: true` with no model ("its web search").

On `10.2.1.21` (read-only GETs), the deployment serves four `kimi-code-cn/*` models (`kimi-for-coding`, `-highspeed`, `k3`, `k3-256k`, all `owned_by: kimi-code-cn`). Its kept quota snapshot has a keyed `kimi-code-cn` plan card (5 hours + 7 days), and the model list advertises `web_search: "magpie"` for those models, so the remote says it runs magpie's search for them. The search endpoint itself was not called (no quota spent), so that is inferred, not directly observed.

For DeepSeek, the service-searcher shape transfers: `DeepSeekSearch(p)` + a keyed `POST /v1/tools/search` caller + `Service: true` in `Searchers()` + the no-model `chosenSearcher`/`searcherSpent` branches. What does not transfer: Kimi's subscription-shared quota (DeepSeek is per-request billed, so cost recording and 429/402 handling are new), its own-models-first default (DeepSeek spends money per search, so manual-only or opt-in is safer), and fetch (magpie has no provider-linked fetch hook at all).

Full detail, quotes, and gaps: [Kimi Code (China)'s search service](../research/kimi-code-cn-search.md).
