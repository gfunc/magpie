# MCP gateway

Status: spec, not yet implemented. This page fixes how gateway-mode magpie
serves MCP to agents on other machines — aggregation shape, stdio and SSE
bridging, centralized sign-in and edge auth, remote client wiring. It was
charted by the wayfinder effort in `wayfinder/mcp-gateway/`; each section
names the decision it rests on. Implementation is a separately-charted
effort.

Acceptance fleet: both Docker stacks plus the laptop agents (kimi-code
included).

## What exists today

- Per-server relay at `/mcp/{name}` (`mcpProxy`,
  [`internal/gateway/mcp.go`](../../internal/gateway/mcp.go)): a signed-in
  streamable-HTTP server is relayed with token renewal; LAN sharing plus an
  enabled gateway key guard it (`sharedWith`), `Origin`-carrying browser
  requests are refused, hop headers are stripped and the caller's key is
  substituted.
- magpie's own MCP servers: web search at `/mcp/magpie/web-search`
  (`SearchMCPPath`), vision at `/mcp/magpie/vision` (`VisionMCPPath`), plugin
  tools at `/mcp/magpie/tools` (`ToolsMCPPath`) — all served by `serveMCP`,
  each call held to the calling key's budget (`reserveFor`,
  `budget.Reserve`) and marked in Routing as `CallFor{Agent, MCP: true}`.
  `keyLimited` passes every `/mcp/` path by
  ([`key_limit.go`](../../internal/gateway/key_limit.go)).
- The Library writes each agent's native MCP config (`syncMCP`/`mcpFile`);
  `through()` ([`internal/library/mcp_signin.go`](../../internal/library/mcp_signin.go))
  rewrites a signed-in server to the relay URL with the LAN key for a
  cross-machine target. Stdio servers are pure config: each machine runs its
  own copy. The relay and `through()` take streamable HTTP only; SSE-only
  servers are refused.
- OAuth sign-in for MCP servers runs in `internal/mcpauth`; its callback
  listener binds the magpie host's `127.0.0.1`
  ([`mcpauth.go`](../../internal/mcpauth/mcpauth.go)). Headless, the vendor
  redirects the laptop's browser to the laptop's own loopback and the flow
  cannot complete (ticket 03).
- `CheckServers` ([`internal/library/mcp_check.go`](../../internal/library/mcp_check.go))
  is the codebase's stdio-MCP speaker: it spawns a command server in home
  with `PWD` set, runs the initialize → notifications/initialized →
  tools/list handshake under a step budget, and caches each server's
  `ToolList` (name + description per tool).
- Gateway mode (`internal/gui/gatewaymode.go`, automatic when `magpie web`
  runs with no agents on the box) hides the Agents and Library pages
  (`GATEWAY_HIDES`, `internal/gui/assets/app.js`), so a headless stack has
  no MCP config surface.

## Routes

One exact-match route joins the existing layout:

| Route | Serves |
| --- | --- |
| `/mcp` | The aggregated endpoint: every MCP server magpie holds — signed-in third-party HTTP, bridged stdio, bridged SSE, and magpie's own tools. |
| `/mcp/{name}` | One server, as today: relayed signed-in HTTP, or a bridged stdio/SSE server (new). |
| `/mcp/magpie/*` | magpie's own tools directly, as today; they also join the aggregate. |

The bare `/mcp` collides with no name (relay names are non-empty; `magpie`
stays reserved for the own-tools paths). One global aggregate only — no
named tool groups (ticket 01).

## Presentation contract

The aggregate presents in one of two modes (tickets 01, 07):

- **Meta-tools (default above the threshold).** Two tools:
  - `search_tools(query)` returns `{server, tool, description, inputSchema}`
    matches, ranked lexically over the `CheckServers` `ToolList` cache —
    tool-name exact, then prefix, then substring, then description. It is
    deterministic, free (no budget reservation), and private (the cache
    never leaves the box).
  - `call_tool(server, tool, arguments)` routes to the right surface —
    HTTP relay, stdio bridge, SSE bridge, or magpie's own tools —
    forwarding content blocks verbatim with `isError` preserved. An unknown
    server or tool answers as a tool error, the `serveMCP` pattern.
- **Eager (small sets).** A merged tools/list of every server's tools, each
  named `server__tool`.

The trigger is adaptive: total tools across the aggregate's servers at or
under a named threshold (default ~30) presents eager; above it, meta-tools.
A per-entry URL parameter (`?eager=1` / `?meta=1`) overrides the trigger
for that consumer. JSON-RPC batches inherit `serveMCP`'s handling.

This shape is bounded by the verifying client: kimi-code reads `mcp.json`
(`mcpServers`), takes streamable HTTP with static `headers` or
`bearerTokenEnvVar`, loads all tool definitions eagerly by default, and has
no protocol-level tool search — so the meta-tools reach it as two ordinary
tools and a merged-only surface would flood its context (ticket 02).

## Sessions

The aggregate and the bridged per-server routes issue `Mcp-Session-Id` at
`initialize`; the id keys the bridge's (session × server) mappings. A
session `DELETE` ends it and reaps what it spawned. Native HTTP relays keep
passing the vendor's own sessions through (ticket 07).

## Metering and attribution

`call_tool` reserves the calling key's budget for the call
(`budget.Reserve`, the `/mcp/magpie/web-search` precedent; `keyLimited`
already passes `/mcp/` by). `search_tools` stays free. Both meta-tools mark
Routing `CallFor{Agent, MCP: true}`, so MCP traffic stays attributable per
agent and per key (ticket 07).

## stdio bridging

magpie hosts command servers itself, converting stdio to streamable HTTP —
no sidecar, no per-machine copies (tickets 01, 05). Hosting happens only
where the server is configured (the stack); chained consumers forward.

- **Process model:** lazy, one process per (session × server). The bridge
  issues the session id, spawns nothing for a session that only browses,
  and spawns at the first actual call to that server. `search_tools` and an
  eager `tools/list` are served from the `CheckServers` cache.
- **Handshake:** the bridge owns initialize → notifications/initialized →
  request at spawn — the `CheckServers` pattern promoted from health probe
  to serving path.
- **Lifecycle:** a crash fails the in-flight call as a clear tool error;
  the next use respawns; repeated crashes trip a capped backoff and mark
  the server down in the Library's health row. An idle TTL (a named
  default, configurable) and session DELETE reap processes. Global and
  per-server process caps answer overflow with a clear error. Child stderr
  is captured into magpie's log, truncated.
- **Env and secrets:** today's mechanism, unchanged — values in
  `library.json` (kept out of no-keys backups), `${NAME}` refs expanded
  from magpie's own environment at spawn (the container's compose env on
  the stack), exactly as `CheckServers` expands them. No new secrets store.
- **Surface:** bridged servers appear on `/mcp/{name}` and the aggregate
  exactly like signed-in HTTP servers.

## SSE-only upstreams

SSE-only servers join the aggregate in the first cut (ticket 07, the user's
call against exclusion). One upstream SSE connection per (session ×
server), with a lifecycle uniform with the stdio bridge: opened at first
use, reaped on idle TTL or DELETE, failure → clear tool error plus restart
with backoff. The bridge absorbs the SSE `endpoint` event's POST address
and shuttles responses from the event stream back into HTTP responses. This
extends `mcpauth` past streamable HTTP and lifts `through()`'s HTTP-only
restriction.

## Centralized sign-in

Sign-in lives on the stack, once per server, in the Library UI. To complete
OAuth headless (ticket 03): a paste-back flow, mcporter `--no-browser`
style — the waiting sign-in box gains a "Callback URL" field posting to
`/api/library/mcp-signin/{id}/callback`; the pasted redirect is checked by
`state` and exchanged with the same `redirect_uri` (or replayed through the
in-process `/callback` handler, as `pastedCallback` does for provider
sign-ins in `internal/provider/signin.go`). It works on both compose stacks
with no network assumption beyond the `magpie web` tunnel and changes no
vendor-facing redirect URI. Tunnels and published ports are rejected: they
cannot reach a container-loopback listener under bridge networking.

## Remote client wiring

**Routing, not copying** (ticket 04, the user's line). Two supported paths:

1. **The chain (preferred).** A magpie-to-magpie upstream pointer: the
   consumer-side magpie (the laptop's) holds one pointer — the stack's base
   URL plus the existing gateway key. It writes a single aggregate entry
   pointing at **its own** `/mcp` into each local agent it's given to,
   riding the existing Library sync and each agent's native config format,
   and transparently forwards the whole `/mcp` surface (aggregate and
   per-server relays) to the stack, substituting its key. Server
   definitions, OAuth tokens and stdio hosting never leave the stack;
   importing server definitions is rejected. The consumer's own local tools
   stay local unless the chain's far end is their host. Attribution: the
   consumer's Routing attributes each call to the local agent before
   forwarding; the stack attributes everything to the pointer's key —
   agent-level at the consumer, key-level at the stack.
2. **Direct entries (supported second option).** Agents on magpie-less
   machines point straight at the stack: aggregate URL plus an
   `Authorization` header. A standardized export — "copy config for
   \<agent\>", extending `ConfigOfServers` to the remote-consumer form —
   emits the single aggregate entry in the agent's native format.

Edge auth is unchanged on both paths: LAN sharing on plus an enabled
gateway key (`sharedWith`). Per-consumer keys are out of the first cut.
Rotation cost: update the pointer (chain) or each direct entry.

## Gateway-mode config surface

The Library's MCP servers section is visible in gateway mode's web UI —
`GATEWAY_HIDES` stops hiding it — so a headless stack is configured
entirely from a laptop browser: add servers (HTTP, SSE, stdio command),
sign in (paste-back), check health, and copy the direct-export snippet
(ticket 01, decision 6).

## Acceptance criteria

All against both Docker stacks, with laptop agents as consumers:

1. In gateway mode, the web UI shows the Library's MCP servers section; a
   server (HTTP, and a stdio command) is added from the laptop browser.
2. An OAuth MCP sign-in completes headless via the paste-back flow on both
   stacks.
3. kimi-code on the laptop, chained through the laptop's magpie pointer,
   holds one aggregate entry in its `mcp.json`; `search_tools` finds a
   tool, `call_tool` runs it; the stack attributes the call to the
   pointer's key and the laptop's Routing attributes it to kimi-code.
4. A magpie-less consumer uses the exported direct entry (stack URL +
   `Authorization` header) against the aggregate.
5. A stdio server configured on the stack is called through the aggregate:
   no process until first use, one process per (session × server), idle-TTL
   reap, crash → clear tool error + backoff restart, down-marked in the
   health row.
6. An SSE-only server configured on the stack is called through the
   aggregate.
7. Aggregate calls reserve the calling key's budget and appear in Routing
   marked `CallFor{Agent, MCP: true}`.
8. Presentation flips at the tool-count threshold; `?eager=1` / `?meta=1`
   override it.

## Non-goals

- Named tool groups and per-consumer tool visibility — parked in the map's
  Not-yet-specified until a real need appears.
- Per-consumer gateway keys with separate limits.
- The implementation itself: fork patch, image build, stack deployment,
  verification — a separately-charted effort.
