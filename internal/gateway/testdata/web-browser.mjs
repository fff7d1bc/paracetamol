import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
import { mkdir, readFile, mkdtemp, rm, symlink, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";
import { join } from "node:path";

const { default: puppeteer } = await import(pathToFileURL(process.env.PARACETAMOL_TEST_PUPPETEER));
const browser = await puppeteer.launch({ executablePath: process.env.PARACETAMOL_TEST_BROWSER, headless: true });
const [plain, locked] = process.argv.slice(2);
try {
  const page = await browser.newPage();
  const errors = [];
  const requests = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("request", request => requests.push({ url: request.url(), method: request.method() }));
  await page.setViewport({ width: 1440, height: 1100 });
  await page.emulateTimezone("America/New_York");
  const snapshotResponse = page.waitForResponse(response => response.url().endsWith("/paracetamol/v1/ui"));
  await page.goto(plain);
  await page.waitForFunction(() => document.querySelector("#connection-state").textContent === "Gateway reachable");
  const snapshot = await (await snapshotResponse).json();
  assert(!requests.some(request => request.url.includes("/statistics")), "statistics fetched before its tab was selected");
  assert.equal(await page.$eval("#snapshot-time", node => node.textContent), "Checked " + new Date(snapshot.collected_at).toISOString());
  assert.equal(await page.$eval("#overview", node => node.hidden), false);
  assert.equal(await page.$eval("#configuration", node => node.hidden), true);
  assert.equal(await page.evaluate(() => getComputedStyle(document.documentElement).scrollbarGutter), "stable");
  const shellBounds = () => page.$eval(".shell", node => {
    const { x, width } = node.getBoundingClientRect();
    return { x, width };
  });
  const overviewBounds = await shellBounds();
  await page.focus("#tab-overview");
  await page.keyboard.press("ArrowRight");
  await page.waitForFunction(() => !document.querySelector("#configuration").hidden);
  assert.equal(await page.$eval("#overview", node => node.hidden), true);
  assert.deepEqual(await shellBounds(), overviewBounds, "scrollbar shifted the page");
  for (const summary of await page.$$("#configuration details > summary")) {
    assert.equal(await summary.evaluate(node => getComputedStyle(node).listStyleType), "none");
    assert.equal(await summary.evaluate(node => getComputedStyle(node, "::after").content), '"Show"');
    await summary.focus();
    await page.keyboard.press("Enter");
    assert.equal(await summary.evaluate(node => node.parentElement.open), true);
    assert.equal(await summary.evaluate(node => getComputedStyle(node, "::after").content), '"Hide"');
    assert.deepEqual(await shellBounds(), overviewBounds, "expansion shifted the page");
    await page.keyboard.press("Space");
    assert.equal(await summary.evaluate(node => node.parentElement.open), false);
  }
  assert.equal(await page.$eval("#api-address", node => node.textContent), plain + "/v1");
  assert.match(await page.$eval("#default-model-note", node => node.textContent), /xhigh/);
  const command = await page.$eval("#setup-command", node => node.textContent);
  assert(!command.includes("\n+"), "shell command includes patch markers");
  assert(command.includes("set -euC") && command.includes("[ ! -L"), "setup lost overwrite guards");
  assert(!command.includes("mktemp"), "setup still uses temporary directories");
  assert(command.includes(encodeURIComponent(plain + "/v1")), "export does not preserve browser origin");
  const home = await mkdtemp(join(tmpdir(), "paracetamol-web-test-"));
  try {
    const env = { ...process.env, HOME: home, TMPDIR: home };
    delete env.XDG_CONFIG_HOME;
    const run = () => spawnSync("sh", ["-c", command], { env, encoding: "utf8" });
    const first = run();
    assert.equal(first.status, 0, first.stderr);
    const original = await readFile(join(home, ".config/maki/providers.toml"), "utf8");
    assert.match(original, /base_url/);
    assert.notEqual(run().status, 0, "setup overwrote existing files");
    assert.equal(await readFile(join(home, ".config/maki/providers.toml"), "utf8"), original);
    assert.equal((await stat(join(home, ".config/maki/providers.toml"))).mode & 0o777, 0o600);
    env.XDG_CONFIG_HOME = join(home, "custom config");
    assert.equal(run().status, 0, "custom XDG location failed");
    assert.equal(await readFile(join(env.XDG_CONFIG_HOME, "maki/providers.toml"), "utf8"), original);
    env.XDG_CONFIG_HOME = join(home, "symlink config");
    await mkdir(join(env.XDG_CONFIG_HOME, "maki"), { recursive: true });
    await symlink(join(home, "missing"), join(env.XDG_CONFIG_HOME, "maki/init.lua"));
    assert.notEqual(run().status, 0, "setup accepted a dangling symlink");
    await assert.rejects(stat(join(env.XDG_CONFIG_HOME, "maki/providers.toml")), { code: "ENOENT" });
    env.XDG_CONFIG_HOME = join(home, "failed config");
    const broken = command.replace("/clients/maki/init.lua", "/clients/maki/missing.lua");
    assert.notEqual(spawnSync("sh", ["-c", broken], { env, encoding: "utf8" }).status, 0);
    await assert.rejects(stat(join(env.XDG_CONFIG_HOME, "maki/providers.toml")), { code: "ENOENT" });
  } finally { await rm(home, { recursive: true, force: true }); }
  await page.click('[data-preview="providers.toml"]');
  await page.waitForFunction(() => !document.querySelector("#preview-panel").hidden);
  assert.match(await page.$eval("#preview-content", node => node.textContent), new RegExp(plain.replaceAll(".", "\\.")));
  assert.equal(await page.$eval("#preview-content", node => node.children.length), 0);

  // Exercise the insecure-HTTP clipboard fallback, including browsers that
  // deny programmatic copying entirely. The selected text remains usable.
  await page.click("#tab-overview");
  await page.waitForFunction(() => !document.querySelector("#overview").hidden);
  await page.evaluate(() => { Object.defineProperty(navigator, "clipboard", { value: undefined }); document.execCommand = () => false; });
  await page.click('[data-copy="api-address"]');
  assert.equal(await page.evaluate(() => window.getSelection().toString()), plain + "/v1");

  await page.click("#tab-models");
  await page.waitForFunction(() => !document.querySelector("#models").hidden);
  assert.equal(await page.$$eval(".model-row", nodes => nodes.length), 3);
  assert.equal(await page.$eval("#model-groups", node => node.querySelectorAll("img").length), 0);
  await page.click(".model-row summary");
  assert.equal(await page.$eval(".model-row summary", node => getComputedStyle(node, "::after").content), '"Hide"');
  await page.click("#refresh");
  await page.waitForFunction(() => !document.querySelector("#refresh").disabled);
  assert.equal(await page.$$eval(".model-row details[open]", nodes => nodes.length), 1);
  assert.match(await page.$eval(".model-detail-grid", node => node.textContent), /medium/);
  assert.match(await page.$eval(".model-detail-grid", node => node.textContent), /xhigh/);
  assert.match(await page.$eval(".model-detail-grid", node => node.textContent), /Configured request slots4/);
  assert.match(await page.$eval(".model-detail-grid", node => node.textContent), /shared across slots/);
  await page.type("#model-search", "27b");
  assert.equal(await page.$$eval(".model-row", nodes => nodes.length), 1);
  await page.$eval("#model-search", node => { node.value = "nothing matches"; node.dispatchEvent(new Event("input")); });
  assert.match(await page.$eval("#models-message", node => node.textContent), /No models match/);
  await page.$eval("#model-search", node => { node.value = ""; node.dispatchEvent(new Event("input")); });

  const screenshotDir = process.env.PARACETAMOL_TEST_SCREENSHOTS;
  if (screenshotDir) {
    await mkdir(screenshotDir, { recursive: true });
    await page.screenshot({ path: join(screenshotDir, "models.png"), fullPage: true });
    const session = await page.createCDPSession();
    const downloadDir = await mkdtemp(join(screenshotDir, "download-"));
    await session.send("Browser.setDownloadBehavior", { behavior: "allow", downloadPath: downloadDir });
    await page.click("#tab-configuration");
    await page.waitForFunction(() => !document.querySelector("#configuration").hidden);
    await page.click('[data-download="providers.toml"]');
    await page.waitForFunction(() => document.querySelector("#feedback").textContent.startsWith("Download requested"));
    // Download completion is asynchronous. Poll the exact test-owned file.
    for (let attempt = 0; ; attempt++) {
      try { assert((await readFile(join(downloadDir, "providers.toml"), "utf8")).includes(plain + "/v1")); break; }
      catch (error) { if (attempt >= 30) throw error; await new Promise(resolve => setTimeout(resolve, 100)); }
    }
    await page.screenshot({ path: join(screenshotDir, "configuration.png"), fullPage: true });
    await page.click("#tab-overview");
    await page.waitForFunction(() => !document.querySelector("#overview").hidden);
    await page.screenshot({ path: join(screenshotDir, "overview.png"), fullPage: true });
  }
  await page.setViewport({ width: 390, height: 844 });
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), "mobile page overflows");
  if (screenshotDir) await page.screenshot({ path: join(screenshotDir, "mobile.png"), fullPage: true });
  for (const tab of ["configuration", "models", "statistics", "overview"]) {
    await page.click("#tab-" + tab);
    await page.waitForFunction(id => !document.getElementById(id).hidden, {}, tab);
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), `mobile ${tab} page overflows`);
    if (tab === "statistics") {
      await page.waitForFunction(() => document.querySelector("#statistics-status").textContent.includes("recording is disabled"));
      assert.equal(await page.$eval("#statistics-content", node => node.hidden), true, "real disabled route showed totals");
      assert(!await page.$eval("#statistics-recording", node => node.textContent.includes("Retention: 0")), "disabled route showed zero-day retention");
    }
  }
  await page.goto(plain + "/#configuration");
  await page.waitForFunction(() => !document.querySelector("#configuration").hidden && document.querySelector("#connection-state").textContent === "Gateway reachable");

  // Statistics fixtures intercept only the protected read route. Browser
  // actions still use the embedded assets and never contact inference.
  const statisticsPage = await browser.newPage();
  statisticsPage.on("pageerror", error => errors.push(error.message));
  await statisticsPage.setViewport({ width: 1440, height: 1100 });
  await statisticsPage.emulateTimezone("America/New_York");
  const metric = (total = 0, observations = 0) => ({ total, observations });
  const emptyUsage = () => ({ requests: 0, succeeded: 0, failed: 0, canceled: 0, input: metric(), output: metric(), cached: metric(), reasoning: metric(), cache_reuse: { input_tokens: 0, cached_tokens: 0, observations: 0 }, total_ms: metric(), first_output_ms: metric() });
  const statisticsFixture = days => {
    const usage = { ...emptyUsage(), requests: 4, succeeded: 2, failed: 1, canceled: 1, input: metric(days * 10000, 3), output: metric(days * 4000, 2), cached: metric(2000, 1), cache_reuse: { input_tokens: 8000, cached_tokens: 2000, observations: 1, ratio: .25 } };
    const daily = Array.from({ length: days }, (_, index) => {
      const date = new Date("2026-10-17T00:00:00Z");
      date.setUTCDate(date.getUTCDate() - days + 1 + index);
      return { date: date.toISOString().slice(0, 10), usage: index === days - 1 ? usage : emptyUsage() };
    });
    return { schema: "paracetamol.statistics.v1", state: "ready", retention_days: 90, period_days: days, from: daily[0].date, to: "2026-10-17", collected_at: "2026-10-17T15:04:05Z", recording_since: "2026-10-01", incomplete: false, dropped_requests: 0, pending_requests: 0, overall: usage, daily, models: [{ model: 'retired/<img src=x onerror="alert(1)">', application: "llama-cpp", usage }] };
  };
  let statisticsMode = "unavailable";
  let heldRequest;
  let statisticsRequests = 0;
  const statisticsReads = [];
  const respondStatistics = (request, data) => request.respond({ status: 200, headers: { "Content-Type": "application/json", "X-Paracetamol-Gateway": "paracetamol.gateway.v1" }, body: JSON.stringify(data) });
  await statisticsPage.setRequestInterception(true);
  statisticsPage.on("request", async request => {
    statisticsReads.push({ url: request.url(), method: request.method() });
    if (!request.url().includes("/paracetamol/v1/statistics?")) return request.continue();
    statisticsRequests++;
    if (statisticsMode === "unavailable") return request.respond({ status: 503, headers: { "Content-Type": "application/json", "X-Paracetamol-Gateway": "paracetamol.gateway.v1" }, body: JSON.stringify({ error: { message: "Gateway statistics are unavailable", type: "gateway_error", code: "statistics_unavailable" } }) });
    if (statisticsMode === "error") return request.abort();
    if (statisticsMode === "hold") { heldRequest = request; return; }
    const days = Number(new URL(request.url()).searchParams.get("days"));
    const data = statisticsFixture(days);
    if (statisticsMode === "disabled") { data.state = "disabled"; data.retention_days = 0; delete data.recording_since; }
    if (statisticsMode === "empty") { data.overall = emptyUsage(); data.models = []; data.daily.forEach(day => { day.usage = emptyUsage(); }); }
    if (statisticsMode === "incomplete") { data.incomplete = true; data.dropped_requests = 2; data.pending_requests = 1; data.overall.cached = metric(); data.overall.cache_reuse = { input_tokens: 0, cached_tokens: 0, observations: 0 }; }
    if (statisticsMode === "zero") { data.overall.output = metric(0, 1); data.overall.cache_reuse = { input_tokens: 0, cached_tokens: 0, observations: 0 }; }
    if (statisticsMode === "malformed") data.period_days = days === 1 ? 7 : 1;
    return respondStatistics(request, data);
  });
  await statisticsPage.goto(plain);
  await statisticsPage.waitForFunction(() => document.querySelector("#connection-state").textContent === "Gateway reachable");
  assert.equal(statisticsRequests, 0);
  await statisticsPage.click("#tab-statistics");
  await statisticsPage.waitForFunction(() => document.querySelector("#statistics-status").textContent.includes("Statistics are unavailable"));
  assert.equal(statisticsRequests, 1);
  assert.equal(await statisticsPage.$eval("#statistics-content", node => node.hidden), true);
  assert.equal(await statisticsPage.$eval("#statistics-metadata", node => node.hidden), true);
  assert(!await statisticsPage.$eval("#statistics-status", node => node.textContent.includes("stale")), "first failed read claimed stale data");
  assert.match(await statisticsPage.$eval("#statistics-status", node => node.textContent), /HTTP 503/);
  statisticsMode = "ready";
  await statisticsPage.click("#refresh-statistics");
  await statisticsPage.waitForFunction(() => !document.querySelector("#statistics-content").hidden);
  assert.equal(statisticsRequests, 2);
  assert.match(await statisticsPage.$eval("#statistics-period", node => node.textContent), /2026-10-11 through 2026-10-17/);
  assert.equal(await statisticsPage.$eval("#statistics-updated", node => node.textContent), "Last updated 2026-10-17T15:04:05.000Z");
  assert.match(await statisticsPage.$eval("#statistics-recording", node => node.textContent), /2026-10-01.*90 days/);
  assert.equal(await statisticsPage.$eval(".cache-card .statistic-value", node => node.textContent), "25%");
  assert.match(await statisticsPage.$eval("#statistics-summary", node => node.textContent), /3 of 4 requests observed · partial/);
  assert.equal(await statisticsPage.$$eval(".daily-chart-row", nodes => nodes.length), 7);
  assert.equal(await statisticsPage.$eval("#statistics-model-table code", node => node.textContent), 'retired/<img src=x onerror="alert(1)">');
  assert.equal(await statisticsPage.$$eval("#statistics-model-table img", nodes => nodes.length), 0);
  if (screenshotDir) await statisticsPage.screenshot({ path: join(screenshotDir, "statistics-ready.png"), fullPage: true });
  await statisticsPage.click("#statistics-content details summary");
  assert.equal(await statisticsPage.$$eval("#statistics-daily-table tbody tr", nodes => nodes.length), 7);
  assert.match(await statisticsPage.$eval("#statistics-daily-table", node => node.textContent), /Unavailable/);
  await statisticsPage.click('[data-days="1"]');
  await statisticsPage.waitForFunction(() => document.querySelector("#statistics-period").textContent.startsWith("Today"));
  assert.equal(await statisticsPage.$$eval(".daily-chart-row", nodes => nodes.length), 1);
  assert.match(await statisticsPage.$eval("#statistics-summary", node => node.textContent), /10,000/);
  await statisticsPage.click('[data-days="30"]');
  await statisticsPage.waitForFunction(() => document.querySelector("#statistics-period").textContent.startsWith("Last 30 days"));
  assert.equal(await statisticsPage.$$eval(".daily-chart-row", nodes => nodes.length), 30);
  statisticsMode = "hold";
  await statisticsPage.click('[data-days="7"]');
  await statisticsPage.waitForFunction(() => document.querySelector("#statistics-status").textContent.startsWith("Loading Last 7 days"));
  for (let attempt = 0; !heldRequest; attempt++) { assert(attempt < 30); await new Promise(resolve => setTimeout(resolve, 20)); }
  statisticsMode = "ready";
  await statisticsPage.click('[data-days="1"]');
  await statisticsPage.waitForFunction(() => document.querySelector("#statistics-period").textContent.startsWith("Today"));
  await respondStatistics(heldRequest, statisticsFixture(7));
  await statisticsPage.waitForNetworkIdle({ idleTime: 100 });
  assert.match(await statisticsPage.$eval("#statistics-period", node => node.textContent), /^Today/);
  assert.equal(await statisticsPage.$$eval(".daily-chart-row", nodes => nodes.length), 1, "late response changed the selected range");
  statisticsMode = "error";
  await statisticsPage.click('[data-days="30"]');
  await statisticsPage.waitForFunction(() => document.querySelector("#statistics-status").textContent.includes("stale"));
  assert.match(await statisticsPage.$eval("#statistics-period", node => node.textContent), /^Today/);
  assert.match(await statisticsPage.$eval("#statistics-status", node => node.textContent), /Could not refresh Last 30 days.*remains for Today/);
  statisticsMode = "disabled";
  await statisticsPage.click("#refresh-statistics");
  await statisticsPage.waitForFunction(() => document.querySelector("#statistics-status").textContent.includes("recording is disabled"));
  assert.equal(await statisticsPage.$eval("#statistics-content", node => node.hidden), true);
  assert(!await statisticsPage.$eval("#statistics-recording", node => node.textContent.includes("Retention: 0")));
  statisticsMode = "unavailable";
  await statisticsPage.click("#refresh-statistics");
  await statisticsPage.waitForFunction(() => document.querySelector("#statistics-status").textContent.includes("Statistics are unavailable"));
  statisticsMode = "empty";
  await statisticsPage.click("#refresh-statistics");
  await statisticsPage.waitForFunction(() => document.querySelector("#statistics-status").textContent.startsWith("No recorded requests"));
  statisticsMode = "zero";
  await statisticsPage.click("#refresh-statistics");
  await statisticsPage.waitForFunction(() => !document.querySelector("#statistics-content").hidden);
  assert.equal(await statisticsPage.$eval(".statistic-card:nth-child(2) .statistic-value", node => node.textContent), "0", "observed zero became unavailable");
  assert.equal(await statisticsPage.$eval(".daily-chart-row:last-child .daily-chart-bar.output", node => node.style.width), "0%", "observed zero rendered as a nonzero bar");
  assert.equal(await statisticsPage.$eval(".daily-chart-row:last-child .daily-chart-bar.output", node => node.classList.contains("missing")), false, "observed zero rendered as missing");
  assert.equal(await statisticsPage.$eval(".cache-card .statistic-value", node => node.textContent), "Unavailable", "missing cache observations became zero percent");
  assert.match(await statisticsPage.$eval(".cache-card", node => node.textContent), /Paired counts unavailable/);
  statisticsMode = "malformed";
  await statisticsPage.click("#refresh-statistics");
  await statisticsPage.waitForFunction(() => document.querySelector("#statistics-status").textContent.includes("invalid statistics"));
  assert.match(await statisticsPage.$eval("#statistics-status", node => node.textContent), /stale/);
  statisticsMode = "incomplete";
  await statisticsPage.click('[data-days="7"]');
  await statisticsPage.waitForFunction(() => !document.querySelector("#statistics-warning").hidden);
  assert.match(await statisticsPage.$eval("#statistics-warning", node => node.textContent), /2 dropped requests; 1 pending/);
  assert.equal(await statisticsPage.$eval(".statistic-card:nth-child(3) .statistic-value", node => node.textContent), "Unavailable");
  if (screenshotDir) await statisticsPage.screenshot({ path: join(screenshotDir, "statistics-desktop.png"), fullPage: true });
  await statisticsPage.setViewport({ width: 390, height: 844 });
  assert(await statisticsPage.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), "mobile statistics page overflows");
  if (screenshotDir) await statisticsPage.screenshot({ path: join(screenshotDir, "statistics-mobile.png"), fullPage: true });
  assert(statisticsReads.every(request => request.method === "GET" && request.url.startsWith(plain)), "statistics performed a mutation or contacted another origin");
  assert(!statisticsReads.some(request => /\/v1\/(chat|completions|responses)|\/backends/.test(request.url)), "statistics called inference or backend management");
  await statisticsPage.setRequestInterception(false);
  statisticsPage.removeAllListeners("request");
  await statisticsPage.goto(locked + "/#statistics");
  await statisticsPage.waitForFunction(() => document.querySelector("#connection-state").textContent === "Authentication required");
  assert.equal(await statisticsPage.$eval("#statistics-content", node => node.hidden), true);
  assert.match(await statisticsPage.$eval("#statistics-status", node => node.textContent), /--header @\/absolute\/path\/to\/private-header-file/);
  assert.equal(await statisticsPage.$$eval("input", nodes => nodes.filter(node => node.type === "password").length), 0);
  if (screenshotDir) await statisticsPage.screenshot({ path: join(screenshotDir, "statistics-auth.png"), fullPage: true });
  await statisticsPage.close();

  // A failed refresh retains a visibly stale snapshot instead of pretending
  // the inventory is empty. No background polling is used.
  await page.setRequestInterception(true);
  page.on("request", request => request.url().endsWith("/paracetamol/v1/ui") ? request.abort() : request.continue());
  await page.click("#refresh-connect");
  await page.waitForFunction(() => document.querySelector("#connection-state").textContent === "Snapshot unavailable");
  assert.match(await page.$eval("#notice", node => node.textContent), /stale/);
  await page.setRequestInterception(false);
  page.removeAllListeners("request");
  await page.goto(locked + "/#configuration");
  await page.waitForFunction(() => document.querySelector("#connection-state").textContent === "Authentication required");
  assert.equal(await page.$$eval(".model-row", nodes => nodes.length), 0);
  assert.equal(await page.$eval('[data-download="providers.toml"]', node => node.disabled), true);
  assert.match(await page.$eval("#setup-command", node => node.textContent), /--header @/);
  assert.equal(await page.$$eval('input[type="password"]', nodes => nodes.length), 0);
  if (screenshotDir) await page.screenshot({ path: join(screenshotDir, "authenticated.png"), fullPage: true });
  assert.deepEqual(errors, []);
  assert(requests.every(request => request.method === "GET"), "browser sent a mutation");
  assert(requests.every(request => request.url.startsWith(plain) || request.url.startsWith("blob:")), "browser contacted an external origin");
  console.log("Browser acceptance passed, including mobile, auth, previews, filtering, stale data and clipboard fallback.");
} finally {
  await browser.close();
}
