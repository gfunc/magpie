// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings › Network and sharing › Model registry (registry-settings.md):
// the registry /api.json serves clients like kimi-code imported on
// /provider. The section draws whole from a fresh state — switch off,
// effort "Default (model's middle)", all four protocol buckets ticked,
// sync off — with the registry URL greyed and the /provider hint while
// off, and a Copy button while on (in a browser on the page, the page's
// own origin, where /api.json is served too). Toggling the switch or sync
// posts the flat key with the page's keep keys; the effort pick menu
// lists Default plus the five levels and posts the choice; a bucket's
// tick posts the reduced list, and taking the last one away posts an
// empty list, which the save refuses: the row says why in the page's
// registry-settings.md, criterion 7b: a copy clicked really copies. No
// native select or checkbox, no coloured left border; Chromium and WebKit,
// no backend — the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, reach, pickItem } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const BUCKETS = ["magpie-openai", "magpie-anthropic", "magpie-openai-responses", "magpie-group"];
const EFFORTS = ["low", "medium", "high", "xhigh", "max"];

function settingsPayload(over) {
  return {
    theme: "light", lang: over?.lang || "en", tray: "panel", currency: "usd",
    dock: false, dockWindow: false, proxy: "", redact: false, redactWords: [],
    codexWarmup: "", noStats: false, trayUsage: "", vision: "", imageGen: "",
    version: "0.1.900", dir: "/config/magpie", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [],
    lan: false, lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
    registry: { buckets: BUCKETS, efforts: EFFORTS },
    registryOn: false, registryEffort: "", registryBuckets: BUCKETS, registrySync: false,
    ...over,
  };
}

// server fakes the settings API. A save that sends every bucket off
// (registryBuckets: []) is answered with the save's refusal code, as
// internal/gui's fail() maps settings.ErrRegistryBucketsOff.
function server(over, posts, { webMode = false, refuseEmptyBuckets = true, lang = "en" } = {}) {
  let s = settingsPayload(over);
  return async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data, status = 200) => route.fulfill({ status, json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:${webMode}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang: s.lang, theme: "light" }, fx: s.fx });
    if (url.pathname === "/api/settings" && req.method() === "POST") {
      const body = JSON.parse(req.postData());
      posts.push(body);
      if (refuseEmptyBuckets && Array.isArray(body.registryBuckets) && body.registryBuckets.length === 0) {
        return json({ error: "the registry carries at least one protocol bucket: leave one checked", code: "registry_buckets_off" }, 400);
      }
      s = { ...s, ...Object.fromEntries(Object.entries(body).filter(([k]) => !["registry"].includes(k))) };
      return json(s);
    }
    if (url.pathname === "/api/settings") return json(s);
    if (url.pathname === "/api/copy") return route.fulfill({ status: 501, body: "" }); // refused: the copy proves the page's own path
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// the section's words in each language the page speaks, from i18n.js's own
// tables so the test can't drift from them
const W = {
  en: {
    serve: "Serve the registry",
    url: "Registry URL",
    hint: "Add it in kimi-code with /provider, as a custom OpenAI-compatible registry",
    effort: "Default thinking effort",
    lead: "Default (model’s middle)",
    leadEnv: "(model’s middle; MAGPIE_REGISTRY_DEFAULT_EFFORT=high applies)",
    buckets: "Protocol buckets",
    refused: "Leave at least one protocol bucket checked: the registry carries models to a client only on a wire it speaks",
    bucketsSub: "Which wires the registry carries models on, named for the client that speaks each; at least one stays checked",
    sync: "Sync across machines",
    syncSub: "The registry’s config — on or off, the effort, the protocol buckets — rides sync, while this switch stays on each computer’s own: the config lands only where the switch is on, so the coupling needs every synced box’s switch on",
    copied: "Copied Registry URL",
  },
  zh: {
    serve: "提供模型注册表",
    url: "注册表 URL",
    hint: "在 kimi-code 里用 /provider 以自定义 OpenAI 兼容注册表的形式添加",
    effort: "默认思考力度",
    lead: "默认（模型的中间档）",
    leadEnv: "（模型的中间档；MAGPIE_REGISTRY_DEFAULT_EFFORT=high 生效中）",
    buckets: "协议桶",
    refused: "至少勾选一个协议桶：注册表只在它说的协议线上向客户端承载模型",
    bucketsSub: "注册表以哪些协议线承载模型，各以说出该协议的客户端命名；至少保留一个",
    sync: "跨机器同步",
    syncSub: "注册表的配置——开关、思考力度、协议桶——随同步走，而这个开关留在每台电脑自己手上：配置只落在开关打开的机器上，所以联动需要每台同步的机器都打开此开关",
    copied: "已复制注册表 URL",
  },
  "zh-TW": {
    serve: "提供模型登錄檔",
    url: "登錄檔 URL",
    hint: "在 kimi-code 裡用 /provider 以自訂 OpenAI 相容登錄檔的形式新增",
    effort: "預設思考力度",
    lead: "預設（模型的中間檔）",
    leadEnv: "（模型的中間檔；MAGPIE_REGISTRY_DEFAULT_EFFORT=high 生效中）",
    buckets: "協定桶",
    refused: "至少勾選一個協定桶：登錄檔只在它說的協定線上向用戶端承載模型",
    bucketsSub: "登錄檔以哪些協定線承載模型，各以說出該協定的用戶端命名；至少保留一個",
    sync: "跨機器同步",
    syncSub: "登錄檔的設定——開關、思考力度、協定桶——隨同步走，而這個開關留在每台電腦自己手上：設定只落在開關開啟的機器上，所以聯動需要每台同步的機器都開啟此開關",
    copied: "已複製登錄檔 URL",
  },
  ja: {
    serve: "レジストリを提供する",
    url: "レジストリ URL",
    hint: "kimi-code の /provider で、カスタムの OpenAI 互換レジストリとして追加します",
    effort: "既定の思考レベル",
    lead: "既定（モデルの中間）",
    leadEnv: "（モデルの中間。MAGPIE_REGISTRY_DEFAULT_EFFORT=high が適用されます）",
    buckets: "プロトコルバケット",
    refused: "プロトコルバケットは最低 1 つオンにしてください。レジストリは、話せる線の上でしかクライアントにモデルを運びません",
    bucketsSub: "レジストリがどのプロトコル線でモデルを運ぶか。それぞれを話すクライアントの名で呼ばれます。最低 1 つはオンのままにします",
    sync: "マシン間で同期",
    syncSub: "レジストリの設定——オン・オフ、思考レベル、プロトコルバケット——は同期に乗りますが、このスイッチ自体は各コンピューターのものです。設定はスイッチがオンのマシンにしか届かないため、連動には同期するすべてのマシンでこのスイッチをオンにする必要があります",
    copied: "レジストリ URL をコピーしました",
  },
  de: {
    serve: "Registry anbieten",
    url: "Registry-URL",
    hint: "In kimi-code mit /provider als eigene OpenAI-kompatible Registry hinzufügen",
    effort: "Standard-Denkstufe",
    lead: "Standard (Mitte des Modells)",
    leadEnv: "(Mitte des Modells; MAGPIE_REGISTRY_DEFAULT_EFFORT=high greift)",
    buckets: "Protokoll-Buckets",
    refused: "Lasse mindestens einen Protokoll-Bucket angehakt: Die Registry trägt Modelle zu einem Client nur auf einem Draht, den er spricht",
    bucketsSub: "Auf welchen Drähten die Registry Modelle trägt, benannt nach dem Client, der jedes spricht; mindestens eines bleibt angehakt",
    sync: "Über Rechner hinweg synchronisieren",
    syncSub: "Die Konfiguration der Registry — an oder aus, die Denkstufe, die Protokoll-Buckets — reitet auf dem Sync, während dieser Schalter jedem Rechner selbst bleibt: Die Konfiguration landet nur, wo der Schalter an ist, deshalb braucht die Kopplung den Schalter von jeder synchronisierten Box an",
    copied: "Registry-URL kopiert",
  },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the Model registry section draws, saves, and says its refusals in the row", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const pages = [];
    t.after(async () => {
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        for (const [i, p] of pages.entries()) await p.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-registry-${i}.png`) });
      }
      await browser.close();
    });
    const open = async (over, opts) => {
      const page = await (await browser.newContext({ viewport: { width: 900, height: 420 }, reducedMotion: "reduce" })).newPage();
      pages.push(page);
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], posts = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(over, posts, opts));
      await watchSettingsReads(page);
      await page.goto("http://localhost:3430/?view=settings&tab=network");
      await settingsDrawn(page); // rows found are the ones that stay (#1659)
      await page.locator("#registryList .registry-row").first().waitFor();
      return { page, errors, posts };
    };
    const row = (page, name) => page.locator("#registryList .registry-row", { has: page.locator(".name", { hasText: name }) }).first();

    // a fresh draw: off, the Default label, four ticks on, sync off
    {
      const { page, errors, posts } = await open();
      const serve = row(page, W.en.serve);
      const sw = serve.locator("button.registry-sw");
      assert.equal(await sw.getAttribute("role"), "switch");
      assert.equal(await sw.getAttribute("aria-checked"), "false");
      assert.equal((await serve.locator(".sub").textContent()).length > 10, true, "the switch's line says the consequence");
      // the URL greyed while off, with the /provider hint, and no copy
      const url = row(page, W.en.url);
      const code = url.locator("code");
      assert.equal(await code.textContent(), "http://127.0.0.1:3425/api.json");
      assert.equal(await code.evaluate((e) => getComputedStyle(e).color), "rgb(185, 185, 194)", "greyed while off"); // the light theme's --faint
      assert.equal((await url.locator(".sub").textContent()).trim(), W.en.hint);
      assert.equal(await url.locator("button.copy").count(), 0, "no copy while off");
      // the effort pick shows the Default label; the menu lists six
      const effort = row(page, W.en.effort);
      const pick = effort.locator("button.registry-effort");
      assert.equal((await pick.locator("span").textContent()).trim(), W.en.lead);
      await reach(page, pick);
      await pick.click();
      const items = page.locator(".proto-menu.registry-effort-menu .pm-item");
      assert.equal(await items.count(), 6, "Default plus the five levels");
      assert.equal((await items.first().textContent()).trim(), W.en.lead);
      assert.deepEqual(await items.evaluateAll((es) => es.slice(1).map((e) => e.querySelector(".pm-name").textContent)), EFFORTS);
      await page.keyboard.press("Escape");
      // four ticks, all pressed, labelled by the translated names
      const buckets = row(page, W.en.buckets);
      assert.deepEqual(await buckets.locator(".registry-bucket span:first-child").evaluateAll((es) => es.map((e) => e.textContent)), ["OpenAI (chat)", "Anthropic (messages)", "OpenAI (responses)", "Routing groups"]);
      assert.equal(await buckets.locator('.dot.tick[aria-pressed="true"]').count(), 4);
      assert.equal((await buckets.locator(".sub").textContent()).trim(), W.en.bucketsSub);
      // sync off
      const sync = row(page, W.en.sync);
      assert.equal(await sync.locator("button.registry-sw").getAttribute("aria-checked"), "false");
      assert.equal(await page.locator("#registryList select, #registryList input[type=checkbox]").count(), 0, "no native select or checkbox");
      assert.equal(await page.locator("#registryList .registry-row").evaluateAll((rs) => rs.some((r) => parseFloat(getComputedStyle(r).borderLeftWidth) > 1)), false, "no left border accent");
      assert.deepEqual(posts, []);
      assert.deepEqual(errors, []);
    }

    // toggling the switch posts registryOn with the keep keys; on, the URL
    // row gains its copy
    {
      const { page, posts } = await open();
      const sw = row(page, W.en.serve).locator("button.registry-sw");
      await reach(page, sw);
      await sw.click();
      await page.waitForFunction(() => document.querySelector("#registryList .registry-row .registry-sw")?.getAttribute("aria-checked") === "true");
      assert.equal(posts.length, 1);
      assert.equal(posts[0].registryOn, true);
      assert.deepEqual(posts[0].registryBuckets, BUCKETS, "the keep keys ride the save");
      assert.equal(posts[0].registryEffort, "");
      assert.equal(posts[0].registrySync, false);
      const url = row(page, W.en.url);
      await url.locator("button.copy").waitFor();
      assert.notEqual(await url.locator("code").evaluate((e) => getComputedStyle(e).color), "rgb(185, 185, 194)", "the URL is not greyed while on");
    }

    // the effort pick posts the level; the menu's leading label says the
    // env fallback applies when the state carries one. The view is wheeled
    // to the row first, as the reader would: the menu drops under its
    // anchor, and one opened at the view's foot would open past the window
    {
      const { page, posts } = await open();
      const pick = row(page, W.en.effort).locator("button.registry-effort");
      await reach(page, pick);
      await pick.click();
      await pickItem(page, page.locator(".proto-menu .pm-item", { hasText: "max" }).last());
      await page.waitForFunction(() => document.querySelector("#registryList .registry-effort span")?.textContent.trim() === "max");
      assert.equal(posts.at(-1).registryEffort, "max");
    }
    {
      const { page } = await open({ registry: { buckets: BUCKETS, efforts: EFFORTS, envEffort: "high" } });
      const pick = row(page, W.en.effort).locator("button.registry-effort");
      await reach(page, pick);
      await pick.click();
      assert.equal((await page.locator(".proto-menu .pm-item").first().textContent()).trim(), W.en.leadEnv);
      await page.keyboard.press("Escape");
    }

    // a bucket unticked posts the reduced list
    {
      const { page, posts } = await open();
      const dot = row(page, W.en.buckets).locator('.dot.tick[data-bucket="magpie-group"]');
      await reach(page, dot);
      await dot.click();
      await page.waitForFunction(() => document.querySelectorAll('#registryList .dot.tick[aria-pressed="true"]').length === 3);
      assert.deepEqual(posts.at(-1).registryBuckets, BUCKETS.slice(0, 3));
    }

    // the last bucket off: the refusal lands in the row, the ticks stay,
    // and no error toast follows
    {
      const { page, posts } = await open({ registryBuckets: ["magpie-group"] });
      const buckets = row(page, W.en.buckets);
      const dot = buckets.locator('.dot.tick[data-bucket="magpie-group"]');
      assert.equal(await dot.getAttribute("aria-pressed"), "true");
      await reach(page, dot);
      await dot.click();
      await page.waitForFunction((want) => document.querySelector("#registryList .registry-row:nth-child(4) .sub")?.textContent.trim() === want, W.en.refused, { timeout: 8000 }).catch(() => {});
      assert.equal((await row(page, W.en.buckets).locator(".sub").textContent()).trim(), W.en.refused, "the refusal in the row");
      assert.equal(await buckets.locator(".sub").evaluate((e) => e.classList.contains("err")), true);
      assert.equal(await page.locator('#registryList .dot.tick[aria-pressed="true"]').count(), 1, "the ticks are unchanged");
      assert.deepEqual(posts.at(-1).registryBuckets, [], "the all-off list was sent");
      assert.equal(await page.locator("#status").textContent(), "", "no error toast beside the row's reason");
      // a save that works clears the row's reason
      const sw = row(page, W.en.serve).locator("button.registry-sw");
      await reach(page, sw);
      await sw.click();
      await page.waitForFunction(() => !document.querySelector("#registryList .registry-row:nth-child(4) .sub")?.classList.contains("err"));
    }

    // in a browser on the page, the URL is the page's own origin
    {
      const { page } = await open({}, { webMode: true });
      assert.equal(await row(page, W.en.url).locator("code").textContent(), "http://localhost:3430/api.json");
    }

    // the resolved value rides the env label: not just "applies" but the
    // effort itself, on the row's button where the menu's lead said it
    {
      const { page } = await open({ registry: { buckets: BUCKETS, efforts: EFFORTS, envEffort: "high" } });
      const pick = row(page, W.en.effort).locator("button.registry-effort");
      assert.equal((await pick.locator("span").textContent()).trim(), W.en.leadEnv);
    }

    // a copy clicked really copies: the clipboard write carries the URL,
    // the button says the page's own "copied" word, and no toast error
    {
      const posts = [];
      const page = await (await browser.newContext({ viewport: { width: 900, height: 420 }, reducedMotion: "reduce" })).newPage();
      pages.push(page);
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.addInitScript(() => {
        window.__writes = [];
        Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: (s) => { window.__writes.push(s); return Promise.resolve(); } } });
      });
      await page.route("**/*", server({ registryOn: true }, posts));
      await watchSettingsReads(page);
      await page.goto("http://localhost:3430/?view=settings&tab=network");
      await settingsDrawn(page);
      await page.locator("#registryList .registry-row").first().waitFor();
      const cp = row(page, W.en.url).locator("button.copy");
      await cp.waitFor();
      await reach(page, cp);
      await cp.click();
      await page.waitForFunction((want) => document.querySelector("#status").textContent === want, W.en.copied);
      assert.deepEqual(await page.evaluate(() => window.__writes), ["http://127.0.0.1:3425/api.json"], "the URL reached the clipboard");
      assert.ok(await page.locator("#status").evaluate((e) => e.classList.contains("ok")));
      assert.ok(await cp.evaluate((b) => b.classList.contains("done")), "the button says it copied");
      assert.deepEqual(errors, []);
    }

    // narrow: the section still lays out as rows, nothing sideways at 420px
    {
      const { page } = await open({ registryOn: true });
      const wide = await page.setViewportSize({ width: 420, height: 700 });
      await page.waitForTimeout(100); // the relayout the resize asks settles
      const rows = page.locator("#registryList .registry-row");
      assert.equal(await rows.count(), 5);
      for (const r of await rows.all()) {
        assert.equal(await r.evaluate((e) => e.scrollWidth <= e.clientWidth + 1), true, "no row overflows at 420px");
        assert.equal(await r.evaluate((e) => e.getBoundingClientRect().right <= 420 + 1), true, "no row past the 420px edge");
      }
    }

    // the section in each language the page speaks: drawn and saved whole,
    // the all-off refusal said in the row by its own words — the row is
    // found by the translated name, so a half-drawn or English fallback
    // section cannot pass for a translated one
    for (const [lang, w] of Object.entries(W)) {
      const { page, posts } = await open({ registryBuckets: ["magpie-group"], lang }, { lang });
      const serve = row(page, w.serve);
      assert.equal(await serve.locator("button.registry-sw").getAttribute("aria-checked"), "false", lang + ": the section drew");
      assert.equal((await row(page, w.url).locator(".sub").textContent()).trim(), w.hint, lang + ": the /provider hint");
      assert.equal((await row(page, w.effort).locator("button.registry-effort span").textContent()).trim(), w.lead, lang + ": the Default label");
      assert.equal((await row(page, w.buckets).locator(".sub").textContent()).trim(), w.bucketsSub, lang + ": the buckets row");
      assert.equal((await row(page, w.sync).locator(".sub").textContent()).trim(), w.syncSub, lang + ": the sync row says the config rides sync, the switch stays per-machine");
      const dot = row(page, w.buckets).locator('.dot.tick[data-bucket="magpie-group"]');
      await reach(page, dot);
      await dot.click();
      await page.waitForFunction((want) => document.querySelector("#registryList .registry-row:nth-child(4) .sub")?.textContent.trim() === want, w.refused, { timeout: 5000 }).catch(() => {});
      assert.equal((await row(page, w.buckets).locator(".sub").textContent()).trim(), w.refused, lang + ": the refusal in the row");
      assert.equal(await page.locator("#status").textContent(), "", lang + ": no error toast beside the row's reason");
      assert.deepEqual(posts.at(-1).registryBuckets, [], lang + ": the all-off list was sent");
    }
  });
}
