import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { test } from "node:test";

const runtime = process.env.PARACETAMOL_PI_RUNTIME;
assert.ok(runtime, "Set PI_RUNTIME to the absolute managed Pi runtime path");
const require = createRequire(join(runtime, "package.json"));
// The agent exports an import-only entry, so require.resolve cannot select it.
const agentRoot = join(runtime, "node_modules/@earendil-works/pi-coding-agent");
const agentEntry = join(agentRoot, JSON.parse(readFileSync(join(agentRoot, "package.json"))).exports["."].import);
const agent = await import(agentEntry);
const tui = await import(require.resolve("@earendil-works/pi-tui"));
agent.initTheme("dark", false);
const jiti = require("jiti").createJiti(import.meta.url, {
	fsCache: false, moduleCache: false,
	alias: {
		"@earendil-works/pi-coding-agent": agentEntry,
		"@earendil-works/pi-tui": require.resolve("@earendil-works/pi-tui"),
	},
});
const { default: picker } = await jiti.import(resolve("agent-clients/pi/extensions/paracetamol-model-picker.ts"));
const model = (id, levels) => ({
	provider: "paracetamol", id, name: id, contextWindow: 262144,
	reasoning: levels.length > 1,
	thinkingLevelMap: Object.fromEntries(["off", "minimal", "low", "medium", "high", "xhigh", "max"]
		.map((level) => [level, levels.includes(level) ? level : null])),
});
const dense = model("unsloth-qwen3.8-27b-mtp-ud-q8-k-xl", ["off", "low", "medium", "xhigh"]);
const huihui = model("huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl", ["off", "low", "medium", "xhigh"]);
const dwarfstar = model("antirez-deepseek-v4-flash-0731-q2-imatrix", ["off", "high"]);
const muse = model("meta-models-muse-glimmer-30b", ["low", "medium", "high", "xhigh"]);

function fixture({ models = [muse, dwarfstar, huihui, dense, dense], actions = [], mode = "tui" } = {}) {
	const handlers = new Map(), commands = new Map(), notices = [], screens = [], selections = [];
	let level = "medium", hooks = 0;
	const pi = {
		on(name, fn) { handlers.set(name, fn); },
		registerCommand(name, spec) { commands.set(name, spec); },
		async setModel(value) { selections.push(value); return true; },
		getThinkingLevel() { return level; }, setThinkingLevel(value) { level = value; },
	};
	const ctx = {
		mode, model: dense, scopedModels: [], modelRegistry: { getAvailable: () => models },
		ui: {
			notify: (message) => notices.push(message),
			onTerminalInput() { hooks++; return () => { hooks--; }; },
			async custom(factory) {
				assert.ok(actions.length, "unexpected picker");
				return new Promise((done) => {
					const component = factory({ requestRender() {} }, { fg: (_color, text) => text }, tui.getKeybindings(), done);
					component.focused = true;
					screens.push(component.render(120).join("\n"));
					actions.shift()(component);
				});
			},
		},
	};
	picker(pi);
	return { pi, ctx, handlers, commands, notices, screens, selections, get level() { return level; }, get hooks() { return hooks; } };
}

test("model picker groups and deduplicates models, then searches native reasoning choices", async () => {
	const f = fixture({ actions: [
		(component) => component.handleInput("\r"),
		(component) => {
			component.handleInput("xhigh");
			const filtered = component.render(120).join("\n");
			assert.match(filtered, /xhigh/);
			assert.doesNotMatch(filtered, /No reasoning|Moderate reasoning/);
			component.handleInput("\r");
		},
	] });
	await f.commands.get("select-model").handler("", f.ctx);
	assert.deepEqual(f.notices, []);
	assert.deepEqual(f.selections, [dense]);
	assert.equal(f.level, "xhigh");
	const grouped = f.screens[0];
	assert.match(grouped, /Qwen 3\.8 \(2\)/);
	assert.match(grouped, /huihui-ai-qwen3\.8-/);
	assert.ok(grouped.indexOf("Qwen 3.8") < grouped.indexOf("Muse Glimmer"));
	assert.ok(grouped.indexOf("Muse Glimmer") < grouped.indexOf("DeepSeek V4 Flash"));
	assert.doesNotMatch(grouped, /Recent/);
	assert.match(f.screens[1], /No reasoning/);
	assert.doesNotMatch(f.screens[1], /Very brief reasoning|Deep reasoning|Maximum reasoning/);
});

test("reasoning choices follow each model and cancellation preserves the level", async () => {
	for (const selected of [dwarfstar, muse]) {
		const f = fixture({ actions: [(component) => component.handleInput("\x1b")] });
		await f.handlers.get("model_select")({ source: "set", model: selected }, f.ctx);
		assert.equal(f.level, "medium");
		assert.equal(f.screens.length, 1);
		if (selected === dwarfstar) {
			assert.match(f.screens[0], /No reasoning/);
			assert.match(f.screens[0], /Deep reasoning/);
			assert.doesNotMatch(f.screens[0], /Moderate reasoning|Extra-high reasoning/);
		} else {
			assert.doesNotMatch(f.screens[0], /No reasoning/);
			assert.match(f.screens[0], /Extra-high reasoning/);
		}
	}
});

test("model cancellation, cycling, restoration and non-TUI modes do not open extra pickers", async () => {
	const f = fixture({ actions: [(component) => component.handleInput("\x1b")] });
	await f.commands.get("select-model").handler("", f.ctx);
	assert.equal(f.selections.length, 0);
	for (const source of ["cycle", "restore"]) {
		await f.handlers.get("model_select")({ source, model: dense }, f.ctx);
	}
	assert.equal(f.screens.length, 1);
	for (const mode of ["print", "rpc"]) {
		const quiet = fixture({ mode });
		quiet.handlers.get("session_start")({}, quiet.ctx);
		await quiet.handlers.get("model_select")({ source: "set", model: dense }, quiet.ctx);
		assert.equal(quiet.hooks, 0);
		assert.equal(quiet.screens.length, 0);
	}
	f.handlers.get("session_start")({}, f.ctx);
	assert.equal(f.hooks, 1);
	f.handlers.get("session_start")({}, f.ctx);
	assert.equal(f.hooks, 1);
	f.handlers.get("session_shutdown")();
	assert.equal(f.hooks, 0);
});
