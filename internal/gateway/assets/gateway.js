"use strict";

// No credential entry or browser storage. Every read stays on this origin,
// and no page action calls inference or backend-management endpoints.
const byID = (id) => document.getElementById(id);
const apiAddress = new URL("/v1", window.location.origin).href;
const exportRoot = "/paracetamol/v1/clients/maki/";
const tabs = ["overview", "configuration", "models", "statistics"];
let snapshot = null;
let busy = false;
let feedbackTimer;
let statisticsDays = 7;
let statisticsAttempted = false;
let statisticsSnapshot = null;
let statisticsSequence = 0;

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
  const name = tabs.includes(window.location.hash.slice(1)) ? window.location.hash.slice(1) : "overview";
  for (const tab of tabs) {
    const selected = tab === name;
    byID("tab-" + tab).setAttribute("aria-selected", String(selected));
    byID("tab-" + tab).tabIndex = selected ? 0 : -1;
    byID(tab).hidden = !selected;
  }
  if (name === "statistics" && !statisticsAttempted) refreshStatistics();
}

function setupCommands(authenticated) {
  const header = authenticated ? " --header @/absolute/path/to/private-header-file" : "";
  const download = (name) => `curl -fsS${header} ${shellQuote(exportURL(name).href)}`;
  byID("review-command").textContent = ["providers.toml", "init.lua"].map((name) => download(name)).join("\n");
  byID("setup-command").textContent = `(
  set -euC
  umask 077
  maki_config_dir="\${XDG_CONFIG_HOME:-$HOME/.config}/maki"
  [ ! -e "$HOME/.maki" ] || { echo 'Run maki migrate xdg first.'; exit 1; }
  for name in providers.toml init.lua; do
    [ ! -e "$maki_config_dir/$name" ] && [ ! -L "$maki_config_dir/$name" ] || { echo "Keep existing $name. Merge instead."; exit 1; }
  done
  providers=$(${download("providers.toml")})
  init=$(${download("init.lua")})
  mkdir -p "$maki_config_dir"
  printf '%s\\n' "$providers" > "$maki_config_dir/providers.toml"
  printf '%s\\n' "$init" > "$maki_config_dir/init.lua"
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
  if (!response.ok) {
    const error = new Error(`Gateway returned HTTP ${response.status}.`);
    error.httpStatus = response.status;
    throw error;
  }
  return response;
}

function setExportAvailable(available) {
  document.querySelectorAll("[data-preview], [data-download]").forEach((button) => { button.disabled = !available; });
}

function lockPage() {
  statisticsSequence++;
  statisticsSnapshot = null;
  statisticsAttempted = true;
  byID("refresh-statistics").disabled = false;
  byID("statistics-content").hidden = byID("statistics-metadata").hidden = byID("statistics-warning").hidden = true;
  byID("statistics-status").textContent = "Statistics are protected. Use authenticated curl with a private header file; this page has no browser login.";
  snapshot = null;
  setExportAvailable(false);
  byID("model-count").hidden = true;
  byID("model-groups").replaceChildren();
  byID("preview-content").textContent = "";
  byID("preview-panel").hidden = true;
  byID("default-model-note").textContent = "The exported configuration selects defaults from your authenticated model inventory.";
  byID("models-message").textContent = "Model inventory is protected. Use the authenticated curl example on Configuration to list models. This page has no browser login.";
  byID("connection-state").textContent = "Authentication required";
  byID("connection-state").className = "badge warn";
  byID("snapshot-time").textContent = "No public inventory";
  byID("notice").textContent = "This gateway requires a Bearer key. Setup instructions remain available, but model details and configuration downloads require authenticated curl.";
  byID("notice").hidden = false;
  setupCommands(true);
  statisticsAuthCommand();
}

function statisticsAuthCommand() {
  const panel = byID("statistics-status");
  panel.append(element("pre", "statistics-auth", `curl -fsS --header @/absolute/path/to/private-header-file ${shellQuote(new URL("/paracetamol/v1/statistics?days=" + statisticsDays, window.location.origin).href)}`));
}

const count = (value) => Number.isSafeInteger(value) && value >= 0;
const metricValid = (value) => value && count(value.total) && count(value.observations);
function usageValid(usage) {
  return usage && ["requests", "succeeded", "failed", "canceled"].every(key => count(usage[key])) &&
    ["input", "output", "cached", "reasoning"].every(key => metricValid(usage[key])) &&
    usage.cache_reuse && ["input_tokens", "cached_tokens", "observations"].every(key => count(usage.cache_reuse[key]));
}

function validStatistics(data, days) {
  const date = value => typeof value === "string" && /^\d{4}-\d{2}-\d{2}$/.test(value) && Number.isFinite(Date.parse(value));
  return data && data.schema === "paracetamol.statistics.v1" && ["ready", "disabled", "unavailable"].includes(data.state) &&
    data.period_days === days && count(data.retention_days) && date(data.from) && date(data.to) &&
    typeof data.collected_at === "string" && Number.isFinite(Date.parse(data.collected_at)) &&
    (!data.recording_since || date(data.recording_since)) &&
    (data.state !== "ready" || (typeof data.incomplete === "boolean" && count(data.dropped_requests) && count(data.pending_requests) &&
      usageValid(data.overall) && Array.isArray(data.daily) && data.daily.length === days && data.daily.every(day => date(day.date) && usageValid(day.usage)) &&
      Array.isArray(data.models) && data.models.every(model => typeof model.model === "string" && typeof model.application === "string" && usageValid(model.usage))));
}

function periodName(days) {
  return days === 1 ? "Today" : `Last ${days} days`;
}

function tokenTotal(metric) {
  return metric.observations ? metric.total.toLocaleString() : "Unavailable";
}

function observationLabel(observations, requests) {
  return `${observations.toLocaleString()} of ${requests.toLocaleString()} requests observed` + (observations < requests ? " · partial" : "");
}

function tokenCell(metric, requests) {
  const cell = element("td");
  cell.append(element("span", "", tokenTotal(metric)), element("small", "", observationLabel(metric.observations, requests)));
  return cell;
}

function cacheLabel(usage) {
  const cache = usage.cache_reuse;
  return cache.observations && cache.input_tokens > 0 ? (100 * cache.cached_tokens / cache.input_tokens).toLocaleString(undefined, { maximumFractionDigits: 1 }) + "%" : "Unavailable";
}

function usageTable(rows, daily) {
  const table = element("table", "usage-table");
  const head = element("thead");
  const labels = daily ? ["UTC date", "Requests", "Input tokens", "Output tokens", "Cached tokens"] : ["Model / application", "Requests / outcomes", "Input tokens", "Output tokens", "Cached tokens", "Cache reuse"];
  const header = element("tr");
  labels.forEach(label => { const cell = element("th", "", label); cell.scope = "col"; header.append(cell); });
  head.append(header);
  const body = element("tbody");
  for (const row of rows) {
    const usage = row.usage;
    const line = element("tr");
    const name = element("th");
    name.scope = "row";
    name.append(element(daily ? "span" : "code", "", daily ? row.date : row.model));
    if (!daily) name.append(element("small", "", row.application));
    const requests = element("td", "", usage.requests.toLocaleString());
    if (!daily) requests.append(element("small", "", `${usage.succeeded} succeeded · ${usage.failed} failed · ${usage.canceled} canceled`));
    line.append(name, requests, ...["input", "output", "cached"].map(key => tokenCell(usage[key], usage.requests)));
    if (!daily) {
      const cache = element("td", "", cacheLabel(usage));
      cache.append(element("small", "", observationLabel(usage.cache_reuse.observations, usage.requests)));
      line.append(cache);
    }
    body.append(line);
  }
  table.append(head, body);
  return table;
}

function renderStatistics(data) {
  byID("statistics-content").hidden = true;
  byID("statistics-warning").hidden = true;
  byID("statistics-metadata").hidden = false;
  byID("statistics-period").textContent = `${periodName(data.period_days)} · ${data.from} through ${data.to} (UTC, inclusive)`;
  byID("statistics-recording").textContent = data.state === "disabled" ? "Recording is disabled." : (data.recording_since ? `Recorded since ${data.recording_since} (UTC). ` : "Recording start unavailable. ") + `Retention: ${data.retention_days} days.`;
  byID("statistics-updated").textContent = (data.state === "ready" ? "Last updated " : "Last checked ") + new Date(data.collected_at).toISOString();
  if (data.state !== "ready") {
    byID("statistics-status").textContent = data.state === "disabled" ? "Statistics recording is disabled. No usage totals are available." : "Statistics storage is unavailable. No usage totals are available; refresh to try again.";
    return;
  }
  const usage = data.overall;
  byID("statistics-status").textContent = usage.requests ? "Recorded aggregates. Refresh to check for new usage." : "No recorded requests in this period. This does not include requests before recording began.";
  if (data.incomplete || data.dropped_requests || data.pending_requests) {
    byID("statistics-warning").hidden = false;
    byID("statistics-warning").textContent = `Recording is incomplete. ${data.dropped_requests.toLocaleString()} dropped requests; ${data.pending_requests.toLocaleString()} pending requests. This warning covers retained history, not just the selected period. An unclean stop can leave losses uncounted. Totals may omit usage, and refreshing may not recover missing records.`;
  }
  if (!usage.requests) return;
  const cards = document.createDocumentFragment();
  for (const [key, title] of [["input", "Input tokens"], ["output", "Output tokens"], ["cached", "Cached tokens"]]) {
    const card = element("article", "panel statistic-card");
    card.append(element("h2", "", title), element("strong", "statistic-value", tokenTotal(usage[key])), element("p", "muted", observationLabel(usage[key].observations, usage.requests)));
    cards.append(card);
  }
  const outcomes = element("article", "panel statistic-card outcomes-card");
  outcomes.append(element("h2", "", "Request outcomes"), element("strong", "statistic-value", usage.requests.toLocaleString()), element("p", "muted", `${usage.succeeded.toLocaleString()} succeeded · ${usage.failed.toLocaleString()} failed · ${usage.canceled.toLocaleString()} canceled`));
  const cache = element("article", "panel statistic-card cache-card");
  cache.append(element("h2", "", "Cache reuse"), element("strong", "statistic-value", cacheLabel(usage)), element("p", "muted", observationLabel(usage.cache_reuse.observations, usage.requests)), element("p", "muted", usage.cache_reuse.observations ? `Paired counts: ${usage.cache_reuse.cached_tokens.toLocaleString()} cached / ${usage.cache_reuse.input_tokens.toLocaleString()} input tokens.` : "Paired counts unavailable."));
  cards.append(outcomes, cache);
  byID("statistics-summary").replaceChildren(cards);
  const chart = document.createDocumentFragment();
  const maximum = Math.max(1, ...data.daily.flatMap(day => [day.usage.input.total, day.usage.output.total]));
  for (const day of data.daily) {
    const row = element("div", "daily-chart-row");
    row.append(element("span", "daily-chart-date", day.date.slice(5)));
    const bars = element("div", "daily-chart-bars");
    for (const key of ["input", "output"]) {
      const metric = day.usage[key];
      const track = element("div", "daily-chart-track");
      const bar = element("span", "daily-chart-bar " + key + (!metric.observations ? " missing" : ""));
      bar.style.width = metric.observations ? (metric.total / maximum * 100) + "%" : "100%";
      track.title = `${day.date} ${key}: ${tokenTotal(metric)}; ${observationLabel(metric.observations, day.usage.requests)}`;
      track.append(bar);
      bars.append(track);
    }
    row.append(bars);
    chart.append(row);
  }
  byID("statistics-chart").replaceChildren(chart);
  byID("statistics-daily-table").replaceChildren(usageTable(data.daily, true));
  byID("statistics-model-table").replaceChildren(usageTable(data.models, false));
  byID("statistics-content").hidden = false;
}

async function refreshStatistics() {
  if (byID("statistics").hidden) return;
  statisticsAttempted = true;
  // Range changes may overlap reads. Only the newest read may replace data;
  // retained data keeps its own period and timestamp even after a failure.
  const sequence = ++statisticsSequence;
  const days = statisticsDays;
  byID("refresh-statistics").disabled = true;
  byID("statistics-status").textContent = `Loading ${periodName(days)}…` + (statisticsSnapshot ? ` Previous data below is for ${periodName(statisticsSnapshot.period_days)} and has not been refreshed.` : "");
  try {
    const response = await readResponse("/paracetamol/v1/statistics?days=" + days);
    const data = await response.json();
    if (sequence !== statisticsSequence) return;
    if (!validStatistics(data, days)) throw new Error("The gateway returned invalid statistics.");
    statisticsSnapshot = data;
    renderStatistics(data);
  } catch (error) {
    if (sequence !== statisticsSequence) return;
    if (error.authentication) lockPage();
    else byID("statistics-status").textContent = (error.httpStatus === 503 ? `Statistics are unavailable for ${periodName(days)}. ` : `Could not refresh ${periodName(days)}. `) + error.message + (statisticsSnapshot ? ` Shown data is stale and remains for ${periodName(statisticsSnapshot.period_days)} (${statisticsSnapshot.from} through ${statisticsSnapshot.to}, UTC).` : " No statistics are available. Refresh to try again.");
  } finally {
    if (sequence === statisticsSequence) byID("refresh-statistics").disabled = false;
  }
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
byID("refresh-statistics").addEventListener("click", refreshStatistics);
document.querySelectorAll("[data-days]").forEach(button => button.addEventListener("click", () => {
  statisticsDays = Number(button.dataset.days);
  document.querySelectorAll("[data-days]").forEach(range => range.setAttribute("aria-pressed", String(range === button)));
  refreshStatistics();
}));
document.querySelector('[role="tablist"]').addEventListener("keydown", (event) => {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  const current = Math.max(0, tabs.indexOf(window.location.hash.slice(1)));
  const selected = event.key === "Home" ? tabs[0] : event.key === "End" ? tabs[tabs.length - 1] : tabs[(current + (event.key === "ArrowRight" ? 1 : tabs.length - 1)) % tabs.length];
  window.location.hash = selected;
  byID("tab-" + selected).focus();
});
window.addEventListener("hashchange", selectTab);
selectTab();
setupCommands(true);
refresh();
