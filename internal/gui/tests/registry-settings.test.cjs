// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings › Network and sharing › Model registry (registry-settings.md):
// the registry /api.json serves clients that import a models.dev-shaped
// custom registry. The section draws whole from a fresh state — registry
// off, effort "Default (model's middle)", all four wires ticked, routing
// groups on, sync off — with the registry URL greyed while off but its
// copy button always beside it and its hint naming no one client. The
// registry, groups, and sync switches draw as the horizontal On/Off
// selectors the rest of Settings uses (aria-pressed on their opts);
// toggling any posts the flat key with the page's keep keys; the effort
// pick menu lists Default plus the five levels and posts the choice; a
// wire's tick posts the reduced list — every wire off with the groups on
// saves clean (groups-only), and with the groups off the save refuses
// registry_empty: the row says why in the page's language, naming both
// levers, with no toast beside it. No native select or checkbox, no
// coloured left border; Chromium and WebKit, no backend — the API is
// faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, reach, pickItem } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const WIRES = ["magpie-openai", "magpie-anthropic", "magpie-openai-responses", "magpie-gemini"];
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
    registry: { buckets: WIRES, efforts: EFFORTS },
    registryOn: false, registryEffort: "", registryBuckets: WIRES, registryGroups: true, registrySync: false,
    ...over,
  };
}

// server fakes the settings API. A save that would list nothing — every
// wire off and the routing groups off — is answered with the save's
// refusal code, as internal/gui's fail() maps settings.ErrRegistryEmpty;
// a groups-only save (registryBuckets: [], groups on) is legal.
function server(over, posts, { webMode = false, lang = "en" } = {}) {
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
      if (Array.isArray(body.registryBuckets) && body.registryBuckets.length === 0 && body.registryGroups === false) {
        return json({ error: "the registry lists nothing with no wire and no routing groups: leave one on", code: "registry_empty" }, 400);
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
    hint: "A models.dev-shaped custom registry any able client can import; kimi-code’s /provider is one way to add it",
    effort: "Default thinking effort",
    lead: "Default (model’s middle)",
    leadEnv: "(model’s middle; high applies)",
    wires: "Wires",
    gemini: "Google GenAI",
    wiresSub: "Which wires the registry carries models on, named for the client that speaks each",
    groups: "List routing groups",
    groupsSub: "The registry’s routing groups ride their own OpenAI-compatible entry beside the wires: listed while this is on, so a curated set imports even with every wire off",
    refused: "The registry would list nothing: leave a wire on, or turn List routing groups on",
    sync: "Sync across machines",
    syncSub: "The registry’s config — on or off, the effort, the wires, the routing groups — rides sync, while this switch stays on each computer’s own: the config lands only where the switch is on, so the coupling needs every synced box’s switch on",
    copied: "Copied Registry URL",
  },
  zh: {
    serve: "提供模型注册表",
    url: "注册表 URL",
    hint: "这是一个 models.dev 形状的自定义注册表，任何能导入自定义注册表的客户端都可以使用；kimi-code 的 /provider 是添加方式之一",
    effort: "默认思考力度",
    lead: "默认（模型的中间档）",
    leadEnv: "（模型的中间档；high 生效中）",
    wires: "协议线",
    gemini: "Google GenAI",
    wiresSub: "注册表以哪些协议线承载模型，各以说出该协议的客户端命名",
    groups: "列出路由组",
    groupsSub: "注册表的路由组走自己的 OpenAI 兼容条目，与协议线并列：打开此开关才列出，所以即使所有协议线都关掉，精选的一组模型也能导入",
    refused: "注册表将列不出任何模型：保留一条协议线，或打开「列出路由组」",
    sync: "跨机器同步",
    syncSub: "注册表的配置——开关、思考力度、协议线、路由组——随同步走，而这个开关留在每台电脑自己手上：配置只落在开关打开的机器上，所以联动需要每台同步的机器都打开此开关",
    copied: "已复制注册表 URL",
  },
  "zh-TW": {
    serve: "提供模型登錄檔",
    url: "登錄檔 URL",
    hint: "這是一個 models.dev 形狀的自訂登錄檔，任何能匯入自訂登錄檔的用戶端都可以使用；kimi-code 的 /provider 是新增方式之一",
    effort: "預設思考力度",
    lead: "預設（模型的中間檔）",
    leadEnv: "（模型的中間檔；high 生效中）",
    wires: "協定線",
    gemini: "Google GenAI",
    wiresSub: "登錄檔以哪些協定線承載模型，各以說出該協定的用戶端命名",
    groups: "列出路由組",
    groupsSub: "登錄檔的路由組走自己的 OpenAI 相容條目，與協定線並列：開啟此開關才會列出，所以即使所有協定線都關掉，精選的一組模型也能匯入",
    refused: "登錄檔將列不出任何模型：保留一條協定線，或開啟「列出路由組」",
    sync: "跨機器同步",
    syncSub: "登錄檔的設定——開關、思考力度、協定線、路由組——隨同步走，而這個開關留在每台電腦自己手上：設定只落在開關開啟的機器上，所以連動需要每台同步的機器都開啟此開關",
    copied: "已複製登錄檔 URL",
  },
  ja: {
    serve: "レジストリを提供する",
    url: "レジストリ URL",
    hint: "models.dev 形状のカスタムレジストリで、インポートできるクライアントならどれでも使えます。kimi-code の /provider は追加方法の一つです",
    effort: "既定の思考レベル",
    lead: "既定（モデルの中間）",
    leadEnv: "（モデルの中間。high が適用されます）",
    wires: "ワイヤ",
    gemini: "Google GenAI",
    wiresSub: "レジストリがどのワイヤでモデルを運ぶか。それぞれを話すクライアントの名で呼ばれます",
    groups: "ルーティンググループを列挙する",
    groupsSub: "レジストリのルーティンググループは、ワイヤとは別に独自の OpenAI 互換エントリで提供されます。このスイッチがオンの間だけ列挙され、ワイヤがすべてオフでも厳選した一式をインポートできます",
    refused: "レジストリに何も列挙されなくなります。ワイヤを 1 つオンにするか、「ルーティンググループを列挙する」をオンにしてください",
    sync: "マシン間で同期",
    syncSub: "レジストリの設定——オン・オフ、思考レベル、ワイヤ、ルーティンググループ——は同期に乗りますが、このスイッチ自体は各コンピューターのものです。設定はスイッチがオンのマシンにしか届かないため、連動には同期するすべてのマシンでこのスイッチをオンにする必要があります",
    copied: "レジストリ URL をコピーしました",
  },
  de: {
    serve: "Registry anbieten",
    url: "Registry-URL",
    hint: "Eine eigene Registry nach models.dev-Art, die jeder fähige Client importieren kann; kimi-codes /provider ist ein Weg, sie hinzuzufügen",
    effort: "Standard-Denkstufe",
    lead: "Standard (Mitte des Modells)",
    leadEnv: "(Mitte des Modells; high greift)",
    wires: "Drähte",
    gemini: "Google GenAI",
    wiresSub: "Auf welchen Drähten die Registry Modelle trägt, benannt nach dem Client, der jedes spricht",
    groups: "Weiterleitungsgruppen listen",
    groupsSub: "Die Weiterleitungsgruppen der Registry reiten auf ihrem eigenen OpenAI-kompatiblen Eintrag neben den Drähten: gelistet, solange dies an ist, sodass eine kuratierte Auswahl selbst mit jedem Draht aus importiert",
    refused: "Die Registry würde nichts listen: Lasse einen Draht an oder schalte Weiterleitungsgruppen listen an",
    sync: "Über Rechner hinweg synchronisieren",
    syncSub: "Die Konfiguration der Registry — an oder aus, die Denkstufe, die Drähte, die Weiterleitungsgruppen — reitet auf dem Sync, während dieser Schalter jedem Rechner selbst bleibt: Die Konfiguration landet nur, wo der Schalter an ist, deshalb braucht die Kopplung den Schalter von jeder synchronisierten Box an",
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
    // the On/Off selector's pressed half, by aria state: one opt pressed,
    // saying which
    const segAt = (loc) => loc.locator(".segs .opt[aria-pressed='true']");
    // a save redraws the section's rows (renderSettings); a seg's click
    // moves its own aria state at once, so waiting on the seg is no sign
    // the save's redraw came. The mark set on a row before the click
    // leaves the page only with that redraw: reads after it see the
    // saved state, never the row the click left behind (chromium under
    // parallel load read the URL's grey a redraw early).
    const saving = (loc) => loc.evaluate((e) => { e.dataset.saving = "1"; });
    const redrawn = (page) => page.waitForFunction(() => !document.querySelector("#registryList [data-saving]"), null, { timeout: 8000 });

    // a fresh draw: registry off, the Default label, four ticks on,
    // groups on, sync off — and the copy beside the greyed URL while off
    {
      const { page, errors, posts } = await open();
      const serve = row(page, W.en.serve);
      assert.equal(await serve.locator(".segs .opt").count(), 2, "the registry switch is an Off/On selector");
      assert.equal((await segAt(serve).textContent()).trim(), "Off");
      assert.equal((await serve.locator(".sub").textContent()).length > 10, true, "the switch's line says the consequence");
      // the URL greyed while off, the client-neutral hint under it, and
      // the copy button beside it anyway
      const url = row(page, W.en.url);
      const code = url.locator("code");
      assert.equal(await code.textContent(), "http://127.0.0.1:3425/api.json");
      assert.equal(await code.evaluate((e) => getComputedStyle(e).color), "rgb(185, 185, 194)", "greyed while off"); // the light theme's --faint
      assert.equal((await url.locator(".sub").textContent()).trim(), W.en.hint);
      assert.equal(await url.locator("button.copy").count(), 1, "the copy sits beside the URL while off");
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
      // four wire ticks, all pressed, labelled by the translated names —
      // the Gemini wire fourth, the routing groups gone from the row
      const wires = row(page, W.en.wires);
      assert.deepEqual(await wires.locator(".registry-bucket span:first-child").evaluateAll((es) => es.map((e) => e.textContent)), ["OpenAI (chat)", "Anthropic (messages)", "OpenAI (responses)", "Google GenAI"]);
      assert.equal(await wires.locator('.dot.tick[aria-pressed="true"]').count(), 4);
      assert.equal((await wires.locator(".sub").textContent()).trim(), W.en.wiresSub);
      // the groups selector sits directly under the Wires row, on
      const groups = row(page, W.en.groups);
      assert.equal(await page.locator("#registryList .registry-row").evaluateAll((rs, name) => rs.findIndex((r) => r.textContent.includes(name)), W.en.groups), 4, "the groups row is the fifth, directly under Wires");
      assert.equal(await groups.locator(".segs .opt").count(), 2, "the groups switch is an Off/On selector");
      assert.equal((await segAt(groups).textContent()).trim(), "On");
      assert.equal((await groups.locator(".sub").textContent()).trim(), W.en.groupsSub);
      // sync off
      const sync = row(page, W.en.sync);
      assert.equal((await segAt(sync).textContent()).trim(), "Off");
      assert.equal(await page.locator("#registryList select, #registryList input[type=checkbox]").count(), 0, "no native select or checkbox");
      assert.equal(await page.locator("#registryList .registry-row").evaluateAll((rs) => rs.some((r) => parseFloat(getComputedStyle(r).borderLeftWidth) > 1)), false, "no left border accent");
      assert.deepEqual(posts, []);
      assert.deepEqual(errors, []);
    }

    // toggling the registry posts registryOn with the keep keys; on, the
    // URL code leaves the grey — a state only the save's redraw draws, so
    // the wait is on the colour itself, not the seg the click moved
    {
      const { page, posts } = await open();
      const on = row(page, W.en.serve).locator(".segs .opt", { hasText: "On" });
      await reach(page, on);
      await on.click();
      await page.waitForFunction((faint) => {
        const code = document.querySelector("#registryList .registry-row:nth-child(2) code");
        return code !== null && getComputedStyle(code).color !== faint;
      }, "rgb(185, 185, 194)", { timeout: 8000 });
      assert.equal(posts.length, 1);
      assert.equal(posts[0].registryOn, true);
      assert.deepEqual(posts[0].registryBuckets, WIRES, "the keep keys ride the save");
      assert.equal(posts[0].registryGroups, true);
      assert.equal(posts[0].registryEffort, "");
      assert.equal(posts[0].registrySync, false);
      const url = row(page, W.en.url);
      assert.equal(await url.locator("button.copy").count(), 1, "the copy stays while on");
      assert.notEqual(await url.locator("code").evaluate((e) => getComputedStyle(e).color), "rgb(185, 185, 194)", "the URL is not greyed while on");
    }

    // the groups selector posts its own key, alone — the seg moves at the
    // click, so the read waits for the save's redraw by its mark
    {
      const { page, posts } = await open();
      const groups = row(page, W.en.groups);
      const off = groups.locator(".segs .opt", { hasText: "Off" });
      await reach(page, off);
      await saving(groups);
      await off.click();
      await redrawn(page);
      assert.equal(posts.length, 1);
      assert.equal(posts[0].registryGroups, false);
      assert.deepEqual(posts[0].registryBuckets, WIRES, "a wire stays on, so the save is clean");
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
      const { page } = await open({ registry: { buckets: WIRES, efforts: EFFORTS, envEffort: "high" } });
      const pick = row(page, W.en.effort).locator("button.registry-effort");
      await reach(page, pick);
      await pick.click();
      assert.equal((await page.locator(".proto-menu .pm-item").first().textContent()).trim(), W.en.leadEnv);
      await page.keyboard.press("Escape");
    }

    // the Gemini wire unticked posts the reduced list
    {
      const { page, posts } = await open();
      const dot = row(page, W.en.wires).locator('.dot.tick[data-bucket="magpie-gemini"]');
      await reach(page, dot);
      await dot.click();
      await page.waitForFunction(() => document.querySelectorAll('#registryList .dot.tick[aria-pressed="true"]').length === 3);
      assert.deepEqual(posts.at(-1).registryBuckets, WIRES.slice(0, 3));
    }

    // every wire off with the groups on saves clean: groups-only is legal,
    // the ticks land unpressed and no refusal follows
    {
      const { page, posts } = await open({ registryBuckets: ["magpie-gemini"], registryGroups: true });
      const dot = row(page, W.en.wires).locator('.dot.tick[data-bucket="magpie-gemini"]');
      await reach(page, dot);
      await dot.click();
      await page.waitForFunction(() => document.querySelectorAll('#registryList .dot.tick[aria-pressed="true"]').length === 0);
      assert.deepEqual(posts.at(-1).registryBuckets, [], "the groups-only list was sent");
      assert.equal(await row(page, W.en.wires).locator(".sub.err").count(), 0, "no refusal: groups-only saves");
      assert.equal(await page.locator("#status").evaluate((e) => e.classList.contains("err")), false, "no error toast either — the save's own \"Saved\" is fine");
    }

    // the last wire off with the groups off: the refusal lands in the
    // Wires row, the ticks stay, and no error toast follows; a save that
    // works clears the row's reason
    {
      const { page, posts } = await open({ registryBuckets: ["magpie-gemini"], registryGroups: false });
      const wires = row(page, W.en.wires);
      const dot = wires.locator('.dot.tick[data-bucket="magpie-gemini"]');
      assert.equal(await dot.getAttribute("aria-pressed"), "true");
      await reach(page, dot);
      await dot.click();
      await page.waitForFunction((want) => document.querySelector("#registryList .registry-row:nth-child(4) .sub")?.textContent.trim() === want, W.en.refused, { timeout: 8000 }).catch(() => {});
      assert.equal((await row(page, W.en.wires).locator(".sub").textContent()).trim(), W.en.refused, "the refusal in the row");
      assert.equal(await wires.locator(".sub").evaluate((e) => e.classList.contains("err")), true);
      assert.equal(await page.locator('#registryList .dot.tick[aria-pressed="true"]').count(), 1, "the ticks are unchanged");
      assert.deepEqual(posts.at(-1).registryBuckets, [], "the all-off list was sent");
      assert.equal(await page.locator("#status").textContent(), "", "no error toast beside the row's reason");
      // a save that works clears the row's reason: a wire back on
      const back = wires.locator('.dot.tick[data-bucket="magpie-openai"]');
      await reach(page, back);
      await back.click();
      await page.waitForFunction(() => !document.querySelector("#registryList .registry-row:nth-child(4) .sub")?.classList.contains("err"));
    }

    // the groups lever refuses too: groups-only with the groups turned off
    // names both levers in the groups row, and the selector stays on
    {
      const { page, posts } = await open({ registryBuckets: [], registryGroups: true });
      const groups = row(page, W.en.groups);
      const off = groups.locator(".segs .opt", { hasText: "Off" });
      await reach(page, off);
      await off.click();
      await page.waitForFunction((want) => document.querySelector("#registryList .registry-row:nth-child(5) .sub")?.textContent.trim() === want, W.en.refused, { timeout: 8000 }).catch(() => {});
      assert.equal((await row(page, W.en.groups).locator(".sub").textContent()).trim(), W.en.refused, "the refusal in the groups row");
      assert.equal(await groups.locator(".sub").evaluate((e) => e.classList.contains("err")), true);
      assert.equal((await segAt(groups).textContent()).trim(), "On", "the selector did not move");
      assert.deepEqual(posts.at(-1).registryBuckets, []);
      assert.equal(posts.at(-1).registryGroups, false);
      assert.equal(await page.locator("#status").textContent(), "", "no error toast beside the row's reason");
    }

    // in a browser on the page, the URL is the page's own origin
    {
      const { page } = await open({}, { webMode: true });
      assert.equal(await row(page, W.en.url).locator("code").textContent(), "http://localhost:3430/api.json");
    }

    // the resolved value rides the env label: not just "applies" but the
    // effort itself, on the row's button where the menu's lead said it
    {
      const { page } = await open({ registry: { buckets: WIRES, efforts: EFFORTS, envEffort: "high" } });
      const pick = row(page, W.en.effort).locator("button.registry-effort");
      assert.equal((await pick.locator("span").textContent()).trim(), W.en.leadEnv);
    }

    // a copy clicked while the registry is off really copies: the
    // clipboard write carries the URL, the button says the page's own
    // "copied" word, and no toast error
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
      await page.route("**/*", server({ registryOn: false }, posts));
      await watchSettingsReads(page);
      await page.goto("http://localhost:3430/?view=settings&tab=network");
      await settingsDrawn(page);
      await page.locator("#registryList .registry-row").first().waitFor();
      const cp = row(page, W.en.url).locator("button.copy");
      await cp.waitFor();
      await reach(page, cp);
      await cp.click();
      await page.waitForFunction((want) => document.querySelector("#status").textContent === want, W.en.copied);
      assert.deepEqual(await page.evaluate(() => window.__writes), ["http://127.0.0.1:3425/api.json"], "the URL reached the clipboard while off");
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
      assert.equal(await rows.count(), 6);
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
      const { page, posts } = await open({ registryBuckets: ["magpie-gemini"], registryGroups: false, lang }, { lang });
      const serve = row(page, w.serve);
      assert.equal(await serve.locator(".segs .opt").first().getAttribute("aria-pressed"), "true", lang + ": the section drew, registry off");
      assert.equal((await row(page, w.url).locator(".sub").textContent()).trim(), w.hint, lang + ": the client-neutral hint");
      assert.equal((await row(page, w.effort).locator("button.registry-effort span").textContent()).trim(), w.lead, lang + ": the Default label");
      assert.equal((await row(page, w.wires).locator(".sub").textContent()).trim(), w.wiresSub, lang + ": the wires row");
      assert.equal(await row(page, w.wires).locator(".registry-bucket", { hasText: w.gemini }).count(), 1, lang + ": the Gemini wire is named");
      assert.equal((await row(page, w.groups).locator(".sub").textContent()).trim(), w.groupsSub, lang + ": the groups row");
      assert.equal((await row(page, w.groups).locator(".segs .opt").first().getAttribute("aria-pressed")), "true", lang + ": groups off, as the payload said");
      assert.equal((await row(page, w.sync).locator(".sub").textContent()).trim(), w.syncSub, lang + ": the sync row says the config rides sync, the switch stays per-machine");
      const dot = row(page, w.wires).locator('.dot.tick[data-bucket="magpie-gemini"]');
      await reach(page, dot);
      await dot.click();
      await page.waitForFunction((want) => document.querySelector("#registryList .registry-row:nth-child(4) .sub")?.textContent.trim() === want, w.refused, { timeout: 5000 }).catch(() => {});
      assert.equal((await row(page, w.wires).locator(".sub").textContent()).trim(), w.refused, lang + ": the refusal in the row");
      assert.equal(await page.locator("#status").textContent(), "", lang + ": no error toast beside the row's reason");
      assert.deepEqual(posts.at(-1).registryBuckets, [], lang + ": the all-off list was sent");
    }
  });
}
