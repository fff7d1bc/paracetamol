import assert from "node:assert/strict";
import { chmodSync, linkSync, mkdirSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { createServer } from "node:http";
import { resolve, join } from "node:path";
import { test } from "node:test";

const runtime = process.env.PARACETAMOL_PI_RUNTIME;
assert.ok(runtime, "Set PI_RUNTIME to the absolute managed Pi runtime path when running make test-pi");
const require = createRequire(join(runtime, "package.json"));
const jiti = require("jiti").createJiti(import.meta.url, {
	fsCache: false, moduleCache: false,
	alias: {
		typebox: require.resolve("typebox"),
		"@earendil-works/pi-tui": require.resolve("@earendil-works/pi-tui"),
	},
});
const { convert } = require("html-to-text");
const web = await jiti.import(resolve("agent-clients/pi/extensions/paracetamol-web.ts"));
const { default: divider } = await jiti.import(resolve("agent-clients/pi/extensions/paracetamol-completion-divider.ts"));

function fakePi(flag = "off") {
	const handlers = new Map(), tools = new Map(), commands = new Map(), entries = [];
	return {
		handlers, tools, commands, entries,
		on(name, fn) { handlers.set(name, fn); },
		registerTool(tool) { tools.set(tool.name, tool); },
		registerCommand(name, command) { commands.set(name, command); },
		registerEntryRenderer() {}, registerFlag() {}, getFlag() { return flag; },
		appendEntry(type, data) { entries.push({ type, data }); },
	};
}
function fixture(t) {
	mkdirSync(resolve("build"), { recursive: true });
	const dir = mkdtempSync(resolve("build/pi-extension-tests-"));
	t.after(() => rmSync(dir, { recursive: true }));
	return { dir, key: join(dir, "brave-search.key") };
}
const textResponse = (text, type = "text/plain") => new Response(text, { headers: { "Content-Type": type } });
const parsed = (response) => JSON.parse(response.content[0].text);
const run = (pi, name, args, signal) => pi.tools.get(name).execute("test", args, signal);

test("search is absent without a key, fetch remains available", (t) => {
	const { key } = fixture(t), pi = fakePi();
	web.registerWebTools(pi, convert, key);
	assert.deepEqual([...pi.tools.keys()], ["web_fetch"]);
});

test("key must be private, regular, singly linked and not a symlink", (t) => {
	const { dir, key } = fixture(t);
	assert.equal(web.readSearchKey(key), undefined);
	writeFileSync(key, "test-search-key\n", { mode: 0o600 });
	assert.equal(web.readSearchKey(key), "test-search-key");
	chmodSync(key, 0o644);
	assert.throws(() => web.readSearchKey(key), /chmod 600/);
	chmodSync(key, 0o600);
	symlinkSync(key, join(dir, "link"));
	assert.throws(() => web.readSearchKey(join(dir, "link")), /symlink/);
	linkSync(key, join(dir, "hardlink"));
	assert.throws(() => web.readSearchKey(key), /regular file/);
	assert.throws(() => web.readSearchKey(dir), /regular file/);
});

test("search sends bounded parameters and a header key only to Brave", async (t) => {
	const { key } = fixture(t), pi = fakePi();
	writeFileSync(key, "test-search-key", { mode: 0o600 });
	web.registerWebTools(pi, convert, key, async (url, init) => {
		assert.equal(url.origin, "https://api.search.brave.com");
		assert.equal(url.searchParams.get("q"), "Go context cancellation");
		assert.equal(url.searchParams.get("count"), "5");
		assert.equal(url.searchParams.get("offset"), "2");
		assert.equal(init.headers["X-Subscription-Token"], "test-search-key");
		assert.equal(init.redirect, "error");
		return Response.json({ web: { results: Array(10).fill({ title: "T".repeat(400), url: "https://example.com", description: "D".repeat(1000) }) }, query: { more_results_available: true } });
	});
	const data = parsed(await run(pi, "web_search", { query: "Go context cancellation", page: 2 }));
	assert.equal(data.results.length, 5);
	assert.equal(data.results[0].title.length, 200);
	assert.equal(data.results[0].snippet.length, 600);
	assert.equal(data.next_page, 3);
	assert.ok(!JSON.stringify(data).includes("test-search-key"));
	await assert.rejects(run(pi, "web_search", { query: "a ".repeat(51) }), /50 words/);
});

test("search errors do not echo provider bodies or keys", async (t) => {
	const { key } = fixture(t), pi = fakePi();
	writeFileSync(key, "do-not-echo", { mode: 0o600 });
	web.registerWebTools(pi, convert, key, async () => new Response("do-not-echo", { status: 429 }));
	await assert.rejects(run(pi, "web_search", { query: "test" }), (error) => /429.*quota/.test(error.message) && !error.message.includes("do-not-echo"));
	web.registerWebTools(pi, convert, key, async () => textResponse("do-not-echo"));
	await assert.rejects(run(pi, "web_search", { query: "test" }), (error) => /invalid JSON/.test(error.message) && !error.message.includes("do-not-echo"));
	writeFileSync(key, "", { mode: 0o600 });
	await assert.rejects(run(pi, "web_search", { query: "test" }), /non-empty/);
});

test("HTML extraction preserves code and links, strips scripts and navigation", async (t) => {
	const { key } = fixture(t), pi = fakePi();
	web.registerWebTools(pi, convert, key, async (_url, init) => {
		assert.ok(!Object.hasOwn(init.headers, "X-Subscription-Token"));
		assert.ok(!Object.hasOwn(init.headers, "Cookie"));
		return textResponse('<html><head><title>Document title</title><style>hidden-style</style></head><body><nav>hidden-nav</nav><script>hidden-script</script><h1>Example &amp; test</h1><pre>if x:\n    print("π")</pre><a href="/guide">Guide</a><a href="../next">Next</a><footer>hidden-footer</footer></body></html>', "text/html");
	});
	const data = parsed(await run(pi, "web_fetch", { url: "https://example.com/doc" }));
	assert.match(data.text, /Example & test/i);
	assert.match(data.text, /Document title/);
	assert.match(data.text, /if x:\n    print\("π"\)/);
	assert.match(data.text, /https:\/\/example.com\/guide/);
	assert.match(data.text, /https:\/\/example.com\/next/);
	assert.doesNotMatch(data.text, /hidden-/);
	web.registerWebTools(pi, convert, key, async () => textResponse("<title>Fragment title</title><style>hidden-style</style><p>Body without a body tag.</p>", "text/html"));
	const fragment = parsed(await run(pi, "web_fetch", { url: "https://example.com/fragment" }));
	assert.match(fragment.text, /Fragment title/);
	assert.match(fragment.text, /Body without a body tag/);
	assert.doesNotMatch(fragment.text, /hidden-style/);
});

test("fetch pagination uses the same bounded snapshot, refreshes only at zero", async (t) => {
	const { key } = fixture(t), pi = fakePi();
	let calls = 0;
	web.registerWebTools(pi, convert, key, async () => { calls++; return textResponse("a".repeat(16_123)); });
	const first = parsed(await run(pi, "web_fetch", { url: "https://example.com/" }));
	const second = parsed(await run(pi, "web_fetch", { url: "https://example.com/", offset: first.next_offset }));
	const third = parsed(await run(pi, "web_fetch", { url: "https://example.com/", offset: second.next_offset }));
	assert.equal(first.text.length, 8000);
	assert.equal(second.text.length, 8000);
	assert.equal(third.text.length, 123);
	assert.equal(third.next_offset, null);
	assert.equal(calls, 1);
	await run(pi, "web_fetch", { url: "https://example.com/", offset: 0 });
	assert.equal(calls, 2);
	await assert.rejects(run(pi, "web_fetch", { url: "https://example.com/", offset: 20_000 }), /exceeds/);
	pi.handlers.get("session_start")({}, { mode: "print" });
	await assert.rejects(run(pi, "web_fetch", { url: "https://example.com/", offset: 8000 }), /expired/);
});

test("fetch reports clipping and limits snapshots to four pages", async (t) => {
	const { key } = fixture(t), pi = fakePi();
	web.registerWebTools(pi, convert, key, async () => textResponse("a".repeat(210_000)));
	for (let i = 0; i < 5; i++) {
		const data = parsed(await run(pi, "web_fetch", { url: `https://example.com/${i}` }));
		assert.equal(data.total_characters, 200_000);
		assert.equal(data.clipped_at_200000_characters, true);
	}
	await assert.rejects(run(pi, "web_fetch", { url: "https://example.com/0", offset: 8000 }), /expired/);
});

test("fetch rejects dangerous URLs and redirect protocols, handles relative redirects", async (t) => {
	const { key } = fixture(t), pi = fakePi();
	let calls = 0;
	web.registerWebTools(pi, convert, key, async (url) => {
		calls++;
		return url.endsWith("/new") ? textResponse("redirected") : new Response(null, { status: 302, headers: { location: "/new" } });
	});
	for (const url of ["file:///etc/passwd", "https://user:password@example.com/"]) {
		await assert.rejects(run(pi, "web_fetch", { url }), /HTTP\(S\)/);
	}
	assert.equal(calls, 0);
	assert.equal(parsed(await run(pi, "web_fetch", { url: "https://example.com/" })).url, "https://example.com/new");
	web.registerWebTools(pi, convert, key, async () => new Response(null, { status: 302, headers: { location: "file:///etc/passwd" } }));
	await assert.rejects(run(pi, "web_fetch", { url: "https://example.com/" }), /HTTP\(S\)/);
	web.registerWebTools(pi, convert, key, async () => new Response(null, { status: 302, headers: { location: "/loop" } }));
	await assert.rejects(run(pi, "web_fetch", { url: "https://example.com/" }), /too many redirects/);
});

test("fetch rejects unsupported media, HTTP errors, oversized and aborted responses", async (t) => {
	const { key } = fixture(t), pi = fakePi();
	for (const [response, error] of [
		[textResponse("%PDF", "application/pdf"), /content type/],
		[new Response("blocked", { status: 403 }), /HTTP 403/],
		[textResponse("a".repeat(2 * 1024 * 1024 + 1)), /2 MiB/],
		[new Response("", { headers: { "Content-Type": "text/plain", "Content-Length": "3000000" } }), /2 MiB/],
	]) {
		web.registerWebTools(pi, convert, key, async () => response);
		await assert.rejects(run(pi, "web_fetch", { url: "https://example.com/" }), error);
	}
	const controller = new AbortController();
	controller.abort();
	await assert.rejects(run(pi, "web_fetch", { url: "https://example.com/" }, controller.signal), /abort/i);
});

test("notifications wait for settled, once only, and never pollute non-TUI output", (t) => {
	const oldTTY = Object.getOwnPropertyDescriptor(process.stdout, "isTTY");
	let output = "";
	t.mock.method(process.stdout, "write", (value) => { output += value; return true; });
	Object.defineProperty(process.stdout, "isTTY", { configurable: true, value: true });
	t.after(() => oldTTY ? Object.defineProperty(process.stdout, "isTTY", oldTTY) : delete process.stdout.isTTY);
	for (const [flag, expected] of [["off", ""], ["bell", "\x07"], ["osc777", "\x1b]777;notify;Pi;Ready for input\x07"], ["osc99", "\x1b]99;"]]) {
		const pi = fakePi(flag), ctx = { mode: "tui", ui: { notify() {} } };
		divider(pi);
		pi.handlers.get("session_start")({}, ctx);
		pi.handlers.get("agent_settled")({}, ctx);
		assert.equal(pi.entries.length, 0);
		output = "";
		pi.handlers.get("agent_start")({}, ctx);
		assert.equal(output, "");
		assert.equal(pi.handlers.has("agent_end"), false);
		pi.handlers.get("agent_start")({}, ctx); // retry/queued continuation
		pi.handlers.get("agent_settled")({}, ctx);
		const once = output;
		assert.ok(output.startsWith(expected));
		if (!expected) assert.equal(output, "");
		pi.handlers.get("agent_settled")({}, ctx);
		assert.equal(output, once);
		assert.equal(pi.entries.length, 1);
		assert.ok(pi.entries[0].data.elapsedMilliseconds >= 0);
		for (const mode of ["print", "rpc"]) {
			output = "";
			pi.handlers.get("agent_start")({}, { ...ctx, mode });
			pi.handlers.get("agent_settled")({}, { ...ctx, mode });
			assert.equal(output, "");
		}
	}
	Object.defineProperty(process.stdout, "isTTY", { configurable: true, value: false });
	const pi = fakePi("bell"), ctx = { mode: "tui", ui: { notify() {} } };
	divider(pi);
	pi.handlers.get("session_start")({}, ctx);
	pi.handlers.get("agent_start")({}, ctx);
	output = "";
	pi.handlers.get("agent_settled")({}, ctx);
	assert.equal(output, "");
});

test("cancellation interrupts an in-flight HTTP response body", async (t) => {
	const { key } = fixture(t), pi = fakePi(), controller = new AbortController();
	const server = createServer((_request, response) => {
		response.writeHead(200, { "Content-Type": "text/plain" });
		response.write("partial body");
		setTimeout(() => controller.abort(), 25);
	});
	await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
	t.after(() => { server.closeAllConnections(); server.close(); });
	web.registerWebTools(pi, convert, key);
	await assert.rejects(run(pi, "web_fetch", { url: `http://127.0.0.1:${server.address().port}/` }, controller.signal), /abort/i);
});
