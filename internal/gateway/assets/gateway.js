"use strict";

// No credential entry or browser storage. Every read stays on this origin,
// and no page action calls inference or backend-management endpoints.
const byID = (id) => document.getElementById(id);
const apiAddress = new URL("/v1", window.location.origin).href;
const exportRoot = "/paracetamol/v1/clients/maki/";
let snapshot = null;
let busy = false;
let feedbackTimer;

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

// These strings are copyable shell source, not HTML. Quote even URLs/model IDs
// rather than relying on the usual hostname alphabet or our present catalog.
function shellQuote(value) {
  return "'" + value.replaceAll("'", "'\"'\"'") + "'";
}

function exportURL(name) {
  const url = new URL(exportRoot + name, window.location.origin);
  url.searchParams.set("base_url", apiAddress);
  return url;
}

function showFeedback(message) {
  clearTimeout(feedbackTimer);
  byID("feedback").textContent = message;
  byID("feedback").hidden = false;
  feedbackTimer = setTimeout(() => { byID("feedback").hidden = true; }, 5000);
}

async function copyText(text, target) {
  try {
    if (!navigator.clipboard?.writeText) throw new Error("Clipboard unavailable");
    await navigator.clipboard.writeText(text);
    showFeedback("Copied to clipboard");
    return;
  } catch { /* Plain LAN HTTP often has no Clipboard API. */ }
  const field = element("textarea");
  field.value = text;
  field.setAttribute("aria-label", "Text to copy");
  document.body.append(field);
  field.select();
  let copied = false;
  try { copied = document.execCommand("copy"); } catch { /* Manual fallback below. */ }
  field.remove();
  if (copied) {
    showFeedback("Copied to clipboard");
    target?.focus();
  } else {
    if (target) {
      const range = document.createRange();
      range.selectNodeContents(target);
      const selection = window.getSelection();
      selection.removeAllRanges();
      selection.addRange(range);
      target.focus();
    }
    showFeedback("Clipboard unavailable. Select the text and use your browser’s Copy command.");
  }
}

function selectTab() {
  const name = window.location.hash === "#models" ? "models" : "connect";
  for (const tab of ["connect", "models"]) {
    const selected = tab === name;
    byID("tab-" + tab).setAttribute("aria-selected", String(selected));
    byID("tab-" + tab).tabIndex = selected ? 0 : -1;
    byID(tab).hidden = !selected;
  }
}

function setupCommands(authenticated) {
  const header = authenticated ? " --header @/absolute/path/to/private-header-file" : "";
  const downloads = ["providers.toml", "init.lua"].map((name) =>
    `curl -fsS${header} ${shellQuote(exportURL(name).href)} \\\n  -o "$maki_setup_dir/${name}"`).join("\n");
  byID("review-command").textContent = `maki_setup_dir=$(mktemp -d)\n${downloads}\nprintf 'Downloaded to %s\\n' "$maki_setup_dir"`;
  byID("setup-command").textContent = `(
  set -eu
  maki_config_dir="$HOME/.config/maki"
  if [ -e "$HOME/.maki" ] || [ -n "\${XDG_CONFIG_HOME:-}" ]; then
    printf '%s\\n' 'Existing or custom layout. Use the merge instructions below.'
    exit 1
  fi
  for name in providers.toml init.lua; do
    if [ -e "$maki_config_dir/$name" ] || [ -L "$maki_config_dir/$name" ]; then
      printf 'Keep existing %s. Use the merge instructions below.\\n' "$name"
      exit 1
    fi
  done
  maki_setup_dir=$(mktemp -d)
${downloads.split("\n").map((line) => "  " + line).join("\n")}
  mkdir -p "$maki_config_dir"
  umask 077
  set -C
  cat "$maki_setup_dir/providers.toml" > "$maki_config_dir/providers.toml"
  cat "$maki_setup_dir/init.lua" > "$maki_config_dir/init.lua"
)`;
  byID("curl-command").textContent = `curl -fsS${header} ${shellQuote(new URL("/v1/models", window.location.origin).href)}`;
  byID("api-key-note").textContent = authenticated
    ? "Use a private header file containing Authorization: Bearer YOUR_KEY for these curl commands. Replace the placeholder path and keep the file readable only by you. For Maki, supply PARACETAMOL_API_KEY or use its native credential prompt. This page never accepts or stores your key."
    : "This gateway has no authentication. If a client requires an API key, use a dummy value such as paracetamol-local. The exported Maki provider already includes one.";
}

async function readResponse(path) {
  const response = await fetch(path, { credentials: "omit", redirect: "error", cache: "no-store", signal: AbortSignal.timeout(12000) });
  if (response.headers.get("X-Paracetamol-Gateway") !== "paracetamol.gateway.v1") {
    throw new Error("This address did not return a Paracetamol gateway response.");
  }
  if (response.status === 401) {
    const error = new Error("Authentication required");
    error.authentication = true;
    throw error;
  }
  if (!response.ok) throw new Error(`Gateway returned HTTP ${response.status}.`);
  return response;
}

function setExportAvailable(available) {
  document.querySelectorAll("[data-preview], [data-download]").forEach((button) => { button.disabled = !available; });
}

function lockPage() {
  snapshot = null;
  setExportAvailable(false);
  byID("model-count").hidden = true;
  byID("model-groups").replaceChildren();
  byID("preview-content").textContent = "";
  byID("preview-panel").hidden = true;
  byID("default-model-note").textContent = "The exported configuration selects defaults from your authenticated model inventory.";
  byID("models-message").textContent = "Model inventory is protected. Use the authenticated curl example on Connect to list models. This page has no browser login.";
  byID("connection-state").textContent = "Authentication required";
  byID("connection-state").className = "badge warn";
  byID("snapshot-time").textContent = "No public inventory";
  byID("notice").textContent = "This gateway requires a Bearer key. Setup instructions remain available, but model details and configuration downloads require authenticated curl.";
  byID("notice").hidden = false;
  setupCommands(true);
}

function validSnapshot(data) {
  return data && typeof data.collected_at === "string" && Number.isFinite(Date.parse(data.collected_at)) &&
    typeof data.gateway === "string" && Array.isArray(data.models) && data.models.every((model) =>
      model && ["id", "name", "family", "publisher", "application", "state"].every((key) => typeof model[key] === "string") &&
      Number.isFinite(model.context) && Number.isFinite(model.max_output_tokens) && Number.isInteger(model.request_slots) && model.request_slots > 0 && typeof model.agent_tools === "boolean" &&
      (!model.reasoning_levels || (Array.isArray(model.reasoning_levels) && model.reasoning_levels.every((level) => typeof level === "string"))));
}

async function refresh() {
  if (busy) return;
  busy = true;
  byID("refresh").disabled = byID("refresh-connect").disabled = true;
  byID("connection-state").textContent = "Checking connection";
  try {
    const response = await readResponse("/paracetamol/v1/ui");
    const data = await response.json();
    if (!validSnapshot(data)) throw new Error("The gateway returned an invalid model snapshot.");
    snapshot = data;
    byID("preview-panel").hidden = true;
    byID("preview-content").textContent = "";
    byID("connection-state").textContent = data.gateway === "ready" ? "Gateway reachable" : "Gateway shutting down";
    byID("connection-state").className = data.gateway === "ready" ? "badge good" : "badge warn";
    byID("snapshot-time").textContent = "Checked " + new Date(data.collected_at).toISOString();
    byID("notice").hidden = true;
    byID("model-count").textContent = String(data.models.length);
    byID("model-count").hidden = false;
    const preferred = data.models.find((model) => model.id === data.default_model && model.agent_tools);
    setExportAvailable(Boolean(preferred));
    byID("default-model-note").textContent = preferred
      ? `Maki starts with ${preferred.id}. Adaptive thinking selects ${preferred.maki_reasoning_default || "the model default"}.`
      : "No coding-capable model is available in this snapshot. Maki exports are unavailable, but other models can still be used through the API.";
    setupCommands(false);
    renderModels();
  } catch (error) {
    if (error.authentication) {
      lockPage();
    } else {
      setExportAvailable(false);
      byID("connection-state").textContent = "Snapshot unavailable";
      byID("connection-state").className = "badge warn";
      byID("notice").textContent = "Could not refresh the gateway snapshot. " + error.message + (snapshot ? " Shown model data is stale. Refresh to try again." : " Check the gateway address and try Refresh connection.");
      byID("notice").hidden = false;
      if (!snapshot) byID("models-message").textContent = "No model snapshot available.";
    }
  } finally {
    busy = false;
    byID("refresh").disabled = byID("refresh-connect").disabled = false;
  }
}

function contextLabel(value) {
  return value % 1024 === 0 ? `${value / 1024}K context` : `${value.toLocaleString()} context`;
}

function renderModels() {
  if (!snapshot) return;
  const expanded = new Set([...document.querySelectorAll(".model-row details[open]")].map((node) => node.dataset.model));
  const query = byID("model-search").value.trim().toLowerCase();
  const visible = snapshot.models.filter((model) => [model.id, model.name, model.family, model.publisher].some((value) => value.toLowerCase().includes(query)));
  const groups = new Map();
  for (const model of visible) {
    if (!groups.has(model.family)) groups.set(model.family, []);
    groups.get(model.family).push(model);
  }
  const preferredFamily = snapshot.models.find((model) => model.id === snapshot.default_model)?.family;
  const families = [...groups.keys()].sort((a, b) => a === b ? 0 : a === preferredFamily ? -1 : b === preferredFamily ? 1 : a === "Other models" ? 1 : b === "Other models" ? -1 : a.localeCompare(b));
  const fragment = document.createDocumentFragment();
  for (const family of families) {
    const section = element("section", "model-family");
    section.append(element("h2", "", family));
    for (const model of groups.get(family)) {
      const row = element("article", "model-row");
      const top = element("div", "model-top");
      const identity = element("div", "model-identity");
      const exactID = element("code", "model-id", model.id);
      exactID.tabIndex = 0;
      identity.append(element("h3", "model-name", model.name), exactID);
      const copy = element("button", "text-button", "Copy ID");
      copy.type = "button";
      copy.setAttribute("aria-label", "Copy model ID " + model.id);
      copy.addEventListener("click", () => copyText(model.id, exactID));
      top.append(identity, copy);
      const badges = element("div", "model-badges");
      const stateClass = model.state === "loaded" ? " good" : ["failed", "unknown", "loading"].includes(model.state) ? " warn" : "";
      badges.append(element("span", "badge", model.publisher), element("span", "badge", contextLabel(model.context)), element("span", "badge" + stateClass, model.state === "unloaded" ? "Available · not loaded" : model.state));
      if (model.id === snapshot.default_model) badges.append(element("span", "badge good", "Maki default"));
      const details = element("details");
      details.dataset.model = model.id;
      details.open = expanded.has(model.id);
      details.append(element("summary", "", "Reasoning and model details"));
      let levels = model.reasoning_control === "toggle" ? ["on"] : [...(model.reasoning_levels || [])];
      if (model.reasoning_off) levels.unshift("off");
      const definitions = [
        ["Application", model.application],
        ["Coding-client configuration", model.agent_tools ? "Included in Maki export" : "Not included in Maki export"],
        ["Supported reasoning", levels.length ? levels.join(" · ") : "No declared reasoning control"],
        ["HTTP reasoning when omitted", model.reasoning_default || (model.reasoning_control === "toggle" ? "on" : "Backend default")],
        ["Maki adaptive reasoning", model.agent_tools ? model.maki_reasoning_default || "No declared reasoning control" : "Not applicable"],
        ["Configured request slots", String(model.request_slots)],
        ["Configured context capacity", model.context.toLocaleString() + " tokens" + (model.application === "llama-cpp" && model.request_slots > 1 ? " shared across slots" : "")],
        ["Runtime context capacity", model.runtime?.context ? model.runtime.context.toLocaleString() + " tokens" : "Unavailable until reported by the backend"],
        ["Client output limit", model.max_output_tokens.toLocaleString() + " tokens"],
      ];
      const grid = element("dl", "model-detail-grid");
      for (const [label, value] of definitions) {
        const item = element("div");
        item.append(element("dt", "", label), element("dd", "", value));
        grid.append(item);
      }
      details.append(grid);
      row.append(top, badges, details);
      section.append(row);
    }
    fragment.append(section);
  }
  byID("model-groups").replaceChildren(fragment);
  byID("models-message").textContent = visible.length ? `${visible.length} of ${snapshot.models.length} available models. Snapshot refresh is manual.` : "No models match your search.";
}

async function configuration(name, preview) {
  try {
    const response = await readResponse(exportURL(name));
    const text = await response.text();
    if (preview) {
      byID("preview-title").textContent = name;
      byID("preview-content").textContent = text;
      byID("preview-panel").hidden = false;
      byID("preview-content").focus();
    } else {
      const url = URL.createObjectURL(new Blob([text], { type: "text/plain;charset=utf-8" }));
      const link = element("a");
      link.href = url;
      link.download = name;
      document.body.append(link);
      link.click();
      link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      showFeedback("Download requested. Review " + name + " before installing it.");
    }
  } catch (error) {
    if (error.authentication) lockPage();
    showFeedback("Configuration unavailable. " + error.message);
  }
}

byID("api-address").textContent = apiAddress;
document.querySelectorAll("[data-copy]").forEach((button) => button.addEventListener("click", () => {
  const target = byID(button.dataset.copy);
  copyText(target.textContent, target);
}));
document.querySelectorAll("[data-preview]").forEach((button) => button.addEventListener("click", () => configuration(button.dataset.preview, true)));
document.querySelectorAll("[data-download]").forEach((button) => button.addEventListener("click", () => configuration(button.dataset.download, false)));
byID("close-preview").addEventListener("click", () => { byID("preview-panel").hidden = true; });
byID("model-search").addEventListener("input", renderModels);
byID("refresh").addEventListener("click", refresh);
byID("refresh-connect").addEventListener("click", refresh);
document.querySelector('[role="tablist"]').addEventListener("keydown", (event) => {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  const selected = event.key === "Home" ? "connect" : event.key === "End" ? "models" : window.location.hash === "#models" ? "connect" : "models";
  window.location.hash = selected;
  byID("tab-" + selected).focus();
});
window.addEventListener("hashchange", selectTab);
selectTab();
setupCommands(true);
refresh();
