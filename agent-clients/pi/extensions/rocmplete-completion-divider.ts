import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import type { Component } from "@earendil-works/pi-tui";
import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";

const ENTRY_TYPE = "rocmplete-completion-divider";

interface CompletionData {
	elapsedMilliseconds: number;
}

function formatDuration(elapsedMilliseconds: number): string {
	const totalSeconds = Math.max(0, Math.round(elapsedMilliseconds / 1000));
	const hours = Math.floor(totalSeconds / 3600);
	const minutes = Math.floor((totalSeconds % 3600) / 60);
	const seconds = totalSeconds % 60;
	const parts: string[] = [];
	if (hours > 0) parts.push(`${hours}h`);
	if (minutes > 0) parts.push(`${minutes}m`);
	if (seconds > 0 || parts.length === 0) parts.push(`${seconds}s`);
	return parts.join(" ");
}

class CompletionDivider implements Component {
	constructor(
		private readonly label: string,
		private readonly border: (text: string) => string,
		private readonly text: (text: string) => string,
	) {}

	render(width: number): string[] {
		if (width <= 0) return [];
		const prefix = "─ ";
		const suffix = " ";
		const available = Math.max(
			0,
			width - visibleWidth(prefix) - visibleWidth(suffix),
		);
		const label = truncateToWidth(this.label, available, "…");
		const contentWidth =
			visibleWidth(prefix) + visibleWidth(label) + visibleWidth(suffix);
		const fill = "─".repeat(Math.max(0, width - contentWidth));
		return [
			`${this.border(prefix)}${this.text(label)}${this.border(`${suffix}${fill}`)}`,
		];
	}

	invalidate(): void {}
}

export default function rocmpleteCompletionDivider(pi: ExtensionAPI): void {
	let startedAt: number | undefined;

	pi.registerEntryRenderer<CompletionData>(
		ENTRY_TYPE,
		(entry, _options, theme) => {
			const elapsed = entry.data?.elapsedMilliseconds;
			if (
				typeof elapsed !== "number" ||
				!Number.isFinite(elapsed) ||
				elapsed < 0
			) {
				return undefined;
			}
			return new CompletionDivider(
				`Worked for ${formatDuration(elapsed)}`,
				(text) => theme.fg("border", text),
				(text) => theme.fg("muted", text),
			);
		},
	);

	pi.on("agent_start", (_event, ctx) => {
		if (ctx.mode === "tui" && startedAt === undefined) {
			startedAt = performance.now();
		}
	});

	pi.on("agent_settled", (_event, ctx) => {
		if (ctx.mode !== "tui" || startedAt === undefined) return;
		const elapsedMilliseconds = performance.now() - startedAt;
		startedAt = undefined;
		pi.appendEntry<CompletionData>(ENTRY_TYPE, { elapsedMilliseconds });
	});

	pi.on("session_start", () => {
		startedAt = undefined;
	});

	pi.on("session_shutdown", () => {
		startedAt = undefined;
	});
}
