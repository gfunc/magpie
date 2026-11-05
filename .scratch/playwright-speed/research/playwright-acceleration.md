# Accelerating the magpie Playwright suite (node:test + raw Playwright library)

Verified 2026-10-28 against nodejs.org (Node.js API and CLI docs, nodejs/node
source), playwright.dev (official docs), the microsoft/playwright repository
(docs sources, `playwright-core` server/client source) and the
microsoft/playwright issue tracker.

Everything below is a first-party claim with an inline link. Anything the
primary sources do not state is marked **not verified**. Claims about our own
suite are marked *(ours)* and carry no citation — they are counted from the
repo, not from the docs.

## Scope and how our suite maps onto the documented patterns

Our suite does **not** use `@playwright/test`. It runs 922 files under
`internal/gui/tests/*.test.cjs` with Node's built-in runner
(`node --test --test-concurrency=N`), importing the raw library
(`const { chromium, webkit } = require("playwright")`). Consequences that
matter for each question:

- Playwright's *test runner* concepts (workers, fixtures, `expect`, `--shard`)
  are **N/A because raw library** for anything we do not install; each
  Playwright concept is mapped below to its raw-library or node:test
  equivalent.
- Playwright's *library* concepts (browser contexts, `launchServer`,
  auto-waiting, routing, `page.clock`) apply **via X** exactly as documented —
  they are the same API the raw library exposes.

Counted in the repo *(ours)*: 922 `*.test.cjs` files; 918 of them contain a
`browser.launch(...)` call (1064 matching lines, 2086 call occurrences); 810
files call `browser.newContext(...)` (968 sites); 1446 `page.waitForTimeout(...)`
calls across 505 files; 808 files pin `channel: "chromium"`; 872 files install
a `page.route(...)`/`context.route(...)` handler that serves the app (the
sampled ones use `page.route("**/*", …)` and read assets from disk with
`fs.readFile`).

## Sources

Node.js (official docs and source)

- https://nodejs.org/api/test.html — test runner execution model, `run()` options, `workerId`
- https://nodejs.org/api/cli.html#--test-concurrency — `--test-concurrency` semantics and default
- https://nodejs.org/api/cli.html#--test-shard — `--test-shard` semantics
- https://github.com/nodejs/node/blob/main/doc/api/cli.md — CLI flag text (source of the two pages above)
- https://github.com/nodejs/node/blob/main/lib/internal/test_runner/utils.js — where the default is computed
- https://github.com/nodejs/node/blob/main/lib/internal/test_runner/runner.js — runner wiring

Playwright official docs (playwright.dev; the same text is in
[microsoft/playwright `docs/src`](https://github.com/microsoft/playwright/tree/main/docs/src))

- https://playwright.dev/docs/browser-contexts — test isolation, context cost
- https://playwright.dev/docs/test-parallel — worker processes, worker reuse
- https://playwright.dev/docs/test-sharding — `--shard`
- https://playwright.dev/docs/actionability — auto-waiting, actionability checks, retrying assertions
- https://playwright.dev/docs/test-assertions — web-first assertions and their polling model
- https://playwright.dev/docs/best-practices — parallelism and sharding guidance
- https://playwright.dev/docs/api/class-browser — `Browser.newContext`
- https://playwright.dev/docs/api/class-browsertype — `launchServer`, `connect`
- https://playwright.dev/docs/api/class-page — `page.route`, `page.waitForTimeout`, `page.waitForFunction`
- https://playwright.dev/docs/api/class-locator — `locator.waitFor`, `locator.waitForFunction`
- https://playwright.dev/docs/api/class-locatorassertions — locator assertions
- https://playwright.dev/docs/api/class-route — `route.fulfill` options
- https://playwright.dev/docs/api/class-browserserver — `BrowserServer.wsEndpoint`
- https://playwright.dev/docs/browsers — Chromium headless shell vs new headless, WebKit
- https://playwright.dev/docs/network — routing
- https://playwright.dev/docs/clock — `page.clock`
- https://playwright.dev/docs/docker — Playwright Server (`run-server`) and remote `connect`
- https://playwright.dev/docs/trace-viewer — trace recording cost note

Playwright source (microsoft/playwright)

- https://github.com/microsoft/playwright/blob/main/packages/playwright-core/src/server/chromium/crNetworkManager.ts — interception mechanics
- https://github.com/microsoft/playwright/blob/main/packages/playwright-core/src/client/browserContext.ts — client-side route dispatch
- https://github.com/microsoft/playwright/blob/main/packages/playwright/src/common/config.ts — worker default (`50%`)
- https://github.com/microsoft/playwright/blob/main/packages/playwright-core/index.mjs — library exports (no `expect`)

Playwright issue tracker (first-party tracker)

- https://github.com/microsoft/playwright/issues/35188 — WebKit vs Chromium slowness (maintainer statement)
- https://github.com/microsoft/playwright/issues/18119 — WebKit action/launch slowness reports
- https://github.com/microsoft/playwright/issues/33319 — WebKit vs Chrome discrepancy
- https://github.com/microsoft/playwright/issues/31481 — WebKit 2-3x slower report
- https://github.com/microsoft/playwright/issues/33566 — new headless vs headless shell behaviour

## 1. node:test parallelism

**Process model.** "When process-level test isolation is enabled, each matching
test file is executed in a separate child process. The maximum number of child
processes running at any time is controlled by the `--test-concurrency` flag.
If the child process finishes with an exit code of 0, the test is considered
passing." ([nodejs.org/api/test.html](https://nodejs.org/api/test.html), *Test
runner execution model*). Isolation is the default mode; `--test-isolation=none`
imports every file into the runner process and forces concurrency to one
([nodejs.org/api/cli.html](https://nodejs.org/api/cli.html#--test-concurrency),
[utils.js](https://github.com/nodejs/node/blob/main/lib/internal/test_runner/utils.js)).

**Semantics of the flag.** "The maximum number of test files that the test
runner CLI will execute concurrently. If `--test-isolation` is set to `'none'`,
this flag is ignored and concurrency is one. Otherwise, concurrency defaults to
`os.availableParallelism() - 1`."
([nodejs.org/api/cli.html#--test-concurrency](https://nodejs.org/api/cli.html#--test-concurrency)).
The source computes it the same way: `concurrency = getOptionValue('--test-concurrency') || true`
when isolation is on, `concurrency = 1` when it is `none`
([utils.js](https://github.com/nodejs/node/blob/main/lib/internal/test_runner/utils.js)).
The `run()` API mirrors the CLI: `concurrency` is "the number of test processes
… each process corresponds to one test file"; `true` means
`os.availableParallelism() - 1` ([nodejs.org/api/test.html](https://nodejs.org/api/test.html)).

**Ceiling relative to CPU count.** The only first-party guidance is the
default: `os.availableParallelism() - 1` — i.e. on this 16-CPU Linux box the
default is **15** ([nodejs.org/api/cli.html#--test-concurrency](https://nodejs.org/api/cli.html#--test-concurrency)).
Node ships no "safe maximum" rule; that judgement is ours. Playwright's own
runner, which does the same kind of work, defaults to **half the logical CPUs**
(`workers: … '50%'`, resolved as `Math.max(1, Math.floor(os.cpus().length * 0.5))`,
[config.ts](https://github.com/microsoft/playwright/blob/main/packages/playwright/src/common/config.ts)),
and documents that workers "strive for optimal utilization of CPU cores"
([playwright.dev/docs/test-sharding](https://playwright.dev/docs/test-sharding)).
For a browser suite each unit of concurrency costs one Node child process
*plus* one browser process, so Playwright's 50% heuristic (8 here) is the
better starting point than Node's 15; the machine in question is latency-bound
rather than CPU-bound, which argues for measuring 8 vs 12 vs 15 rather than
assuming.

**Is `browser.launch()` per file safe at high concurrency?** Yes for
correctness: each file is its own child process
([nodejs.org/api/test.html](https://nodejs.org/api/test.html)), so files cannot
interfere through globals or module state, and a browser launched in one file
is closed with that file's process. The constraint is resource, not
correctness: the number of live browsers equals the number of live child
processes, so concurrency must be chosen against RAM (16 GB available here)
and against the browser's own startup cost.

**Also relevant.** Node's runner assigns each file a worker id
(`NODE_TEST_WORKER_ID`, 1..N) and documents it as the way to "split resources
(like database connections or server ports) across concurrent test files"
([nodejs.org/api/test.html](https://nodejs.org/api/test.html)) — useful if we
move to a shared HTTP server (§5) whose port must not collide.

## 2. Browser launch vs context cost

**Contexts are the documented isolation unit and are cheap.** "Playwright
achieves this using BrowserContext's which are equivalent to incognito-like
profiles. **They are fast and cheap to create and are completely isolated,
even when running in a single browser.**"
([playwright.dev/docs/browser-contexts](https://playwright.dev/docs/browser-contexts)).
"Creates a new browser context. It won't share cookies/cache with other browser
contexts." ([playwright.dev/docs/api/class-browser](https://playwright.dev/docs/api/class-browser)).

**The documented pattern is one browser, one context per test.** "Running the
test creates a new browser context each time. When using Playwright as a Test
Runner, browser contexts are created by default. Otherwise, you can create
browser contexts manually."
([playwright.dev/docs/browser-contexts](https://playwright.dev/docs/browser-contexts)).
The runner's own worker model matches: "All workers have identical environments
and each starts its own browser… Playwright Test reuses a single worker as much
as it can to make testing faster, so multiple test files are usually run in a
single worker one after another."
([playwright.dev/docs/test-parallel](https://playwright.dev/docs/test-parallel)),
and "Playwright runs tests in separate worker processes, each with its own
isolated BrowserContext"
([playwright.dev/docs/test-parallel](https://playwright.dev/docs/test-parallel)).

**Applies to us via X:** a node:test child process is the exact analogue of a
Playwright worker. The Playwright pattern "one browser per worker, one context
per test" translates to "one `browser.launch()` per test file, one
`browser.newContext()` per matrix leaf". Our files currently launch inside the
matrix loop, i.e. one browser per scenario — the pattern the docs describe as
unnecessary, since the docs' own runner launches one browser per worker and
reuses it across every test in that worker.

**What is *not* verified:** no first-party source states a millisecond cost for
`browser.launch()` versus `browser.newContext()`. The docs' qualitative claim
("fast and cheap to create") is the only first-party statement, and it is about
contexts.

## 3. Shared browser server (`launchServer` + `connect`)

**API.** `BrowserType.launchServer` "Returns the browser app instance. You can
connect to it via `BrowserType.connect`, which requires the major/minor client/
server version to match"; the documented usage is
`const browserServer = await chromium.launchServer(); const wsEndpoint =
browserServer.wsEndpoint(); const browser = await chromium.connect(wsEndpoint);`
([playwright.dev/docs/api/class-browsertype](https://playwright.dev/docs/api/class-browsertype)).
`BrowserServer.wsEndpoint` is the "Browser websocket endpoint which can be used
as an argument to `BrowserType.connect`"
([playwright.dev/docs/api/class-browserserver](https://playwright.dev/docs/api/class-browserserver)).
`connect` "attaches Playwright to an existing browser instance created via
`BrowserType.launchServer` in Node.js", and the versions must match
([playwright.dev/docs/api/class-browsertype](https://playwright.dev/docs/api/class-browsertype)).

**Official recommendation context.** The documented use of this pattern is
running the browser on another machine: "You can run Playwright Server in
Docker while keeping your tests running on the host system or another machine.
This is useful for running tests on unsupported Linux distributions or remote
execution scenarios", started with
`npx playwright run-server --port 3000 --host 0.0.0.0` and connected with
`await playwright['chromium'].connect('ws://127.0.0.1:3000/')`
([playwright.dev/docs/docker](https://playwright.dev/docs/docker)).

**Limits.** `launchServer` is a method *on a browser type*, so one server
instance is per engine — a chromium server and a webkit server are two
servers ([playwright.dev/docs/api/class-browsertype](https://playwright.dev/docs/api/class-browsertype)).
The version-match constraint is explicit (1.2.3 is compatible with 1.2.x)
(same page). The server listens on loopback by default and `host` "exposes the
browser RPC to anything that can reach the listening port" if widened
(same page). Context isolation still applies to a connected browser: contexts
remain incognito-like and "completely isolated, even when running in a single
browser"
([playwright.dev/docs/browser-contexts](https://playwright.dev/docs/browser-contexts)).

**Applies to us via X** the raw library (`chromium.launchServer()` /
`chromium.connect(ws)` exist in the library, not only in the runner). Note the
practical caveat for our topology: with node:test, each file is a separate
child process, so a shared server would have to be started by the parent and
its `wsEndpoint` handed to children — node:test inherits environment variables
into children
([nodejs.org/api/test.html](https://nodejs.org/api/test.html), *Child process
option inheritance*), so `PW_WS=…` in the environment is the mechanism.
**Not verified:** no first-party source states a measured saving from
`launchServer` + `connect` versus `launch()` per file, nor that it is
recommended for speed (it is documented for remote execution).

## 4. Auto-waiting instead of fixed sleeps

**The docs call fixed waits out.** `Page.waitForTimeout` is annotated
"*discouraged: Never wait for timeout in production. Tests that wait for time
are inherently flaky. Use Locator actions and web assertions that wait
automatically*", and "Note that `page.waitForTimeout()` should only be used for
debugging."
([playwright.dev/docs/api/class-page](https://playwright.dev/docs/api/class-page)).

**Auto-waiting is the model to copy.** "Playwright performs a range of
actionability checks on the elements before making actions… It auto-waits for
all the relevant checks to pass and only then performs the requested action. If
the required checks do not pass within the given `timeout`, action fails with
the `TimeoutError`." The checked conditions are Visible, Stable, Receives
Events, Enabled, Not Inert (per action)
([playwright.dev/docs/actionability](https://playwright.dev/docs/actionability)).

**Web-first assertions poll.** "Playwright also includes auto-retrying
assertions that remove flakiness by waiting until the condition is met…
Playwright will be re-testing the element with the test id of `status` until the
fetched element has the `"Submitted"` text. It will re-fetch the element and
check it over and over, until the condition is met or until the timeout is
reached." Default assertion timeout is 5 seconds
([playwright.dev/docs/test-assertions](https://playwright.dev/docs/test-assertions)).

**Library-level equivalent (we use the raw library, so `expect` is not
available).** The `playwright` package re-exports `chromium`, `firefox`,
`webkit`, `selectors`, `devices`, `errors`, `request` — and **not** `expect`
([playwright-core/index.mjs](https://github.com/microsoft/playwright/blob/main/packages/playwright-core/index.mjs));
`expect` and the web-first matchers live in `@playwright/test`
([playwright.dev/docs/test-assertions](https://playwright.dev/docs/test-assertions)).
The raw-library polling primitives are:

- `locator.waitFor({ state })` — "Returns when element specified by locator
  satisfies the `state` option. If target element already satisfies the
  condition, the method returns immediately. Otherwise, waits for up to
  `timeout` milliseconds until the condition is met."
  ([playwright.dev/docs/api/class-locator](https://playwright.dev/docs/api/class-locator))
- `locator.waitForFunction(expression)` — "The locator is re-resolved on each
  retry, so it tolerates the element being re-rendered while waiting."
  ([playwright.dev/docs/api/class-locator](https://playwright.dev/docs/api/class-locator))
- `page.waitForFunction(expression, arg, { polling, timeout })` — "Returns when
  the `expression` returns a truthy value"
  ([playwright.dev/docs/api/class-page](https://playwright.dev/docs/api/class-page));
  `polling` accepts a number of milliseconds or `'raf'`
  ([playwright.dev/docs/api/class-page](https://playwright.dev/docs/api/class-page)).

Our suite already uses `page.waitForFunction` in places (e.g.
`internal/gui/tests/menu-scroll.test.cjs` waits for the dropdown's
`scrollTop > 0`), which is the pattern to generalise.

**Optional extra lever: the clock.** `page.clock` "overrides native global
classes and functions related to time" (`Date`, `setTimeout`, `setInterval`,
`requestAnimationFrame`, `performance`, …) and offers `install`, `fastForward`,
`runFor`, `pauseAt`, `resume`, `setFixedTime`
([playwright.dev/docs/clock](https://playwright.dev/docs/clock)). For the
double-redraw cases (Settings draws twice, so the test waits 1000 ms twice) a
clock-driven wait is the deterministic replacement — **applies via X** the raw
library (`page.clock` is on `Page`, not on the runner).

**Trade-off to weigh:** polling waits are not free — they cost wall-clock only
until the condition holds, and they fail with a timeout instead of hanging, but
a badly chosen poll interval can burn as much time as the sleep it replaced.
The docs give no polling-interval guidance — **not verified**.

## 5. Route interception overhead

**What the docs guarantee.** "Once routing is enabled, every request matching
the url pattern will stall unless it's continued, fulfilled or aborted."
([playwright.dev/docs/api/class-page](https://playwright.dev/docs/api/class-page)).
So a `page.route("**/*", …)` handler is not a passive observer: every asset
request is paused in the browser until the handler answers.

**What the source shows (mechanism, first-party code).**

- Enabling interception turns on CDP `Fetch` interception for *all* URLs at the
  request stage — `info.session.send('Fetch.enable', { handleAuthRequests: true,
  patterns: [{ urlPattern: '*', requestStage: 'Request' }] })`
  ([crNetworkManager.ts](https://github.com/microsoft/playwright/blob/main/packages/playwright-core/src/server/chromium/crNetworkManager.ts)).
- While interception is on, the browser's HTTP cache is switched off —
  `info.session.send('Network.setCacheDisabled', { cacheDisabled: enabled })`
  in the same method
  ([crNetworkManager.ts](https://github.com/microsoft/playwright/blob/main/packages/playwright-core/src/server/chromium/crNetworkManager.ts)).
  That means every repeated asset fetch is re-fetched, never served from cache.
- Each paused request travels to the Node client and back: the client
  subscribes with `this._channel.on('route', ({ route }) => this._onRoute(...))`
  ([client/browserContext.ts](https://github.com/microsoft/playwright/blob/main/packages/playwright-core/src/client/browserContext.ts)),
  the user's handler runs there, and the answer returns over the same channel
  before the server issues `Fetch.continueRequest` / `Fetch.fulfillRequest`
  ([crNetworkManager.ts](https://github.com/microsoft/playwright/blob/main/packages/playwright-core/src/server/chromium/crNetworkManager.ts)).
  That is a protocol round trip **per request**, on top of the file read.
- `route.fulfill({ body })` sends the bytes over the protocol, and
  `route.fulfill({ path })` means "File path to respond with" — Playwright
  reads it in Node and ships it across
  ([playwright.dev/docs/api/class-route](https://playwright.dev/docs/api/class-route)).
  Either way the asset bytes are copied through the Node process; a real
  HTTP server lets the browser pull them from a socket directly.

**Is there a first-party statement recommending a real server for large asset
sets?** No — **not verified**. No document in playwright.dev says "use a local
HTTP server instead of `page.route` for static assets". The recommendation in
§"Recommendations" below is therefore derived from the documented and source
facts above (per-request stall, cache disabled, protocol round trip per
request), not from a first-party recommendation.

**Also note (first-party, applies to us):** `page.route` "will not intercept
requests intercepted by Service Worker", and Playwright recommends
`serviceWorkers: 'block'` when using request interception; `page.route` also
"will not intercept the first request of a popup page" — use
`BrowserContext.route` there
([playwright.dev/docs/api/class-page](https://playwright.dev/docs/api/class-page)).

## 6. WebKit specifics

**Maintainer statement (first-party).** "Playwright does not do anything
specific on WebKit that would make it slow. The protocol is different and so is
implementation to some extent, but I can't think of something that would just
use cpu if you are doing nothing… However, **WebKit build in general is slower
than Chromium**, so unless it is a recent regression, chances to fix it are
very small." — dgozman (Playwright maintainer),
[microsoft/playwright#35188](https://github.com/microsoft/playwright/issues/35188).

**Reports in the first-party tracker (user reports, not maintainer
statements).** WebKit runs the same tests "considerably longer than on Chrome
across the board… up to twice as long"
([#33319](https://github.com/microsoft/playwright/issues/33319)); "The same set
of tests takes round about 2-3x in webkit than running in chrome"
([#31481](https://github.com/microsoft/playwright/issues/31481)); a 20-test
suite taking 3 minutes in Chromium and 11 minutes in WebKit
([#18119](https://github.com/microsoft/playwright/issues/18119)).

**Mitigations reported in the first-party tracker (user reports).**

- Tracing and video recording inflate WebKit runtimes disproportionately:
  switching `video`/`trace` from `retain-on-failure` to `on-first-retry` cut
  one reporter's WebKit time from 7.5 min to 4.3 min and Chromium from 3.9 min
  to 2.3 min ([#18119](https://github.com/microsoft/playwright/issues/18119)).
  First-party corroboration for the trace side: `trace: 'on'` is
  "not recommended as it's performance heavy"
  ([playwright.dev/docs/trace-viewer](https://playwright.dev/docs/trace-viewer)).
- `deviceScaleFactor: 1` instead of the default 2 reportedly cut WebKit times
  ~1.8x for one reporter ([#18119](https://github.com/microsoft/playwright/issues/18119)).
- Per-project worker limits (`testProject.workers`, first-party API) were used
  to cap WebKit concurrency ([#18119](https://github.com/microsoft/playwright/issues/18119)).
- A launch-time datapoint from the same thread: WebKit `browserType.launch`
  ≈1.7 s versus ≈61 ms for the whole Chromium `beforeEach` hook
  ([#18119](https://github.com/microsoft/playwright/issues/18119)) — a user
  measurement, not a Playwright benchmark.

**What WebKit is.** "Playwright's WebKit is derived from the latest WebKit main
branch sources… Playwright doesn't work with the branded version of Safari
since it relies on patches."
([playwright.dev/docs/browsers](https://playwright.dev/docs/browsers)).

**No first-party per-engine mitigation list exists** — **not verified**. The
maintainer statement above says the difference is inherent, so the levers are
ours: fewer WebKit launches (§2), fewer waits (§4), and capping WebKit
concurrency separately from Chromium.

## 7. Sharding

**@playwright/test's sharding.** `npx playwright test --shard=x/y` splits the
suite into shards that "can run independently… each shard can run on its own,
utilizing the available CPU cores"; "Playwright can only shard tests that can
be run in parallel. By default, this means Playwright will shard test files."
([playwright.dev/docs/test-sharding](https://playwright.dev/docs/test-sharding)).
Granularity depends on `fullyParallel`: with it, "Playwright Test runs
individual tests in parallel across multiple shards, ensuring each shard
receives an even distribution of tests"; without it, "entire test files are
assigned to shards… If your test files are not evenly sized… certain shards may
end up running significantly more tests"
([playwright.dev/docs/test-sharding](https://playwright.dev/docs/test-sharding)).
`--shard` is **N/A because raw library** — it is a `@playwright/test` CLI flag.

**The node:test equivalent already exists in our runner.** "Test suite shard to
execute in a format of `<index>/<total>`… This command will divide all tests
files into `total` equal parts, and will run only those that happen to be in
an `index` part."
([nodejs.org/api/cli.html#--test-shard](https://nodejs.org/api/cli.html#--test-shard)).
The programmatic form is `run({ shard: { index, total } })`, documented as
"used to horizontally parallelize test running across machines or processes"
([nodejs.org/api/test.html](https://nodejs.org/api/test.html)). This applies
**via X** node:test directly — no Playwright dependency needed.

**Applicability to our suite.** Sharding by file list across machines is
exactly `node --test --test-shard=1/4` … `--shard=4/4` over
`internal/gui/tests/*.test.cjs`. Caveat from Playwright's own sharding
guidance: file-level sharding balances badly when files differ in size
([playwright.dev/docs/test-sharding](https://playwright.dev/docs/test-sharding)),
and ours differ enormously (a heavy file's scenario costs 10–13 s
*(ours)*). Node's sharding is "equal parts" of the file list — the same
imbalance applies. Mitigations: shard by file count but keep heavy files
balanced, or split the heaviest files, or shard per (file, engine) pair.

**Merging reports** is a Playwright-runner feature (blob reporter +
`merge-reports`) and is **N/A because raw library**; node:test has its own
reporters and CI can aggregate them per shard.

## 8. Headless shell vs `channel: "chromium"`

**What the docs say.** "Playwright ships a regular Chromium build for headed
operations and a separate chromium headless shell for headless mode. If you
are only running tests in headless shell (i.e. the `channel` option is **not**
specified)…" ([playwright.dev/docs/browsers](https://playwright.dev/docs/browsers)).
"You can opt into the new headless mode by using `'chromium'` channel… New
Headless on the other hand is the real Chrome browser, and is thus more
authentic, reliable, and offers more features"
([playwright.dev/docs/browsers](https://playwright.dev/docs/browsers), quoting
the Chrome team's post). The `channel` option itself: "Use 'chromium' to opt in
to new headless mode"
([playwright.dev/docs/api/class-browsertype](https://playwright.dev/docs/api/class-browsertype)).

**Any performance difference? Not verified.** No first-party source states that
headless shell starts faster or runs faster than the new headless mode. The
docs only describe behavioural differences: "Google Chrome and Microsoft Edge
have switched to a new headless mode… This differs from chromium headless
shell that is used in Playwright by default when running headless, so expect
different behaviour in some cases" ([playwright.dev/docs/browsers](https://playwright.dev/docs/browsers),
see also [#33566](https://github.com/microsoft/playwright/issues/33566)).

**Implication for us.** 808 of our files already use `channel: "chromium"`
*(ours)*, i.e. they run the full Chromium build in new headless mode, not the
headless shell. Nothing in the docs justifies switching, and switching would
change rendering behaviour that those files currently assert against. Treat
"channel: chromium" as a behaviour choice, not a performance lever; the only
documented consequence is the installation size (`--only-shell` vs
`--no-shell`, [playwright.dev/docs/browsers](https://playwright.dev/docs/browsers)).

## Recommendations for this suite

Baseline *(ours)*: a serial (`--test-concurrency=1`) run of 69 files takes
74 minutes — about **64 s per file**, which extrapolates to roughly **16 hours**
for the 922-file suite. The machine (16 CPUs, 16 GB free) sits near idle during
runs, so the suite is latency-bound, not CPU-bound. Ranked by expected
wall-clock impact:

1. **Run the suite concurrently instead of serially.** node:test already runs
   one file per child process with `--test-concurrency` bounding live children
   ([nodejs.org/api/test.html](https://nodejs.org/api/test.html)); the default
   is `os.availableParallelism() - 1` = 15 here
   ([nodejs.org/api/cli.html#--test-concurrency](https://nodejs.org/api/cli.html#--test-concurrency)).
   This is by far the largest lever: ~16.5 h → ~1.1 h at 15-way, ~2.1 h at
   Playwright's own 50%-of-CPUs default of 8 workers
   ([config.ts](https://github.com/microsoft/playwright/blob/main/packages/playwright/src/common/config.ts)).
   Start at 8 (Playwright's own heuristic for browser-heavy work), then try 12
   and 15 and watch RAM and flake rate; the machine being latency-bound means
   the curve may stay flat well past 8. Cap Chromium and WebKit differently if
   WebKit becomes the memory or CPU bottleneck (§6).
2. **Launch one browser per file, create a context per scenario.** Today ~918
   files launch inside the matrix loop, so the ~10k browser scenarios each pay
   a process launch. Playwright's documented model is one browser per worker
   and one context per test — contexts are "fast and cheap to create and are
   completely isolated, even when running in a single browser"
   ([playwright.dev/docs/browser-contexts](https://playwright.dev/docs/browser-contexts);
   [playwright.dev/docs/test-parallel](https://playwright.dev/docs/test-parallel)).
   **Applies via X** the raw library: hoist `browser.launch()` to file scope
   (or a `before`/`after` pair), keep `browser.newContext()` inside the leaf.
   Expect the largest single saving after concurrency, especially for WebKit
   (a reported WebKit `launch` of ~1.7 s vs ~61 ms for a whole Chromium
   `beforeEach` — user measurement in
   [#18119](https://github.com/microsoft/playwright/issues/18119)).
3. **Replace the 1446 fixed sleeps with polling waits.** `page.waitForTimeout`
   is documented as `discouraged` and "should only be used for debugging"
   ([playwright.dev/docs/api/class-page](https://playwright.dev/docs/api/class-page)).
   Replace with `locator.waitFor({ state })`, `locator.waitForFunction`, or
   `page.waitForFunction(..., { polling: 'raf' })`
   ([playwright.dev/docs/api/class-locator](https://playwright.dev/docs/api/class-locator),
   [playwright.dev/docs/api/class-page](https://playwright.dev/docs/api/class-page));
   for the double-redraw cases (Settings drawing twice → 1000 ms × 2) use
   `page.clock` to make time deterministic
   ([playwright.dev/docs/clock](https://playwright.dev/docs/clock)). *Ours:* at
   an average 0.3–1.0 s per sleep, 1446 sleeps are roughly **7–24 minutes of
   the 74-minute serial run** — this cost is per-scenario, so it does not
   shrink with concurrency; removing it is a proportional win on top of §1 and
   it also removes the flake source the sleeps are papering over.
4. **Stop intercepting static assets with `page.route("**/*", …)`; serve them
   from a real local HTTP server and intercept only `/api/*`.** With routing
   on, every request stalls until the handler answers, CDP `Fetch` interception
   is enabled for `*` at the request stage, and the browser HTTP cache is
   switched off
   ([playwright.dev/docs/api/class-page](https://playwright.dev/docs/api/class-page);
   [crNetworkManager.ts](https://github.com/microsoft/playwright/blob/main/packages/playwright-core/src/server/chromium/crNetworkManager.ts));
   each asset fetch additionally crosses the Playwright protocol twice
   ([client/browserContext.ts](https://github.com/microsoft/playwright/blob/main/packages/playwright-core/src/client/browserContext.ts)).
   A local HTTP server (one per worker, port from the node worker id —
   [nodejs.org/api/test.html](https://nodejs.org/api/test.html)) restores the
   cache and removes a round trip per asset. Keep `page.route` for the JSON
   `/api/*` fixtures, where interception is the point. **Not verified** as a
   first-party recommendation — this one follows from the mechanics above.
5. **Shard across machines with node's built-in `--test-shard=<i>/<n>`.**
   `node --test --test-shard=1/4 … --shard=4/4` divides the file list into
   equal parts ([nodejs.org/api/cli.html#--test-shard](https://nodejs.org/api/cli.html#--test-shard));
   the `run({ shard })` API is documented for "horizontally parallelize test
   running across machines"
   ([nodejs.org/api/test.html](https://nodejs.org/api/test.html)). Playwright's
   `--shard` is **N/A because raw library**, and its own guidance applies
   anyway: file-level sharding is unbalanced when files differ in size
   ([playwright.dev/docs/test-sharding](https://playwright.dev/docs/test-sharding)),
   so balance the heaviest files (10–13 s per scenario *(ours)*) by splitting
   them or by sharding per (file, engine).

Secondary, lower-impact but cheap:

- Keep tracing/video off or at `on-first-retry`; trace `'on'` is "performance
  heavy" by the docs' own note
  ([playwright.dev/docs/trace-viewer](https://playwright.dev/docs/trace-viewer))
  and was the largest single WebKit slowdown in the tracker reports
  ([#18119](https://github.com/microsoft/playwright/issues/18119)).
- `deviceScaleFactor: 1` for WebKit where the suite does not assert on retina
  rendering — reported ~1.8x WebKit speed-up by one user
  ([#18119](https://github.com/microsoft/playwright/issues/18119)); this is a
  user report, not a Playwright statement.
- Consider `chromium.launchServer()` + `chromium.connect(ws)` shared across
  the node child processes (§3) if per-file launches remain significant; the
  API exists in the raw library
  ([playwright.dev/docs/api/class-browsertype](https://playwright.dev/docs/api/class-browsertype)),
  but the documented use case is remote execution
  ([playwright.dev/docs/docker](https://playwright.dev/docs/docker)), not speed,
  and no first-party source claims a speed win — measure before adopting.
- `channel: "chromium"` (already used by 808 files *(ours)*) buys new-headless
  fidelity, not speed; no first-party performance claim exists for either
  headless mode ([playwright.dev/docs/browsers](https://playwright.dev/docs/browsers)).
  Do not switch on performance grounds.

## Measured on this machine (2026-10-28, chromium, 8 files)

Same 8 files at three concurrency levels, 16 CPUs, machine otherwise idle
*(ours)*:

| concurrency | wall | tests | pass/fail |
| --- | --- | --- | --- |
| 1 | 211 s | 64 | 60/4 |
| 4 | 98 s | 64 | 59/5 |
| 8 | 96 s | 64 | 59/5 |

- Scaling is real but capped by file count here: at 8 files, concurrency 8
  runs one file per process, so the 96 s floor **is the slowest file's wall
  time** — `usage-ledger`'s "Requests" test alone burns 78–95 s in timeouts
  across all three levels.
- The failures are the pre-existing ones from the 2026-10-28 morning triage
  (usage-ledger Requests, library-mcp-tools ja 420, routing-rule-drag, and
  the two live-route cases); none is new flake from parallelism — the drag
  case merely rotates which locale/width variant times out.
- Extrapolation for the full 922-file suite: concurrency 1 ≈ 16 h,
  concurrency 8 ≈ 2 h, and the true constraint past that point is the
  longest single file, arguing for splitting the 40 s+ files or sharding
  per (file, engine) as §7 suggests.

## Open questions for the owner of the suite

- The exact per-file and per-scenario wall-clock split (launch vs page load vs
  waits vs actions) is not measured in this repo. A single instrumented run
  with `page.clock`/trace timings would tell whether §2 or §3 dominates after
  §1 lands.
- Whether the suite's assertions depend on the current new-headless Chromium
  rendering (808 files use `channel: "chromium"`) is a compatibility question
  only the suite's own history can answer; the docs give no performance
  reason to change it either way.
