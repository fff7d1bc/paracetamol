import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import type { Component } from "@earendil-works/pi-tui";
import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";

const ENTRY_TYPE = "paracetamol-completion-divider";
const NOTIFICATIONS: Record<string, string> = {
	off: "",
	bell: "\x07",
	osc777: "\x1b]777;notify;Pi;Ready for input\x07",
	osc99: "\x1b]99;i=paracetamol:d=0;Pi\x1b\\\x1b]99;i=paracetamol:p=body;Ready for input\x1b\\",
};

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

export default function paracetamolCompletionDivider(pi: ExtensionAPI): void {
	let startedAt: number | undefined;
	let notification = "";
	pi.registerFlag("completion-notify", {
		type: "string",
		default: "off",
		description: "Terminal notification after Pi settles: off, bell, osc777, osc99",
	});

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
		// Only fixed terminal sequences, never model text or a project title.
		// Do not emit control bytes in print/RPC output or a redirected stream.
		if (notification && process.stdout.isTTY) process.stdout.write(notification);
	});

	pi.on("session_start", (_event, ctx) => {
		startedAt = undefined;
		const mode = pi.getFlag("completion-notify") ?? "off";
		notification = typeof mode === "string" && Object.hasOwn(NOTIFICATIONS, mode)
			? NOTIFICATIONS[mode] : "";
		if (ctx.mode === "tui" && !(typeof mode === "string" && Object.hasOwn(NOTIFICATIONS, mode))) {
			ctx.ui.notify("Invalid --completion-notify. Use off, bell, osc777 or osc99. Notifications disabled.", "warning");
		}
	});

	pi.on("session_shutdown", () => {
		startedAt = undefined;
	});
}
