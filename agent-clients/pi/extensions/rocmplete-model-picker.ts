import type {
	ExtensionAPI,
	ExtensionContext,
	Theme,
} from "@earendil-works/pi-coding-agent";
import { ThinkingSelectorComponent } from "@earendil-works/pi-coding-agent";
import type { Model, ThinkingLevel } from "@earendil-works/pi-ai";
import {
	Container,
	getKeybindings,
	Input,
	type Focusable,
	type KeybindingsManager,
	Spacer,
	Text,
	type TUI,
	truncateToWidth,
} from "@earendil-works/pi-tui";

const THINKING_LEVELS: ThinkingLevel[] = [
	"off",
	"minimal",
	"low",
	"medium",
	"high",
	"xhigh",
	"max",
];

const FAMILY_ORDER = [
	"Qwen 3.8",
	"Qwen 3.6",
	"Muse Glimmer",
	"KAT-Coder",
	"Gemma 4",
	"DeepSeek V4 Flash",
	"Other ROCmplete",
];

type AnyModel = Model<any>;

function sameModel(left: AnyModel | undefined, right: AnyModel): boolean {
	return left?.provider === right.provider && left.id === right.id;
}

function modelFamily(model: AnyModel): string {
	if (model.provider === "rocmplete") {
		if (model.id.startsWith("qwen3.8-")) return "Qwen 3.8";
		if (model.id.startsWith("qwen3.6-")) return "Qwen 3.6";
		if (model.id.startsWith("muse-glimmer-")) return "Muse Glimmer";
		if (model.id.startsWith("kat-coder-")) return "KAT-Coder";
		if (model.id.startsWith("gemma4-")) return "Gemma 4";
		return "Other ROCmplete";
	}
	if (model.provider === "dwarfstar") return "DeepSeek V4 Flash";
	return `Provider: ${model.provider}`;
}

function familyRank(family: string): [number, string] {
	const index = FAMILY_ORDER.indexOf(family);
	return index === -1 ? [FAMILY_ORDER.length, family] : [index, family];
}

function compareModels(left: AnyModel, right: AnyModel): number {
	const leftFamily = modelFamily(left);
	const rightFamily = modelFamily(right);
	const [leftRank] = familyRank(leftFamily);
	const [rightRank] = familyRank(rightFamily);
	if (leftRank !== rightRank) return leftRank - rightRank;
	if (leftFamily !== rightFamily) return leftFamily.localeCompare(rightFamily);
	return left.id.localeCompare(right.id);
}

function modelSearchText(model: AnyModel): string {
	return [modelFamily(model), model.provider, model.id, model.name]
		.filter(Boolean)
		.join(" ")
		.toLocaleLowerCase();
}

function contextLabel(tokens: number): string {
	if (tokens >= 1024 && tokens % 1024 === 0) return `${tokens / 1024}K ctx`;
	return `${tokens} ctx`;
}

function supportedThinkingLevels(model: AnyModel): ThinkingLevel[] {
	if (!model.reasoning) return ["off"];
	const mapping = model.thinkingLevelMap;
	return THINKING_LEVELS.filter((level) => {
		if (mapping && Object.prototype.hasOwnProperty.call(mapping, level)) {
			return mapping[level] !== null;
		}
		return level !== "xhigh" && level !== "max";
	});
}

function availableModels(ctx: ExtensionContext): AnyModel[] {
	const models =
		ctx.scopedModels.length > 0
			? ctx.scopedModels.map((entry) => entry.model)
			: ctx.modelRegistry.getAvailable();
	const unique = new Map<string, AnyModel>();
	for (const model of models) {
		unique.set(`${model.provider}\0${model.id}`, model);
	}
	return [...unique.values()].sort(compareModels);
}

class GroupedModelSelector implements Focusable {
	private readonly tui: TUI;
	private readonly theme: Theme;
	private readonly keybindings: KeybindingsManager;
	private readonly models: AnyModel[];
	private readonly currentModel: AnyModel | undefined;
	private readonly done: (model: AnyModel | undefined) => void;
	private readonly search = new Input();
	private filteredModels: AnyModel[];
	private selectedIndex = 0;
	private _focused = false;

	constructor(
		tui: TUI,
		theme: Theme,
		keybindings: KeybindingsManager,
		models: AnyModel[],
		currentModel: AnyModel | undefined,
		done: (model: AnyModel | undefined) => void,
	) {
		this.tui = tui;
		this.theme = theme;
		this.keybindings = keybindings;
		this.models = models;
		this.filteredModels = models;
		this.currentModel = currentModel;
		this.done = done;
		const currentIndex = models.findIndex((model) => sameModel(currentModel, model));
		this.selectedIndex = currentIndex === -1 ? 0 : currentIndex;
	}

	get focused(): boolean {
		return this._focused;
	}

	set focused(value: boolean) {
		this._focused = value;
		this.search.focused = value;
	}

	private updateFilter(): void {
		const tokens = this.search
			.getValue()
			.trim()
			.toLocaleLowerCase()
			.split(/\s+/)
			.filter(Boolean);
		this.filteredModels =
			tokens.length === 0
				? this.models
				: this.models.filter((model) => {
						const text = modelSearchText(model);
						return tokens.every((token) => text.includes(token));
					});
		const currentIndex = this.filteredModels.findIndex((model) =>
			sameModel(this.currentModel, model),
		);
		this.selectedIndex = currentIndex === -1 ? 0 : currentIndex;
	}

	private move(delta: number): void {
		const count = this.filteredModels.length;
		if (count === 0) return;
		this.selectedIndex = (this.selectedIndex + delta + count) % count;
	}

	handleInput(data: string): void {
		if (this.keybindings.matches(data, "tui.select.up")) {
			this.move(-1);
		} else if (this.keybindings.matches(data, "tui.select.down")) {
			this.move(1);
		} else if (this.keybindings.matches(data, "tui.select.pageUp")) {
			this.move(-8);
		} else if (this.keybindings.matches(data, "tui.select.pageDown")) {
			this.move(8);
		} else if (this.keybindings.matches(data, "tui.select.confirm")) {
			this.done(this.filteredModels[this.selectedIndex]);
			return;
		} else if (this.keybindings.matches(data, "tui.select.cancel")) {
			this.done(undefined);
			return;
		} else {
			const previous = this.search.getValue();
			this.search.handleInput(data);
			if (this.search.getValue() !== previous) this.updateFilter();
		}
		this.tui.requestRender();
	}

	render(width: number): string[] {
		const renderWidth = Math.max(24, width);
		const lines: string[] = [];
		lines.push(this.theme.fg("border", "─".repeat(renderWidth)));
		lines.push(this.theme.fg("accent", "Select a model"));
		lines.push("");
		lines.push(this.theme.fg("muted", "Search"));
		lines.push(...this.search.render(renderWidth));
		lines.push("");

		if (this.filteredModels.length === 0) {
			lines.push(this.theme.fg("muted", "  No matching models"));
		} else {
			const maxVisible = 12;
			const start = Math.max(
				0,
				Math.min(
					this.selectedIndex - Math.floor(maxVisible / 2),
					this.filteredModels.length - maxVisible,
				),
			);
			const end = Math.min(start + maxVisible, this.filteredModels.length);
			const counts = new Map<string, number>();
			for (const model of this.filteredModels) {
				const family = modelFamily(model);
				counts.set(family, (counts.get(family) ?? 0) + 1);
			}
			let renderedFamily: string | undefined;
			for (let index = start; index < end; index++) {
				const model = this.filteredModels[index]!;
				const family = modelFamily(model);
				if (family !== renderedFamily) {
					if (renderedFamily !== undefined) lines.push("");
					lines.push(
						this.theme.fg("accent", `${family} (${counts.get(family)})`),
					);
					renderedFamily = family;
				}
				const selected = index === this.selectedIndex;
				const current = sameModel(this.currentModel, model);
				const prefix = selected ? "→ " : "  ";
				const detail = `${contextLabel(model.contextWindow)}${current ? "  ✓ current" : ""}`;
				const detailWidth = detail.length + 3;
				const id = truncateToWidth(
					model.id,
					Math.max(8, renderWidth - detailWidth - 2),
					"…",
				);
				const gap = " ".repeat(
					Math.max(2, renderWidth - prefix.length - id.length - detail.length),
				);
				const line = `${prefix}${id}${gap}${detail}`;
				lines.push(selected ? this.theme.fg("accent", line) : line);
			}
			if (start > 0 || end < this.filteredModels.length) {
				lines.push(
					this.theme.fg(
						"muted",
						`  (${this.selectedIndex + 1}/${this.filteredModels.length})`,
					),
				);
			}
		}

		lines.push("");
		lines.push(
			this.theme.fg(
				"muted",
				"↑↓ navigate · type to search · Enter select · Esc cancel",
			),
		);
		lines.push(this.theme.fg("border", "─".repeat(renderWidth)));
		return lines;
	}

	invalidate(): void {
		this.search.invalidate();
	}

	dispose(): void {}
}

class ThinkingPicker extends Container implements Focusable {
	private readonly selector: ThinkingSelectorComponent;
	private _focused = false;

	constructor(
		model: AnyModel,
		theme: Theme,
		currentLevel: ThinkingLevel,
		availableLevels: ThinkingLevel[],
		onSelect: (level: ThinkingLevel) => void,
		onCancel: () => void,
	) {
		super();
		this.addChild(new Text(theme.fg("accent", `Reasoning for ${model.id}`), 0, 0));
		this.addChild(new Spacer(1));
		this.selector = new ThinkingSelectorComponent(
			currentLevel,
			availableLevels,
			onSelect,
			onCancel,
		);
		this.addChild(this.selector);
	}

	get focused(): boolean {
		return this._focused;
	}

	set focused(value: boolean) {
		this._focused = value;
	}

	handleInput(data: string): void {
		this.selector.getSelectList().handleInput(data);
	}
}

async function promptForThinking(
	pi: ExtensionAPI,
	ctx: ExtensionContext,
	model: AnyModel,
): Promise<void> {
	const levels = supportedThinkingLevels(model);
	if (ctx.mode !== "tui" || levels.length < 2) return;
	const current = pi.getThinkingLevel();
	const selected = await ctx.ui.custom<ThinkingLevel | undefined>(
		(_tui, theme, _keybindings, done) =>
			new ThinkingPicker(
				model,
				theme,
				current,
				levels,
				done,
				() => done(undefined),
			),
	);
	if (selected !== undefined) pi.setThinkingLevel(selected);
}

async function promptForModel(
	pi: ExtensionAPI,
	ctx: ExtensionContext,
): Promise<void> {
	if (ctx.mode !== "tui") {
		ctx.ui.notify("Model selection is available in interactive mode", "warning");
		return;
	}
	const models = availableModels(ctx);
	if (models.length === 0) {
		ctx.ui.notify("No configured models are available", "warning");
		return;
	}
	const selected = await ctx.ui.custom<AnyModel | undefined>(
		(tui, theme, keybindings, done) =>
			new GroupedModelSelector(
				tui,
				theme,
				keybindings,
				models,
				ctx.model,
				done,
			),
	);
	if (selected === undefined) return;
	if (!(await pi.setModel(selected))) {
		ctx.ui.notify(`Model ${selected.provider}/${selected.id} is unavailable`, "error");
		return;
	}
	await promptForThinking(pi, ctx, selected);
}

export default function rocmpleteModelPicker(pi: ExtensionAPI): void {
	let selectionActive = false;
	let unsubscribeTerminal: (() => void) | undefined;

	const runModelPicker = async (ctx: ExtensionContext): Promise<void> => {
		if (selectionActive) return;
		selectionActive = true;
		try {
			await promptForModel(pi, ctx);
		} catch (error) {
			ctx.ui.notify(
				`ROCmplete model picker failed: ${error instanceof Error ? error.message : String(error)}`,
				"error",
			);
		} finally {
			selectionActive = false;
		}
	};

	pi.registerCommand("select-model", {
		description: "Select a ROCmplete model by family, then choose reasoning",
		handler: async (_arguments, ctx) => runModelPicker(ctx),
	});

	pi.on("session_start", (_event, ctx) => {
		if (ctx.mode !== "tui") return;
		unsubscribeTerminal?.();
		unsubscribeTerminal = ctx.ui.onTerminalInput((data) => {
			if (selectionActive) return undefined;
			const keybindings = getKeybindings();
			const modelShortcut = keybindings.matches(data, "app.model.select");
			const bareModelCommand =
				keybindings.matches(data, "tui.input.submit") &&
				ctx.ui.getEditorText().trim() === "/model";
			if (!modelShortcut && !bareModelCommand) return undefined;
			if (bareModelCommand) ctx.ui.setEditorText("");
			void runModelPicker(ctx);
			return { consume: true };
		});
	});

	pi.on("model_select", async (event, ctx) => {
		if (
			ctx.mode !== "tui" ||
			selectionActive ||
			event.source !== "set"
		) {
			return;
		}
		selectionActive = true;
		try {
			await promptForThinking(pi, ctx, event.model);
		} finally {
			selectionActive = false;
		}
	});

	pi.on("session_shutdown", () => {
		unsubscribeTerminal?.();
		unsubscribeTerminal = undefined;
	});
}
