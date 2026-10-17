# DeepSeek server-side search: support matrix and result shapes

Verified 2026-10-17 against DeepSeek's API docs, DeepSeek's news/change log,
Anthropic's web-search reference, and magpie's existing code and fixtures.
Anything the primary sources do not state is marked **not verified**.

## Sources

- https://api-docs.deepseek.com/guides/anthropic_api — Anthropic-compatible endpoint
- https://api-docs.deepseek.com/quick_start/agent_integrations/claude_code — Claude Code integration and web search
- https://api-docs.deepseek.com/guides/responses_api — Responses API compatibility
- https://api-docs.deepseek.com/api/create-response — Responses API reference
- https://api-docs.deepseek.com/quick_start/agent_integrations/codex — Codex integration and web search
- https://api-docs.deepseek.com/guides/search — Search & Fetch guide (schemas, billing, limits)
- https://api-docs.deepseek.com/api/search — Search API reference
- https://api-docs.deepseek.com/api/list-models — Models API (no search capability flag)
- https://api-docs.deepseek.com/quick_start/pricing — model and Search/Fetch pricing
- https://api-docs.deepseek.com/news/news261017 — first-party 2026-10-17 announcement
- https://api-docs.deepseek.com/updates — change log, 2026-10-17 entry
- https://platform.claude.com/docs/en/agents-and-tools/tool-use/web-search-tool — canonical Anthropic `web_search_20250305` shape
- `internal/gateway/search_found_test.go`, `internal/gateway/search.go`,
  `internal/gateway/anthropic.go`, `internal/gateway/gateway.go` — magpie's existing paths

## Bottom line

| Protocol | Server-side search | Which models | What happens when unsupported |
| --- | --- | --- | --- |
| Anthropic Messages (`https://api.deepseek.com/anthropic`) | Yes, via the Anthropic web-search tool blocks | `deepseek-flash` documented end to end; `deepseek-v4-pro` **not verified** (no model restriction is documented) | Not documented |
| Responses (`https://api.deepseek.com`) | Yes, `web_search` | `deepseek-v4-pro` only | `deepseek-flash`: tool is **silently ignored**, no error and no note; the model answers without searching |
| Chat Completions (`https://api.deepseek.com`) | No server-side search | Neither model | Tool calls are client-side; no `web_search` tool is documented |

The protocol restriction and the model restriction are different things:
Responses restricts `web_search` by model; the Anthropic endpoint documents the
tool blocks at endpoint level with no model qualifier. There is no published
model × protocol matrix beyond that.

## 1. Anthropic-compatible endpoint

### What DeepSeek documents

The endpoint is `https://api.deepseek.com/anthropic`:

> "our API has added support for the Anthropic API format, with the `base_url`
> being `https://api.deepseek.com/anthropic`."
> — [Anthropic API guide](https://api-docs.deepseek.com/guides/anthropic_api)

The compatibility table marks the search tool's wire blocks supported, with no
model qualifier:

> `array, type = "server_tool_use"` — **Supported**
> `array, type = "web_search_tool_result"` — **Supported**
> — [Anthropic API guide](https://api-docs.deepseek.com/guides/anthropic_api)

The same table says `citations` on an **input** text block are ignored, and
`mcp_tool_use` / `mcp_tool_result` / `code_execution_tool_result` are not
supported. It does not say anything about output citations, result counts, or
which model runs the search.

Claude Code is the only end-to-end Anthropic-side example DeepSeek publishes,
and its recommended configuration uses `deepseek-flash`:

> "The DeepSeek API natively supports the Web Search feature in Claude Code.
> When using Claude Code, if the model determines that your question requires
> a web search, it will invoke the Web Search tool and perform the search
> through the API provided by DeepSeek."
> — [Integrate with Claude Code](https://api-docs.deepseek.com/quick_start/agent_integrations/claude_code)

The setup block sets `ANTHROPIC_MODEL=deepseek-flash[1m]`,
`ANTHROPIC_DEFAULT_OPUS_MODEL=deepseek-flash[1m]`, and
`ANTHROPIC_DEFAULT_SONNET_MODEL=deepseek-flash[1m]` (same page). So
`deepseek-flash` is positively documented for Anthropic-side search.

**Which models:** `deepseek-flash` — documented. `deepseek-v4-pro` — **not
verified**. The endpoint compatibility table applies to both models and does
not exclude Pro, and Pro is a first-class model on the Anthropic API
(`claude-opus*` names map to it), but no DeepSeek page states or demonstrates
that Pro executes `web_search_20250305`.

### The tool version is not named by DeepSeek

`web_search_20250305` does not appear anywhere in DeepSeek's docs. The
Anthropic guide names the block types, and the Claude Code page says "Web
Search feature", but neither spells out the tool type. The version comes from
Anthropic's protocol and from magpie's fixture, not from DeepSeek's text
(**not verified** as a DeepSeek-specific claim).

### Result blocks

DeepSeek publishes no Anthropic response sample. The canonical Anthropic shape,
which DeepSeek says it accepts, is:

```json
{"type":"server_tool_use","id":"srvtoolu_...","name":"web_search","input":{"query":"..."}}
{"type":"web_search_tool_result","tool_use_id":"srvtoolu_...","content":[
  {"type":"web_search_result","url":"https://...","title":"...",
   "encrypted_content":"...","page_age":"..."}
]}
```

> "Search results include: `url` ... `title` ... `page_age` ... `encrypted_content`"
> — [Anthropic web search reference](https://platform.claude.com/docs/en/agents-and-tools/tool-use/web-search-tool)

Anthropic's reference also shows `text` blocks with `citations` of type
`web_search_result_location` (`url`, `title`, `encrypted_index`, `cited_text`).
There are **no snippets** in the Anthropic result shape: the result block
carries title, URL, age, and sealed content only; citation text is at most 150
characters of the cited passage.

Magpie's fixture records DeepSeek's observed response as exactly this shape,
with DeepSeek-sealed `encrypted_content` and the model `deepseek-flash`:

```go
// fakeDeepSeek ... Anthropic runs web_search_20250305 and answers
// web_search_tool_result blocks with DeepSeek's encrypted_content.
{"type":"server_tool_use","id":"call_00_x","name":"web_search","input":{"query":"今天上海天气"},"caller":{"type":"direct"}},
{"type":"web_search_tool_result","tool_use_id":"call_00_x","content":[
  {"type":"web_search_result","title":"上海天气预报","url":"...","encrypted_content":"DEEPSEEK-SEALED","page_age":null},
  {"type":"web_search_result","title":"上海当前天气","url":"...","encrypted_content":"DEEPSEEK-SEALED-2","page_age":"October 3, 2026"}]},
{"type":"text","text":"上海今天小雨，22/17℃。"}
```
— `internal/gateway/search_found_test.go:19-38`

The fixture has no citations on the text block. That is one fixture, not a
documented guarantee: **output citations from DeepSeek are not verified**.
Likewise, result counts and whether `max_uses` is honoured are **not
verified**; DeepSeek's compatibility table lists only `name`, `input_schema`,
and `description` under `tools`.

### Does magpie's pass-through have enough to render them?

- **Same-protocol Anthropic turn:** `passthrough` relays the upstream bytes
  (`gateway.go:3588-3590`), and its Anthropic branch only adjusts advisor
  tools, metadata, billing lines, thinking, and effort — it does not rewrite
  `web_search_*` blocks (`gateway.go:4123-4161`). The client gets DeepSeek's
  blocks as sent, including `encrypted_content`, which is what Anthropic
  clients expect to replay on the next turn.
- **Cross-protocol or supplemented search:** magpie decodes
  `web_search_tool_result` into `Hit{Title, URL, PageAge}` — title, URL, and
  age only, explicitly skipping `encrypted_content` (`search.go:824-843`) —
  and re-encodes `web_search_result` with `encrypted_content: ""`
  (`anthropic.go:1616-1628`). Titles, links, and page ages render; snippets and
  citations do not exist to render; sealed content is deliberately dropped
  because it is valid only at the vendor that produced it.

So the pass-through path has enough for titles, URLs, and ages. It does not
have snippets or citation offsets, because the Anthropic protocol does not
carry them in the result block.

## 2. Responses API

### The restriction is explicit

> "`web_search` | Supported with `deepseek-v4-pro`. The API runs the search on
> the server, adds the results to the context, and continues the response.
> Ignored with `deepseek-flash`"
> — [Responses API guide](https://api-docs.deepseek.com/guides/responses_api)

The API reference repeats it: "`web_search` is supported with
`deepseek-v4-pro`" and the tool type is "`web_search` (`deepseek-v4-pro`
only)" ([Responses API reference](https://api-docs.deepseek.com/api/create-response)).

The streaming events are Pro-only too:

> "`response.web_search_call.in_progress` / `response.web_search_call.searching`
> / `response.web_search_call.completed` — A server-side `web_search` call
> starts / is running / completes (`deepseek-v4-pro` only)"
> — [Responses API guide](https://api-docs.deepseek.com/guides/responses_api)

### What happens when flash ignores it

The guide says unsupported parameters are silent:

> "Unsupported parameters are **silently ignored** and do not cause errors, so
> existing Responses API clients can connect without modification."
> — [Responses API guide](https://api-docs.deepseek.com/guides/responses_api)

The tools table uses "Ignored with `deepseek-flash`". There is no documented
error, warning, or note in the response, and the request is otherwise valid, so
the turn proceeds without a search; the docs describe no failure for this case.
This is documented behaviour, not an empirical observation here; no API call
was made. **What the answer text says about the missing tool is not verified**
— the docs promise no error, not any particular wording.

### Codex confirms the same rule in prose

> "Built-in web search is disabled by the setup script and is available only
> with deepseek-v4-pro."
> "Codex's built-in web search is available only when Codex uses
> deepseek-v4-pro."
> "The example configuration above also disables web search because it uses
> deepseek-flash."
> — [Integrate with Codex](https://api-docs.deepseek.com/quick_start/agent_integrations/codex)

**Documentation inconsistency:** the same page's `~/.codex/models.json` sample
sets `"supports_search_tool": true` for `deepseek-flash` and `false` for
`deepseek-v4-pro` (`web_search_tool_type` is `"text"` for both). That field
contradicts the page's prose, the Responses guide, and the API reference. The
prose is specific and repeated; treat the catalog field as stale/incorrect
unless tested. **Which one Codex actually honours is not verified.**

### Output shape

DeepSeek documents the output item name and replay, not its fields:

- Output item type `web_search_call` is listed among response output items
  ([Responses API reference](https://api-docs.deepseek.com/api/create-response)).
- "`web_search_call` items returned by a previous response can be passed back
  in `input`; their search results are restored and concatenated into the
  context." ([Responses API guide](https://api-docs.deepseek.com/guides/responses_api))

OpenAI's Responses shape (`action.query`, `action.sources`, and
`url_citation` annotations on message text) is the reference DeepSeek says it
is compatible with, but DeepSeek does not document those fields. **The exact
`web_search_call` payload and whether citations/annotations come back are not
verified.** Magpie already asks for `include: ["web_search_call.action.sources"]`
on non-xAI hosts (`responses.go:1215-1216`), which assumes the OpenAI shape but
does not prove DeepSeek honours it.

## 3. Is the Anthropic endpoint model-restricted like Responses?

No published matrix exists. The evidence:

- The Anthropic compatibility table marks the search blocks supported without a
  model qualifier.
- The Claude Code page says Anthropic-side web search works, and its config uses
  `deepseek-flash`.
- No DeepSeek page says `deepseek-v4-pro` lacks it.
- No DeepSeek page says `deepseek-v4-pro` has it either.

So: **documented for `deepseek-flash`; `deepseek-v4-pro` not verified**. If
the spec needs Pro on Anthropic, it must be tested against a real key before
relying on it.

For DeepSeek Harness, the only first-party statement is the billing note that
server-side tool calls through Harness "already hit the Search and Fetch APIs"
([Search & Fetch guide](https://api-docs.deepseek.com/guides/search)). The
Harness docs site does not publish a search-provider page in its guide index,
so **which model or protocol Harness uses for search is not verified**.

### Model × protocol table

| Model | Chat Completions | Responses `web_search` | Anthropic web search |
| --- | --- | --- | --- |
| `deepseek-flash` | No server-side search; client-side tool calls only | **Ignored silently**; model answers without search | **Supported** (Claude Code guide; compatibility table) |
| `deepseek-v4-pro` | No server-side search; client-side tool calls only | **Supported** (guide + API reference) | **Not verified**; endpoint table has no model qualifier |

Per-agent mapping from DeepSeek's own docs: Claude Code → Anthropic →
`deepseek-flash` configured; Codex → Responses → `deepseek-v4-pro` for web
search.

## 4. Server-side result vs standalone `POST /v1/tools/search`

The standalone API returns a much richer object. From the
[Search & Fetch guide](https://api-docs.deepseek.com/guides/search) and
[Search API reference](https://api-docs.deepseek.com/api/search):

| Field | Standalone `results[]` | Anthropic `web_search_result` | Responses `web_search_call` |
| --- | --- | --- | --- |
| `title` | yes | yes | not documented |
| `url` | yes | yes | sources only if the OpenAI-style `action.sources` is honoured (**not verified**) |
| `snippet` (100–200 chars) | yes | **no** | not documented |
| `summary` (600–800 chars) | yes | **no** | not documented |
| `content` | yes when `need_content` | **no** | no |
| `rerank_score` (0–3) | yes | no | no |
| `site_name`, `site_icon` | yes | no | no |
| `date` (RFC 3339) | yes | `page_age` instead (human-readable, nullable) | not documented |
| `authority` (0–4, -1 unknown) | yes | no | no |
| `encrypted_content` | no | yes (vendor-sealed; must be replayed) | no |
| citation offsets / `cited_text` | no | Anthropic adds `web_search_result_location` citations; DeepSeek output **not verified** | OpenAI-style `url_citation` **not verified** |
| Count | `count` 1–50, default 10 | not documented (tool `max_uses` is not in DeepSeek's compatibility list) | not documented |

DeepSeek says the server-side path hits the same APIs:

> "when you use the DeepSeek API through DeepSeek Harness, Claude Code, or
> Codex, server-side tool calls already hit the Search and Fetch APIs."
> — [Search & Fetch guide](https://api-docs.deepseek.com/guides/search)

So the backend is the same in-house engine, but the wire shape is not: the
standalone result's snippet, summary, content, score, site metadata, date, and
authority are not available to a client reading Anthropic or Responses blocks.

## 5. Free window vs standalone billing

Standalone Search/Fetch are billed now, per successful request:

> "Search and Fetch are billed per successful request, not by tokens or by the
> number of returned results" — $0.75 per 1,000 successful requests.
> "A request is billable only when it returns HTTP 200 and produces a result
> (Search must return a non-empty `results` array, and Fetch must return page
> content). Failed requests, empty search results, and timeouts are not billed."
> — [Search & Fetch guide](https://api-docs.deepseek.com/guides/search)

The free window is only for server-side tool calls:

> "These calls are currently free of charge; per-request billing starts at
> 00:00 Beijing time on November 1, 2026."
> — [Search & Fetch guide](https://api-docs.deepseek.com/guides/search) and
> [Models & Pricing](https://api-docs.deepseek.com/quick_start/pricing)

The announcement gives the same instant in UTC:

> "Billing starts **Oct 31, 2026, at 16:00 UTC**."
> — [DeepSeek-V4.1-Flash-1017 announcement](https://api-docs.deepseek.com/news/news261017)

So: **direct standalone API calls are billed immediately** under the rules
above; only server-side calls made through Harness, Claude Code, or Codex are
waived until 2026-11-01 00:00 Beijing (2026-10-31 16:00 UTC). The Claude Code
page also notes that invoking web search costs extra model tokens to summarize
the retrieved content, on top of Search API billing.

## 6. Consequences for magpie (from the code)

- `searchHosts` lists `api.deepseek.com` under both Anthropic and Responses
  (`search.go:202-207`), and `searchesFor` is model-agnostic for DeepSeek
  (`search.go:144-155`; `searchModels` has no DeepSeek entry). A direct
  `deepseek-flash` Responses turn is therefore still treated as native: magpie
  sends `web_search`, DeepSeek ignores it, and the turn answers without a
  search. This is the silent-search gap the ticket anticipated.
- A direct Anthropic turn on `deepseek-flash` is safely pass-through: DeepSeek
  searches and the result blocks are relayed. Pro on Anthropic is unverified.
- When magpie uses DeepSeek as its *searcher* for another model, the silent
  flash turn is caught: `searchWith` reports `errNotSearched` when no
  `server_tool_use` / `web_search_tool_result` came back, or `errFoundNothing`
  when a search ran but named no page, and `modelSearch` moves on to the next
  searcher or search API (`search.go:844-851`; `docs/subsystems/gateway-routing.md:342-350`).
  The silent failure reaches the client only on a direct pass-through turn.
- Rendering needs only title, URL, and page age. There are no snippets to
  preserve, and citations are absent/unverified on the DeepSeek response.
- Any supplement with `/v1/tools/search` must map the standalone object into
  the client's protocol itself; it cannot rely on the server-side block shape.

## Not verified

- `deepseek-v4-pro` executing web search on the Anthropic endpoint.
- DeepSeek emitting Anthropic output citations (`web_search_result_location`)
  and their fields.
- DeepSeek's Responses `web_search_call` payload fields (`action.query`,
  `action.sources`, annotations) and result counts.
- Whether `max_uses` and domain filters are honoured on DeepSeek's Anthropic
  endpoint (not in its compatibility table).
- The text of a `deepseek-flash` Responses answer when `web_search` is ignored
  (docs say silent, no sample).
- Which model/protocol DeepSeek Harness uses for search.
- Which of Codex's prose or its `supports_search_tool` catalog field is correct.
