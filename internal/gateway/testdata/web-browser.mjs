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
  for (const tab of ["configuration", "models", "overview"]) {
    await page.click("#tab-" + tab);
    await page.waitForFunction(id => !document.getElementById(id).hidden, {}, tab);
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), `mobile ${tab} page overflows`);
  }
  await page.goto(plain + "/#configuration");
  await page.waitForFunction(() => !document.querySelector("#configuration").hidden && document.querySelector("#connection-state").textContent === "Gateway reachable");

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
