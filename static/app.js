import { clamp, escapeHTML, sameSet, copyToClipboard } from "./app_utils.mjs";
import { refreshCustomSelects } from "./custom_select.mjs";
import { createTableModel, createTextModel, createBrowseModel, scheduleVirtualRender, virtualHeight, adjustViewportForScrollbar } from "./output_view.mjs";
import { normalizePayload } from "./virtual_output.mjs";
import { connectionGroups, mergeConnections } from "./preconfigured_connections.mjs";
import { loadCollapsedProfiles, setProfileCollapsed } from "./profile_state.mjs";

const tools = Array.isArray(window.ORBY_TOOLS) ? window.ORBY_TOOLS : [];
const presetConfig = window.ORBY_PRESET_CONFIG && Array.isArray(window.ORBY_PRESET_CONFIG.profiles) ? window.ORBY_PRESET_CONFIG : { profiles: [] };
const connectionKey = "orby_connections_v1";
const workspaceLayoutKey = "orby_workspace_layout_v1";
const NEW_CONNECTION = "";
const commandHistory = [];
const activeConnectionIDs = new Set();
const browserSessionID = loadBrowserSessionID();
const drafts = {};
const outputModels = new WeakMap();
let activeToolName = "";
let expressionState = null;
let renderGeneration = 0;
let activeProbeController = null;
let probeGeneration = 0;
let manualConnectionID = newID();
let historyToolFilter = "all";
let outputRefreshPending = false;
let lastBrowsePattern = "*";
let queryInFlight = false;
let cancelInFlight = false;
let queryStartedAt = 0;

const els = {
  composerTool: document.getElementById("composer-tool"),
  composerToolPicker: document.getElementById("composer-tool-picker"),
  composerToolLogo: document.getElementById("composer-tool-logo"),
  composerConnection: document.getElementById("composer-connection"),
  pluginLeading: document.getElementById("plugin-leading"),
  pluginComposer: document.getElementById("plugin-composer"),
  actionPanel: document.getElementById("plugin-action-panel"),
  composerFormatPicker: document.getElementById("composer-format-picker"),
  composerFormatIcon: document.getElementById("composer-format-icon"),
  composerFormat: document.getElementById("composer-format"),
  queryForm: document.getElementById("query-form"),
  runQuery: document.getElementById("run-query"),
  browseKeys: document.getElementById("browse-keys"),
  emptyState: document.getElementById("empty-state"),
  emptyConnectionName: document.getElementById("empty-connection-name"),
  outputScroll: document.querySelector(".output-scroll"),
  outputArea: document.getElementById("output-area"),
  clearOutputs: document.getElementById("clear-outputs"),
  host: document.getElementById("host-input"),
  port: document.getElementById("port-input"),
  mode: document.getElementById("mode-select"),
  environment: document.getElementById("environment-select"),
  pluginFields: document.getElementById("plugin-fields"),
  formHost: document.getElementById("form-host"),
  formPort: document.getElementById("form-port"),
  formMode: document.getElementById("form-mode"),
  formEnvironment: document.getElementById("form-environment"),
  readOnlyStatus: document.getElementById("read-only-status"),
  formConnectionID: document.getElementById("form-connection-id"),
  formLeaseID: document.getElementById("form-lease-id"),
  formConnectionName: document.getElementById("form-connection-name"),
  historyPanel: document.getElementById("history-panel"),
  historySearch: document.getElementById("history-search"),
  connectionList: document.getElementById("connection-list"),
  operatorMain: document.querySelector(".operator-main"),
  panelBackdrop: document.getElementById("panel-backdrop"),
  toggleLeftPanel: document.getElementById("toggle-left-panel"),
  toggleRightPanel: document.getElementById("toggle-right-panel"),
  leftSidebarResizer: document.getElementById("left-sidebar-resizer"),
  rightSidebarResizer: document.getElementById("right-sidebar-resizer"),
  connectionName: document.getElementById("connection-name"),
  connectionSelect: document.getElementById("connection-select"),
  toolSelect: document.getElementById("tool-select"),
  newConnection: document.getElementById("new-connection"),
  saveConnection: document.getElementById("save-connection"),
  duplicateConnection: document.getElementById("duplicate-connection"),
  deleteConnection: document.getElementById("delete-connection"),
  connect: document.getElementById("connect-btn"),
  disconnect: document.getElementById("disconnect-btn"),
  connectionState: document.getElementById("connection-state"),
  connectionStatusText: document.getElementById("conn-status-text"),
  topConnection: document.getElementById("top-connection-status"),
  topConnectionText: document.getElementById("top-connection-text"),
  topActiveConnection: document.getElementById("top-active-connection"),
  composerActivity: document.getElementById("composer-activity"),
  queryStatus: document.getElementById("query-status"),
};

const defaultWorkspaceLayout = { leftOpen: true, rightOpen: true, leftWidth: 280, rightWidth: 340 };
let workspaceLayout = loadWorkspaceLayout();

function loadWorkspaceLayout() {
  try {
    const stored = JSON.parse(localStorage.getItem(workspaceLayoutKey) || "null");
    if (!stored || typeof stored !== "object") return { ...defaultWorkspaceLayout };
    return {
      leftOpen: stored.leftOpen !== false,
      rightOpen: stored.rightOpen !== false,
      leftWidth: clamp(Number(stored.leftWidth) || defaultWorkspaceLayout.leftWidth, 240, 520),
      rightWidth: clamp(Number(stored.rightWidth) || defaultWorkspaceLayout.rightWidth, 280, 560),
    };
  } catch {
    return { ...defaultWorkspaceLayout };
  }
}

function saveWorkspaceLayout() {
  try { localStorage.setItem(workspaceLayoutKey, JSON.stringify(workspaceLayout)); } catch { /* Storage is optional. */ }
}

function applyWorkspaceLayout() {
  els.operatorMain.style.setProperty("--left-sidebar-width", `${workspaceLayout.leftWidth}px`);
  els.operatorMain.style.setProperty("--right-sidebar-width", `${workspaceLayout.rightWidth}px`);
  els.operatorMain.classList.toggle("left-panel-collapsed", !workspaceLayout.leftOpen);
  els.operatorMain.classList.toggle("right-panel-collapsed", !workspaceLayout.rightOpen);
  document.documentElement.classList.remove("left-panel-collapsed", "right-panel-collapsed");
}

function newID() {
  return globalThis.crypto?.randomUUID?.() || `connection-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function loadBrowserSessionID() {
  const key = "orby_browser_session_v1";
  try {
    const current = sessionStorage.getItem(key);
    if (current) return current;
    const created = newID();
    sessionStorage.setItem(key, created);
    return created;
  } catch {
    return newID();
  }
}

function loadSavedConnections() {
  try {
    const parsed = JSON.parse(localStorage.getItem(connectionKey) || "[]");
    if (!Array.isArray(parsed)) return [];
    let migrated = false;
    const values = parsed.filter((item) => item && typeof item.id === "string" && typeof item.name === "string" && typeof item.tool === "string")
      .map((item) => {
        if (item.tool !== "aql") return item;
        migrated = true;
        return { ...item, tool: "aerospike" };
      });
    if (migrated) storeConnections(values);
    return values;
  } catch { return []; }
}

function loadConnections() {
  return mergeConnections(loadSavedConnections(), presetConfig);
}

function storeConnections(values) {
  try { localStorage.setItem(connectionKey, JSON.stringify(values)); } catch { /* Storage is optional. */ }
}

function currentTool() {
  return tools.find((tool) => tool?.name === els.composerTool.value) || tools[0] || null;
}

function currentElements() {
  return Array.isArray(currentTool()?.composer?.elements) ? currentTool().composer.elements : [];
}

function formatsForTool(tool) {
  const formats = Array.isArray(tool?.formats) ? tool.formats.filter(Boolean) : [];
  return formats.length ? [...new Set(formats)] : [tool?.defaultFormat || "raw"];
}

function renderToolOptions() {
  const options = tools.map((tool) => `<option value="${escapeHTML(tool.name)}">${escapeHTML(tool.label || tool.name)}</option>`).join("");
  els.composerTool.innerHTML = options;
  els.toolSelect.innerHTML = options;
  refreshCustomSelects(els.composerTool);
  refreshCustomSelects(els.toolSelect);
}

function syncToolPresentation() {
  const tool = currentTool();
  const label = tool?.label || tool?.name || "Plugin";
  els.composerToolLogo.src = tool?.icon || "/static/icons/terminal.svg";
  els.composerToolPicker.title = label;
  els.composerTool.setAttribute("aria-label", `Plugin: ${label}`);
  els.browseKeys.hidden = tool?.name !== "redis";
  refreshCustomSelects(els.composerTool);
}

function renderFormats(preferred) {
  const formats = formatsForTool(currentTool());
  els.composerFormat.innerHTML = formats.map((format) => `<option value="${escapeHTML(format)}">${escapeHTML(format.toUpperCase())}</option>`).join("");
  els.composerFormat.value = formats.includes(preferred) ? preferred : (formats.includes(currentTool()?.defaultFormat) ? currentTool().defaultFormat : formats[0]);
  syncFormatPresentation();
  refreshCustomSelects(els.composerFormat);
}

function syncFormatPresentation() {
  const format = els.composerFormat.value || "raw";
  const icons = { json: "/static/icons/braces.svg", table: "/static/icons/table-2.svg", raw: "/static/icons/terminal.svg" };
  els.composerFormatIcon.src = icons[format] || icons.raw;
  els.composerFormatPicker.title = `Output: ${format.toUpperCase()}`;
}

function connectionsForTool() {
  return loadConnections().filter((item) => item.tool === els.composerTool.value);
}

function selectedConnection() {
  return connectionsForTool().find((item) => item.id === els.composerConnection.value) || null;
}

function renderConnectionOptions(preferred = els.composerConnection.value) {
  const compatible = connectionsForTool();
  const html = `<option value="">New connection</option>${compatible.map((item) => `<option value="${escapeHTML(item.id)}">${escapeHTML(item.name)}</option>`).join("")}`;
  els.composerConnection.innerHTML = html;
  els.connectionSelect.innerHTML = html;
  const selected = compatible.some((item) => item.id === preferred) ? preferred : NEW_CONNECTION;
  els.composerConnection.value = selected;
  els.connectionSelect.value = selected;
  renderConnectionRail();
  return selected;
}

function renderConnectionRail() {
  const selectedID = els.composerConnection.value;
  const selectedTool = els.composerTool.value;
  const connections = loadConnections();
  const manualConnected = activeConnectionIDs.has(manualConnectionID) ? " is-connected" : "";
  const manual = !selectedID ? `<button type="button" class="connection-item is-active${manualConnected}" data-connection-id="" data-connection-tool="${escapeHTML(selectedTool)}"><span class="connection-dot"></span><span class="connection-item-copy"><strong>New connection</strong><small>Enter connection details</small></span></button>` : "";
  const collapsedProfiles = loadCollapsedProfiles(localStorage);
  const groups = connectionGroups(connections, presetConfig.profiles).map((group) => {
    const items = group.connections.map((item) => {
      const active = item.id === selectedID && item.tool === selectedTool ? " is-active" : "";
      const connected = activeConnectionIDs.has(item.id) ? " is-connected" : "";
      const endpoint = item.host?.includes(":") ? item.host : [item.host, item.port].filter(Boolean).join(":");
      const stageBadge = item.environment === "stage" ? `<span class="connection-env-badge">STAGE</span>` : "";
      return `<button type="button" class="connection-item${active}${connected}" data-connection-id="${escapeHTML(item.id)}" data-connection-tool="${escapeHTML(item.tool)}"><span class="connection-dot"></span><span class="connection-item-copy"><strong>${escapeHTML(item.name)}</strong><small>${escapeHTML(endpoint || item.tool)}</small></span>${stageBadge}</button>`;
    }).join("");
    const open = collapsedProfiles.has(group.profile) ? "" : " open";
    return `<details class="connection-group" data-profile="${escapeHTML(group.profile)}"${open}><summary class="connection-group-summary"><span>${escapeHTML(group.label)}</span><span class="connection-group-count">${group.connections.length}</span><span class="connection-group-chevron" aria-hidden="true"></span></summary><div class="connection-group-items">${items}</div></details>`;
  }).join("");
  els.connectionList.innerHTML = manual + groups;
  els.connectionList.querySelectorAll("details.connection-group").forEach((details) => details.addEventListener("toggle", () => {
    setProfileCollapsed(localStorage, details.dataset.profile, !details.open);
  }));
}

async function refreshActiveConnections() {
  try {
    const response = await fetch(`/connection-status?session=${encodeURIComponent(browserSessionID)}`, { headers: { Accept: "application/json" } });
    if (!response.ok) return;
    const result = await response.json();
    const next = new Set(Array.isArray(result.active) ? result.active : []);
    if (sameSet(next, activeConnectionIDs)) return;
    activeConnectionIDs.clear();
    for (const id of next) activeConnectionIDs.add(id);
    renderConnectionRail();
  } catch { /* Keep the last known state if the local status request fails. */ }
}

function renderPluginFields(values = {}) {
  const fields = Array.isArray(currentTool()?.fields) ? currentTool().fields : [];
  els.pluginFields.innerHTML = fields.map((field) => `<label>${escapeHTML(field.label || field.key)}
    <input data-plugin-field="${escapeHTML(field.key)}" name="${escapeHTML(field.key)}" form="query-form"
      type="${escapeHTML(field.inputType || "text")}" value="${escapeHTML(values[field.key] ?? field.default ?? "")}" placeholder="${escapeHTML(field.placeholder || "")}"></label>`).join("");
}

function syncFormState() {
  const connection = selectedConnection();
  els.formConnectionID.value = connection?.id || manualConnectionID;
  els.formLeaseID.value = `${browserSessionID}:${els.formConnectionID.value}`;
  els.formConnectionName.value = els.connectionName.value.trim() || "New connection";
  els.formHost.value = els.host.value.trim();
  els.formPort.value = els.port.value.trim();
  els.formMode.value = els.mode.value;
  els.formEnvironment.value = els.environment.value;
  els.toolSelect.value = els.composerTool.value;
  els.connectionSelect.value = els.composerConnection.value;
  els.emptyConnectionName.textContent = els.formConnectionName.value;
  els.topActiveConnection.textContent = els.formConnectionName.value === "New connection"
    ? "Connections"
    : els.formConnectionName.value;
  refreshCustomSelects(els.toolSelect);
  refreshCustomSelects(els.mode);
  refreshCustomSelects(els.environment);
  syncEnvironmentBadge();
}

// The topbar badge is the one signal always visible regardless of which
// panel is open, so it must reflect the resolved (not merely selected)
// environment: a preset always shows its true connections.json value here
// too, since applyConnection() seeds els.environment from the preset itself.
function syncEnvironmentBadge() {
  const stage = els.formEnvironment.value === "stage";
  els.readOnlyStatus.textContent = stage ? "[STAGE · WRITES ALLOWED]" : "[READ ONLY]";
  els.readOnlyStatus.classList.toggle("is-stage", stage);
}

function connectionParams() {
  const params = new URLSearchParams({
    tool: els.composerTool.value,
    connectionId: els.formConnectionID.value,
    leaseId: els.formLeaseID.value,
    connectionName: els.formConnectionName.value,
    host: els.formHost.value,
    port: els.formPort.value,
    mode: els.formMode.value,
    environment: els.formEnvironment.value,
  });
  els.pluginFields.querySelectorAll("[data-plugin-field]").forEach((input) => params.set(input.dataset.pluginField, input.value));
  return params;
}

async function connectSelectedConnection() {
  syncFormState(); activeProbeController?.abort(); const generation = ++probeGeneration;
  const controller = new AbortController(); activeProbeController = controller; els.connect.disabled = true; setConnectionStatus("pending", "Checking TCP reachability…");
  const params = connectionParams();
  try {
    const response = await fetch("/connect", { method: "POST", headers: { "Content-Type": "application/x-www-form-urlencoded" }, body: params, signal: controller.signal });
    const result = await response.json(); if (generation !== probeGeneration) return;
    setConnectionStatus(response.ok && result.reachable ? "reachable" : "unreachable", result.message || "Target unreachable");
    if (response.ok && result.reachable) { await refreshActiveConnections(); await renderComposer(composerValues()); }
  } catch (error) { if (error?.name !== "AbortError") setConnectionStatus("unreachable", "Reachability check failed"); }
  finally { if (generation === probeGeneration) { activeProbeController = null; els.connect.disabled = false; } }
}

async function selectAndConnect(id, tool) {
  const selectedTool = tool || els.composerTool.value;
  if (els.composerTool.value !== selectedTool) { els.composerTool.value = selectedTool; await handleToolChange(id); }
  else await applyConnection(id);
  closeMobilePanels();
  if (!id) return;
  if (!activeConnectionIDs.has(id)) await connectSelectedConnection();
  else { setConnectionStatus("reachable", "Connected"); await renderComposer(composerValues()); }
}

async function applyConnection(id) {
  const connection = connectionsForTool().find((item) => item.id === id);
  if (!connection) manualConnectionID = newID();
  els.composerConnection.value = connection?.id || NEW_CONNECTION;
  els.connectionSelect.value = els.composerConnection.value;
  els.connectionName.value = connection?.name || "";
  els.host.value = connection?.host || "";
  els.port.value = connection?.port || "";
  els.mode.value = connection?.mode || "single";
  els.environment.value = connection?.environment === "stage" ? "stage" : "prod";
  renderPluginFields(connection?.fields || {});
  syncFormState();
  renderConnectionRail();
  setConnectionStatus("disconnected", connection ? "Ready to connect" : "Enter connection details");
  await renderComposer(drafts[activeToolName] || {});
  updateConnectionActions();
}

function collectCurrentConnection(id = els.composerConnection.value || manualConnectionID) {
  const fields = {};
  els.pluginFields.querySelectorAll("[data-plugin-field]").forEach((input) => { fields[input.dataset.pluginField] = input.value; });
  return { id, name: els.connectionName.value.trim(), tool: els.composerTool.value, host: els.host.value.trim(), port: els.port.value.trim(), mode: els.mode.value, environment: els.environment.value, fields };
}

function showConnectionMessage(message, invalid = false) {
  els.connectionStatusText.textContent = message;
  els.connectionStatusText.classList.toggle("field-error", invalid);
}

function hasValidConnectionDetails() {
  return Boolean(els.connectionName.value.trim() && els.host.value.trim() && els.port.value.trim());
}

function updateConnectionActions() {
  const valid = hasValidConnectionDetails();
  const connection = selectedConnection();
  const saved = Boolean(connection);
  const preset = Boolean(connection?.preset);
  const pending = els.connectionState.dataset.state === "pending";
  els.saveConnection.disabled = !valid || preset;
  els.duplicateConnection.disabled = !saved;
  els.deleteConnection.disabled = !saved || preset;
  els.connect.disabled = !valid || pending;
  els.disconnect.disabled = els.connectionState.dataset.state !== "reachable";
  // A preset's environment is server-enforced from connections.json regardless
  // of this control, so disable it here to avoid implying a client override works.
  els.environment.disabled = preset;
}

function saveCurrentConnection() {
  if (selectedConnection()?.preset) return showConnectionMessage("Duplicate a preset before editing it.", true);
  const connection = collectCurrentConnection();
  if (!connection.name || !connection.host || !connection.port) return showConnectionMessage("Name, host, and port are required to save.", true);
  const values = loadSavedConnections().filter((item) => item.id !== connection.id);
  values.unshift(connection);
  storeConnections(values);
  renderConnectionOptions(connection.id);
  applyConnection(connection.id);
  showConnectionMessage("Connection saved.");
}

function duplicateCurrentConnection() {
  const source = selectedConnection() || collectCurrentConnection();
  const { preset, profile, ...details } = source;
  const copy = { ...details, id: newID(), name: `${source.name || "Connection"} copy`, fields: { ...(source.fields || {}) } };
  storeConnections([copy, ...loadSavedConnections()]);
  renderConnectionOptions(copy.id);
  applyConnection(copy.id);
}

function deleteSelectedConnection() {
  const id = els.composerConnection.value;
  if (!id || selectedConnection()?.preset) return;
  storeConnections(loadSavedConnections().filter((item) => item.id !== id));
  applyConnection(renderConnectionOptions(NEW_CONNECTION));
}

const leftMobileQuery = window.matchMedia("(max-width: 1050px)");
const rightMobileQuery = window.matchMedia("(max-width: 820px)");

function isMobilePanel(side) {
  return side === "left" ? leftMobileQuery.matches : rightMobileQuery.matches;
}

function syncPanelControls() {
  for (const side of ["left", "right"]) {
    const button = side === "left" ? els.toggleLeftPanel : els.toggleRightPanel;
    const label = side === "left" ? "connections sidebar" : "details sidebar";
    const open = isMobilePanel(side)
      ? els.operatorMain.classList.contains(`mobile-${side}-open`)
      : !els.operatorMain.classList.contains(`${side}-panel-collapsed`);
    button.setAttribute("aria-expanded", String(open));
    button.setAttribute("aria-label", `${open ? "Hide" : "Show"} ${label}`);
    button.title = `${open ? "Hide" : "Show"} ${label}`;
  }
}

function closeMobilePanels() {
  els.operatorMain.classList.remove("mobile-left-open", "mobile-right-open");
  syncPanelControls();
}

function closePanels() {
  closeMobilePanels();
  if (leftMobileQuery.matches || rightMobileQuery.matches) return;
  workspaceLayout.leftOpen = false;
  workspaceLayout.rightOpen = false;
  applyWorkspaceLayout();
  saveWorkspaceLayout();
  syncPanelControls();
}

function togglePanel(side) {
  if (isMobilePanel(side)) {
    const className = `mobile-${side}-open`;
    const willOpen = !els.operatorMain.classList.contains(className);
    closeMobilePanels();
    if (willOpen) els.operatorMain.classList.add(className);
    syncPanelControls();
    return;
  }
  const className = `${side}-panel-collapsed`;
  const open = els.operatorMain.classList.contains(className);
  workspaceLayout[`${side}Open`] = open;
  applyWorkspaceLayout();
  saveWorkspaceLayout();
  syncPanelControls();
  window.requestAnimationFrame(() => window.dispatchEvent(new Event("resize")));
}

function sidebarWidth(side, clientX) {
  const bounds = els.operatorMain.getBoundingClientRect();
  const otherWidth = side === "left"
    ? (workspaceLayout.rightOpen ? workspaceLayout.rightWidth : 0)
    : (workspaceLayout.leftOpen ? workspaceLayout.leftWidth : 0);
  const available = Math.max(0, bounds.width - otherWidth - 420);
  const requested = side === "left" ? clientX - bounds.left : bounds.right - clientX;
  const minimum = side === "left" ? 240 : 280;
  const maximum = Math.min(side === "left" ? 520 : 560, Math.max(minimum, available));
  return clamp(requested, minimum, maximum);
}

function setSidebarWidth(side, width) {
  workspaceLayout[`${side}Width`] = width;
  applyWorkspaceLayout();
  window.requestAnimationFrame(() => window.dispatchEvent(new Event("resize")));
}

function startSidebarResize(side, event) {
  if (isMobilePanel(side)) return;
  event.preventDefault();
  const handle = side === "left" ? els.leftSidebarResizer : els.rightSidebarResizer;
  handle.classList.add("is-dragging");
  document.body.classList.add("is-resizing-sidebar");
  const move = (moveEvent) => setSidebarWidth(side, sidebarWidth(side, moveEvent.clientX));
  const stop = () => {
    handle.classList.remove("is-dragging");
    document.body.classList.remove("is-resizing-sidebar");
    window.removeEventListener("pointermove", move);
    window.removeEventListener("pointerup", stop);
    window.removeEventListener("pointercancel", stop);
    saveWorkspaceLayout();
  };
  window.addEventListener("pointermove", move);
  window.addEventListener("pointerup", stop, { once: true });
  window.addEventListener("pointercancel", stop, { once: true });
}

function resizeSidebarWithKeyboard(side, event) {
  if (isMobilePanel(side) || !["ArrowLeft", "ArrowRight"].includes(event.key)) return;
  event.preventDefault();
  const direction = event.key === "ArrowRight" ? 1 : -1;
  const signedDirection = side === "left" ? direction : -direction;
  const current = workspaceLayout[`${side}Width`];
  const minimum = side === "left" ? 240 : 280;
  const maximum = side === "left" ? 520 : 560;
  setSidebarWidth(side, clamp(current + signedDirection * 16, minimum, maximum));
  saveWorkspaceLayout();
}

function composerValues() {
  const values = {};
  els.queryForm.querySelectorAll("[data-composer-name]").forEach((control) => {
    values[control.dataset.composerName] = control.type === "checkbox" ? String(control.checked) : control.value;
  });
  return values;
}

function saveDraft() {
  if (activeToolName) drafts[activeToolName] = composerValues();
}

function elementControl(element, state) {
  if (element.kind === "literal") {
    const span = document.createElement("span");
    span.className = element.text === "*" ? "composer-literal composer-star" : "composer-literal";
    span.textContent = element.text || "";
    return span;
  }
  if (element.kind === "input") {
    const input = document.createElement("input");
    input.className = "composer-input";
    input.name = element.name || "";
    input.dataset.composerName = element.name || "";
    input.type = element.inputType || "text";
    input.placeholder = element.placeholder || "";
    input.value = state[element.name] ?? element.default ?? "";
    input.setAttribute("form", "query-form");
    input.autocomplete = "off";
    input.spellcheck = false;
    if (element.grow) input.classList.add("composer-grow");
    if (currentTool()?.name === "redis" && element.name === "query") return redisGhostWrap(input);
    return input;
  }
  if (element.kind === "checkbox") {
    const label = document.createElement("label");
    label.className = element.icon ? "composer-checkbox composer-icon-toggle" : "composer-checkbox";
    label.title = element.text || element.name || "Option";
    const input = document.createElement("input");
    input.type = "checkbox";
    input.name = element.name || "";
    input.value = "true";
    input.dataset.composerName = element.name || "";
    input.checked = String(state[element.name] ?? element.default ?? "false") === "true";
    input.setAttribute("form", "query-form");
    const text = document.createElement("span");
    text.textContent = element.text || element.name || "";
    if (element.icon) {
      const icon = document.createElement("img");
      icon.src = element.icon;
      icon.alt = "";
      label.append(input, icon, text);
    } else {
      label.append(input, text);
    }
    return label;
  }
  if (element.kind === "select") {
    const select = document.createElement("select");
    select.className = "composer-select";
    select.name = element.name || "";
    select.dataset.composerName = element.name || "";
    select.dataset.options = element.options || "";
    select.dataset.dependsOn = element.dependsOn || "";
    select.dataset.preferred = state[element.name] ?? element.default ?? "";
    select.setAttribute("form", "query-form");
    select.innerHTML = `<option value="">${escapeHTML(element.name || "select")}…</option>`;
    return select;
  }
  if (element.kind === "action") {
    const wrapper = document.createElement("span");
    wrapper.className = "composer-action-wrap";
    const input = document.createElement("input");
    input.type = "hidden";
    input.name = element.name || "";
    input.dataset.composerName = element.name || "";
    input.value = state[element.name] || "";
    input.setAttribute("form", "query-form");
    const button = document.createElement("button");
    button.type = "button";
    button.className = "composer-action filter-icon-button";
    button.dataset.action = element.action || "";
    const actionLabel = element.label || "Edit filters";
    button.title = actionLabel;
    button.setAttribute("aria-label", actionLabel);
    button.innerHTML = `<span class="filter-icon" aria-hidden="true"></span><span class="action-count"></span>`;
    wrapper.append(input, button);
    return wrapper;
  }
  return document.createTextNode("");
}

function currentToolCommands() {
  return Array.isArray(currentTool()?.commands) ? currentTool().commands : [];
}

function redisGhostWrap(input) {
  const wrap = document.createElement("span");
  wrap.className = "redis-cmd-wrap";
  const ghost = document.createElement("span");
  ghost.className = "ghost-layer";
  ghost.setAttribute("aria-hidden", "true");
  const ghostTyped = document.createElement("span");
  ghostTyped.className = "g-typed";
  const ghostSugg = document.createElement("span");
  ghostSugg.className = "g-sugg";
  ghost.append(ghostTyped, ghostSugg);
  let suggested = "";
  function renderGhost() {
    const value = input.value;
    const typedText = value.trimEnd();
    const tokens = value.split(/\s+/).filter((t) => t.length > 0);
    const typedCmd = (tokens[0] || "").toUpperCase();
    if (tokens.length === 0 || !typedCmd) {
      ghostTyped.textContent = "";
      ghostSugg.textContent = "";
      suggested = "";
      return;
    }
    const commands = currentToolCommands();
    const exact = commands.find((command) => command.name === typedCmd);
    if (!exact) {
      const matches = commands.filter((command) => command.name.startsWith(typedCmd));
      if (matches.length === 0) {
        ghostTyped.textContent = "";
        ghostSugg.textContent = "";
        suggested = "";
        return;
      }
      const pick = matches[0];
      ghostTyped.textContent = typedText;
      ghostSugg.textContent = pick.name.slice(typedCmd.length) + " ";
      suggested = pick.name.slice(typedCmd.length) + " ";
      return;
    }
    const args = exact.args;
    const filled = tokens.length - 1;
    if (filled < args.length) {
      const remaining = args.slice(filled).join(" ");
      ghostTyped.textContent = typedText;
      ghostSugg.textContent = " " + remaining;
      suggested = " " + remaining;
    } else {
      ghostTyped.textContent = "";
      ghostSugg.textContent = "";
      suggested = "";
    }
  }
  input.addEventListener("input", renderGhost);
  input.addEventListener("focus", renderGhost);
  input.addEventListener("keydown", (event) => {
    if (event.key !== "Tab" || !suggested) return;
    event.preventDefault();
    const pos = input.selectionStart ?? input.value.length;
    const before = input.value.slice(0, pos);
    const trailing = before.match(/\s*$/)?.[0] || "";
    if (trailing) {
      const cleanPos = pos - trailing.length;
      const delimiter = /^\s/.test(suggested) ? " " : "";
      const insertion = suggested.replace(/^\s+/, "");
      input.value = input.value.slice(0, cleanPos) + delimiter + insertion + input.value.slice(pos);
    } else {
      input.value = input.value.slice(0, pos) + suggested + input.value.slice(pos);
    }
    suggested = "";
    renderGhost();
  });
  wrap.append(ghost, input);
  return wrap;
}

function composerControls(elements, state) {
  return elements.map((element) => {
    const control = elementControl(element, state);
    if (element.hideNarrow && control.classList) control.classList.add("composer-hide-narrow");
    return control;
  });
}

async function renderComposer(state = {}) {
  const generation = ++renderGeneration;
  els.actionPanel.hidden = true;
  els.actionPanel.innerHTML = "";
  const expressionRaw = state.expression || "";
  try { expressionState = expressionRaw ? JSON.parse(expressionRaw) : null; } catch { expressionState = null; }
  const elements = currentElements();
  const leadingElements = elements.filter((element) => element.placement === "leading");
  const queryElements = elements.filter((element) => element.placement !== "leading");
  els.pluginLeading.replaceChildren(...composerControls(leadingElements, state));
  els.pluginLeading.hidden = leadingElements.length === 0;
  els.queryForm.classList.toggle("has-leading", leadingElements.length > 0);
  els.pluginComposer.replaceChildren(...composerControls(queryElements, state));
  bindComposerEvents();
  for (const select of els.pluginComposer.querySelectorAll("select[data-options]")) {
    if (!select.dataset.dependsOn) await loadOptions(select, generation);
  }
  for (const select of els.pluginComposer.querySelectorAll("select[data-options][data-depends-on]")) await loadOptions(select, generation);
  updateActionCount();
  refreshCustomSelects(els.pluginLeading);
  refreshCustomSelects(els.pluginComposer);
}

function bindComposerEvents() {
  els.queryForm.querySelectorAll("[data-composer-name]").forEach((control) => {
    control.addEventListener("input", saveDraft);
    control.addEventListener("change", saveDraft);
  });
  els.pluginComposer.querySelectorAll("select[data-options]").forEach((select) => {
    select.addEventListener("change", async () => {
      updateSelectTitle(select);
      const dependents = els.pluginComposer.querySelectorAll(`select[data-depends-on="${CSS.escape(select.name)}"]`);
      for (const dependent of dependents) await loadOptions(dependent, renderGeneration);
    });
  });
  els.pluginComposer.querySelectorAll("button[data-action]").forEach((button) => button.addEventListener("click", () => openAction(button.dataset.action)));
  const queryInput = els.pluginComposer.querySelector('[data-composer-name="query"]');
  queryInput?.addEventListener("keydown", handleHistoryKey);
}

function optionParams(resource) {
  syncFormState();
  const params = new URLSearchParams({
    tool: els.composerTool.value, resource,
    connectionId: els.formConnectionID.value, leaseId: els.formLeaseID.value, connectionName: els.formConnectionName.value,
    host: els.formHost.value, port: els.formPort.value, mode: els.formMode.value, environment: els.formEnvironment.value,
  });
  for (const [name, value] of Object.entries(composerValues())) params.set(name, value);
  return params;
}

async function loadOptions(select, generation = renderGeneration) {
  const dependency = select.dataset.dependsOn;
  if (dependency && !composerValues()[dependency]) {
    select.innerHTML = `<option value="">${escapeHTML(select.name)}…</option>`;
    updateSelectTitle(select);
    return;
  }
  const preferred = select.dataset.preferred || select.value;
  if (els.connectionState.dataset.state !== "reachable" || !els.formHost.value || !els.formPort.value) {
    select.innerHTML = `<option value="">${escapeHTML(select.name)}…</option>`;
    updateSelectTitle(select);
    return;
  }
  select.disabled = true;
  try {
    const response = await fetch(`/plugin-options?${optionParams(select.dataset.options)}`);
    const data = response.ok ? await response.json() : { options: [] };
    if (generation !== renderGeneration) return;
    const options = Array.isArray(data.options) ? data.options : [];
    select.innerHTML = options.length ? options.map((item) => `<option value="${escapeHTML(item.value)}">${escapeHTML(item.label || item.value)}</option>`).join("") : `<option value="">No ${escapeHTML(select.name)}</option>`;
    if (options.some((item) => item.value === preferred)) select.value = preferred;
    select.dataset.preferred = "";
  } catch {
    if (generation === renderGeneration) select.innerHTML = `<option value="">Unavailable</option>`;
  } finally {
    if (generation === renderGeneration) { select.disabled = false; updateSelectTitle(select); saveDraft(); refreshCustomSelects(select); }
  }
}

function updateSelectTitle(select) {
  select.title = select.selectedOptions[0]?.textContent || select.name || "";
}

function openAction(action) {
  if (action !== "expression" || !currentTool()?.composer?.expression) return;
  els.actionPanel.hidden = false;
  renderExpressionPanel();
  keepLatestOutputVisible();
}

function keepLatestOutputVisible() {
  window.requestAnimationFrame(() => { els.outputScroll.scrollTop = els.outputScroll.scrollHeight; });
}

function emptyGroup(logic = "and") { return { kind: "group", logic, children: [] }; }
function emptyCondition() {
  const type = currentTool()?.composer?.expression?.types?.[0];
  return { kind: "condition", bin: "", type: type?.value || "", operator: type?.operators?.[0]?.value || "", value: "" };
}

function renderExpressionPanel() {
  if (!expressionState) expressionState = emptyGroup();
  els.actionPanel.innerHTML = `<div class="expression-head"><strong>Filter expression</strong><span>Choose every bin type explicitly</span><button type="button" data-close-panel>Close</button></div><div class="expression-tree"></div><p class="expression-message" role="status"></p>`;
  els.actionPanel.querySelector(".expression-tree").append(renderExpressionNode(expressionState, null));
  els.actionPanel.querySelector("[data-close-panel]").addEventListener("click", () => { els.actionPanel.hidden = true; keepLatestOutputVisible(); });
  syncExpressionInput();
  refreshCustomSelects(els.actionPanel);
}

function renderExpressionNode(node, parent) {
  if (node.kind === "condition") return renderCondition(node, parent);
  const container = document.createElement("div");
  container.className = "expression-group";
  const head = document.createElement("div");
  head.className = "expression-group-head";
  head.innerHTML = `<select aria-label="Group logic"><option value="and">AND</option><option value="or">OR</option></select><button type="button" data-add-condition>+ Condition</button><button type="button" data-add-group>+ Group</button>${parent ? '<button type="button" data-remove-group>Remove</button>' : ""}`;
  const logic = head.querySelector("select"); logic.value = node.logic || "and";
  logic.addEventListener("change", () => { node.logic = logic.value; syncExpressionInput(); });
  head.querySelector("[data-add-condition]").addEventListener("click", () => { node.children.push(emptyCondition()); renderExpressionPanel(); });
  head.querySelector("[data-add-group]").addEventListener("click", () => { node.children.push(emptyGroup()); renderExpressionPanel(); });
  head.querySelector("[data-remove-group]")?.addEventListener("click", () => { parent.children.splice(parent.children.indexOf(node), 1); renderExpressionPanel(); });
  container.append(head);
  const children = document.createElement("div"); children.className = "expression-children";
  node.children.forEach((child) => children.append(renderExpressionNode(child, node)));
  container.append(children);
  return container;
}

function renderCondition(node, parent) {
  const row = document.createElement("div");
  row.className = "expression-condition";
  const bin = document.createElement("input");
  bin.type = "text";
  bin.placeholder = "bin name";
  bin.setAttribute("aria-label", "Bin");
  bin.value = node.bin || "";
  const type = document.createElement("select");
  type.setAttribute("aria-label", "Bin type");
  const types = currentTool().composer.expression.types || [];
  type.innerHTML = types.map((item) => `<option value="${escapeHTML(item.value)}">${escapeHTML(item.label || item.value)}</option>`).join("");
  type.value = node.type;
  const operator = document.createElement("select"); operator.setAttribute("aria-label", "Operator");
  const valueHost = document.createElement("span"); valueHost.className = "expression-value";
  const remove = document.createElement("button"); remove.type = "button"; remove.textContent = "×"; remove.setAttribute("aria-label", "Remove condition");
  function renderValue() {
    const definition = types.find((item) => item.value === type.value) || types[0];
    operator.innerHTML = (definition?.operators || []).map((item) => `<option value="${escapeHTML(item.value)}">${escapeHTML(item.label || item.value)}</option>`).join("");
    operator.value = (definition?.operators || []).some((item) => item.value === node.operator) ? node.operator : definition?.operators?.[0]?.value || "";
    valueHost.innerHTML = definition?.inputType === "select" ? '<select aria-label="Value"><option value="true">true</option><option value="false">false</option></select>' : `<input aria-label="Value" type="${escapeHTML(definition?.inputType || "text")}">`;
    const value = valueHost.firstElementChild; value.value = node.value || "";
    value.addEventListener("input", () => { node.value = value.value; syncExpressionInput(); });
    value.addEventListener("change", () => { node.value = value.value; syncExpressionInput(); });
    node.type = type.value; node.operator = operator.value;
    refreshCustomSelects(valueHost);
  }
  bin.addEventListener("input", () => { node.bin = bin.value; syncExpressionInput(); });
  type.addEventListener("change", () => { node.type = type.value; node.value = ""; renderValue(); syncExpressionInput(); });
  operator.addEventListener("change", () => { node.operator = operator.value; syncExpressionInput(); });
  remove.addEventListener("click", () => { parent.children.splice(parent.children.indexOf(node), 1); renderExpressionPanel(); });
  renderValue(); row.append(bin, type, operator, valueHost, remove); return row;
}

function validateExpression(node = expressionState) {
  if (!node || (node.kind === "group" && node.children.length === 0)) return "";
  if (node.kind === "group") {
    if (!node.children.length) return "Remove empty groups or add a condition.";
    for (const child of node.children) { const message = validateExpression(child); if (message) return message; }
    return "";
  }
  return node.bin && node.type && node.operator && String(node.value).trim() ? "" : "Complete every filter condition.";
}

function syncExpressionInput() {
  const input = els.pluginComposer.querySelector('[data-composer-name="expression"]');
  if (!input) return;
  input.value = expressionState?.children?.length ? JSON.stringify(expressionState) : "";
  saveDraft(); updateActionCount();
}

function updateActionCount() {
  const count = countConditions(expressionState);
  const label = els.pluginComposer.querySelector(".action-count");
  if (label) label.textContent = count ? String(count) : "";
}

function countConditions(node) {
  if (!node) return 0;
  if (node.kind === "condition") return 1;
  return (node.children || []).reduce((total, child) => total + countConditions(child), 0);
}

function composerSummary() {
  const values = composerValues();
  return currentElements().map((element) => {
    if (element.kind === "literal") return element.decorative ? "" : (element.text || "");
    if (element.kind === "input" || element.kind === "select") return values[element.name] || "";
    return "";
  }).filter(Boolean).join(" ").replace(/\s+\.\s+/g, ".");
}

function filteredHistory() {
  const query = els.historySearch.value.trim().toLowerCase();
  return commandHistory.map((item, index) => ({ item, index })).filter(({ item }) => {
    const matchesTool = historyToolFilter === "all" || item.tool === historyToolFilter;
    const matchesQuery = !query || `${item.summary} ${item.connectionName || ""}`.toLowerCase().includes(query);
    return matchesTool && matchesQuery;
  });
}

function historyGroup(createdAt) {
  const date = new Date(createdAt || Date.now());
  const today = new Date(); today.setHours(0, 0, 0, 0);
  const yesterday = new Date(today); yesterday.setDate(today.getDate() - 1);
  return date >= today ? "Today" : date >= yesterday ? "Yesterday" : "Earlier";
}

function renderHistory() {
  let group = "";
  els.historyPanel.innerHTML = filteredHistory().map(({ item }) => {
    const nextGroup = historyGroup(item.createdAt);
    const heading = nextGroup === group ? "" : `<div class="history-group">${nextGroup}</div>`;
    group = nextGroup;
    const meta = [item.connectionName, item.format?.toUpperCase()].filter(Boolean).join(" · ");
    return `${heading}<div class="hist-item"><span class="hist-badge ${escapeHTML(item.tool)}">${escapeHTML(item.tool === "aerospike" ? "AS" : item.tool)}</span><span class="hist-copy"><span class="hist-cmd">${escapeHTML(item.summary)}</span><small>${escapeHTML(meta)}</small></span></div>`;
  }).join("") || `<p class="history-empty">No matching commands</p>`;
}

function addHistory(tool, summary, state) {
  commandHistory.unshift({
    tool, summary, state: { ...state }, createdAt: Date.now(),
    connectionName: els.formConnectionName.value || "New connection",
    format: els.composerFormat.value,
  });
  commandHistory.splice(100); renderHistory();
}

async function handleHistoryKey(event) {
  if (event.key === "Enter") { event.preventDefault(); els.queryForm.requestSubmit(); }
}

function setConnectionStatus(state, message) {
  const labels = { "details-changed": "Unsaved changes", pending: "Connecting", reachable: "Reachable", unreachable: "Unavailable", disconnected: "Disconnected" };
  const label = labels[state] || state;
  els.connectionState.dataset.state = state; els.connectionState.textContent = label;
  els.topConnection.dataset.state = state; els.topConnectionText.textContent = label;
  showConnectionMessage(message);
  updateConnectionActions();
}

function invalidateConnectionStatus(message = "Unsaved connection changes") {
  activeProbeController?.abort(); activeProbeController = null; probeGeneration += 1;
  els.connect.textContent = "Connect"; setConnectionStatus("details-changed", message);
}

function setRunning(running) {
  queryInFlight = running;
  cancelInFlight = false;
  els.runQuery.disabled = false;
  els.runQuery.classList.toggle("is-running", running);
  els.runQuery.querySelector(".run-label").textContent = running ? "Cancel" : "Run";
  els.runQuery.setAttribute("aria-label", running ? "Cancel running query" : "Run query");
  if (els.composerActivity) els.composerActivity.hidden = !running;
  if (running) queryStartedAt = Date.now();
}

function isEditableTarget(target) {
  const element = target instanceof Element ? target : null;
  return Boolean(element && (element.matches("input, textarea, select") || element.isContentEditable));
}

function announceQueryStatus(message) {
  if (els.queryStatus) els.queryStatus.textContent = message;
}

// Aborting closes the connection, which cancels the plugin context server-side;
// the cancelled block is rendered client-side because no response will arrive.
function cancelRunningQuery() {
  if (!queryInFlight || cancelInFlight) return;
  cancelInFlight = true;
  const elapsed = Date.now() - queryStartedAt;
  setRunning(false);
  els.queryForm.dispatchEvent(new CustomEvent("htmx:abort", { bubbles: true, cancelable: true, detail: { elt: els.queryForm } }));
  appendCancelledBlock(elapsed);
  keepLatestOutputVisible();
  announceQueryStatus("Query cancelled");
}

function appendCancelledBlock(elapsed) {
  const tool = currentTool() || { name: "", badge: "", colorClass: "" };
  const state = composerValues();
  const fields = collectCurrentConnection().fields || {};
  const article = document.createElement("article");
  article.className = "cmd-block";
  article.dataset.outputFormat = els.composerFormat.value;
  article.dataset.tool = tool.name;
  article.dataset.query = state.query || "";
  article.dataset.state = JSON.stringify(state);
  article.dataset.resultStatus = "cancelled";
  article.dataset.connectionId = els.formConnectionID.value;
  article.dataset.leaseId = els.formLeaseID.value;
  article.dataset.connectionName = els.formConnectionName.value;
  article.dataset.host = els.formHost.value;
  article.dataset.port = els.formPort.value;
  article.dataset.mode = els.formMode.value;
  article.dataset.environment = els.formEnvironment.value;
  article.dataset.fields = JSON.stringify(fields);
  const format = els.composerFormat.value || "raw";
  article.innerHTML =
    `<header class="cmd-header">` +
    `<time class="cmd-time">${new Date().toLocaleTimeString()}</time>` +
    `<span class="tool-badge ${escapeHTML(tool.colorClass || "")}">${escapeHTML(tool.badge || tool.name || "")}</span>` +
    `<code class="cmd-query">${escapeHTML(state.query || "")}</code>` +
    `<div class="cmd-meta">` +
    `<span>${escapeHTML(els.formConnectionName.value || "")}</span>` +
    `<span>${elapsed}ms</span>` +
    `<span class="cmd-format">${escapeHTML(format.toUpperCase())}</span>` +
    `<span class="cmd-status">CANCELLED</span>` +
    `</div>` +
    `</header>` +
    `<div class="cmd-output"><div class="virtual-output" data-virtual-kind="error">` +
    `<script type="application/json" class="virtual-output-data">${JSON.stringify({ kind: "error", text: "cancelled by user" })}</script>` +
    `<div class="virtual-scroll" role="region" aria-label="Query output" tabindex="0"></div>` +
    `</div></div>`;
  els.outputArea.append(article);
}

function updateOutputState() {
  const hasOutputs = els.outputArea.querySelectorAll(".cmd-block").length > 0;
  els.emptyState.hidden = hasOutputs;
  els.clearOutputs.hidden = !hasOutputs;
}

function markLatestOutput() {
  const blocks = [...els.outputArea.querySelectorAll(".cmd-block")];
  blocks.forEach((block) => block.classList.remove("is-latest")); blocks.at(-1)?.classList.add("is-latest");
}


function initializeVirtualOutputs() {
  els.outputArea.querySelectorAll(".virtual-output:not([data-virtual-ready])").forEach((root) => {
    const source = root.querySelector(".virtual-output-data");
    let payload;
    try { payload = JSON.parse(source?.textContent || "{}"); }
    catch { payload = { kind: "error", text: "Unable to render output" }; }
    payload = normalizePayload(payload);
    source?.remove();
    const block = root.closest(".cmd-block");
    const model = payload.kind === "table" ? createTableModel(root, payload) : payload.kind === "browse" ? createBrowseModel(root, payload) : createTextModel(root, payload);
    outputModels.set(block, model);
    root.setAttribute("data-virtual-ready", "true");
    model.viewport.style.height = `${virtualHeight(model.contentHeight, false)}px`;
    model.render();
    requestAnimationFrame(() => {
      adjustViewportForScrollbar(model, false);
      scheduleVirtualRender(model);
    });
  });
}

function resizeVirtualOutput(block, expanded) {
  const model = outputModels.get(block);
  if (!model) return;
  model.viewport.style.height = `${virtualHeight(model.contentHeight, expanded)}px`;
  scheduleVirtualRender(model);
  adjustViewportForScrollbar(model, expanded);
}

function clearOutputs() {
  els.outputArea.querySelectorAll(".cmd-block").forEach((block) => outputModels.get(block)?.destroy?.());
  els.outputArea.replaceChildren();
  updateOutputState();
}

function removeOutput(block) {
  outputModels.get(block)?.destroy?.();
  outputModels.delete(block);
  block.remove();
  markLatestOutput();
  updateOutputState();
}

async function copyOutput(block, button) {
  const text = outputModels.get(block)?.copyText() || block.querySelector(".cmd-output")?.innerText || "";
  await copyToClipboard(text);
  button.classList.add("is-confirmed");
  window.setTimeout(() => button.classList.remove("is-confirmed"), 1200);
}

async function rerunQuery(block) {
  let state = {};
  try { state = JSON.parse(block.dataset.state || "{}"); } catch { state = {}; }
  if (!Object.keys(state).length && block.dataset.query) state.query = block.dataset.query;
  const format = block.dataset.outputFormat;
  // Order matters: connection must be applied before renderComposer so
  // dynamic options (namespaces/sets) load against the block's connection.
  if (els.composerTool.value !== block.dataset.tool) { els.composerTool.value = block.dataset.tool; await handleToolChange(); }
  applyBlockConnection(block);
  setConnectionStatus("reachable", "Connected");
  await renderComposer(state);
  if ([...els.composerFormat.options].some((option) => option.value === format)) els.composerFormat.value = format;
  syncFormatPresentation();
  els.queryForm.requestSubmit();
}

// Runs an inspect command (e.g. HGETALL <key>) from a BROWSE block against the
// block's own connection, reusing the same connection replay as rerunQuery.
async function inspectBrowseKey(block, command) {
  if (els.composerTool.value !== block.dataset.tool) { els.composerTool.value = block.dataset.tool; await handleToolChange(); }
  applyBlockConnection(block);
  setConnectionStatus("reachable", "Connected");
  await renderComposer({ query: command });
  els.composerFormat.value = "raw";
  syncFormatPresentation();
  els.queryForm.requestSubmit();
}

function applyBlockConnection(block) {
  const data = block.dataset;
  const connectionId = data.connectionId || "";
  const saved = connectionId && connectionsForTool().some((item) => item.id === connectionId);
  els.composerConnection.value = saved ? connectionId : NEW_CONNECTION;
  els.connectionSelect.value = els.composerConnection.value;
  els.connectionName.value = data.connectionName || "";
  els.host.value = data.host || "";
  els.port.value = data.port || "";
  els.mode.value = data.mode || "single";
  els.environment.value = data.environment === "stage" ? "stage" : "prod";
  els.environment.disabled = Boolean(saved && selectedConnection()?.preset);
  renderPluginFields(parseFieldsJSON(data.fields));
  syncFormState();
  // Override syncFormState: replay the original lease so pool/preset lookups match the first run.
  els.formConnectionID.value = connectionId || els.formConnectionID.value;
  els.formLeaseID.value = data.leaseId || els.formLeaseID.value;
  els.formConnectionName.value = data.connectionName || els.formConnectionName.value;
  els.formHost.value = data.host || els.formHost.value;
  els.formPort.value = data.port || els.formPort.value;
  els.formMode.value = data.mode || els.formMode.value;
  els.formEnvironment.value = data.environment || els.formEnvironment.value;
  syncEnvironmentBadge();
  renderConnectionRail();
}

function parseFieldsJSON(raw) {
  if (!raw) return {};
  try { const parsed = JSON.parse(raw); return parsed && typeof parsed === "object" ? parsed : {}; }
  catch { return {}; }
}

async function handleToolChange(preferredConnection = "") {
  saveDraft(); activeToolName = els.composerTool.value; syncToolPresentation(); renderFormats();
  const selected = renderConnectionOptions(preferredConnection); await applyConnection(selected); syncFormState();
}

function refreshOutputAfterSwap() {
  if (outputRefreshPending) return;
  outputRefreshPending = true;
  window.requestAnimationFrame(() => {
    outputRefreshPending = false;
    initializeVirtualOutputs();
    updateOutputState();
    markLatestOutput();
    els.outputArea.lastElementChild?.scrollIntoView({ behavior: "smooth", block: "nearest" });
  });
}

renderToolOptions();
activeToolName = els.composerTool.value;
syncToolPresentation(); renderFormats();
applyConnection(renderConnectionOptions());
setConnectionStatus("disconnected", "Enter connection details"); updateOutputState();
refreshActiveConnections();
setInterval(refreshActiveConnections, 30_000);
applyWorkspaceLayout();
syncPanelControls();
refreshCustomSelects(document);

els.composerTool.addEventListener("change", () => handleToolChange());
els.toolSelect.addEventListener("change", () => { els.composerTool.value = els.toolSelect.value; handleToolChange(); });
els.composerConnection.addEventListener("change", () => applyConnection(els.composerConnection.value));
els.connectionSelect.addEventListener("change", () => applyConnection(els.connectionSelect.value));
els.newConnection.addEventListener("click", () => applyConnection(NEW_CONNECTION));
els.saveConnection.addEventListener("click", saveCurrentConnection);
els.duplicateConnection.addEventListener("click", duplicateCurrentConnection);
els.deleteConnection.addEventListener("click", deleteSelectedConnection);
els.connectionList.addEventListener("click", async (event) => {
  const item = event.target.closest("[data-connection-id]"); if (!item) return;
  await selectAndConnect(item.dataset.connectionId, item.dataset.connectionTool);
});
els.toggleLeftPanel.addEventListener("click", () => togglePanel("left"));
els.toggleRightPanel.addEventListener("click", () => togglePanel("right"));
els.leftSidebarResizer.addEventListener("pointerdown", (event) => startSidebarResize("left", event));
els.rightSidebarResizer.addEventListener("pointerdown", (event) => startSidebarResize("right", event));
els.leftSidebarResizer.addEventListener("keydown", (event) => resizeSidebarWithKeyboard("left", event));
els.rightSidebarResizer.addEventListener("keydown", (event) => resizeSidebarWithKeyboard("right", event));
els.panelBackdrop.addEventListener("click", closePanels);
document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  if (queryInFlight && !isEditableTarget(event.target)) {
    event.preventDefault();
    cancelRunningQuery();
    return;
  }
  closePanels();
});
els.composerFormat.addEventListener("change", syncFormatPresentation);
els.connectionName.addEventListener("input", () => { syncFormState(); renderConnectionRail(); invalidateConnectionStatus(); });
[els.host, els.port, els.mode].forEach((element) => {
  const changed = () => { syncFormState(); invalidateConnectionStatus(); };
  element.addEventListener("input", changed); element.addEventListener("change", changed);
});
// Environment doesn't affect reachability, so switching it takes effect on the
// next query without forcing a "reconnect" prompt like host/port/mode do.
els.environment.addEventListener("change", syncFormState);

els.connect.addEventListener("click", connectSelectedConnection);
els.disconnect.addEventListener("click", async () => {
  syncFormState();
  try {
    await fetch("/disconnect", { method: "POST", headers: { "Content-Type": "application/x-www-form-urlencoded" }, body: connectionParams() });
  } finally {
    await refreshActiveConnections();
    setConnectionStatus("disconnected", "Disconnected; connection details remain editable");
  }
});

els.queryForm.addEventListener("submit", (event) => {
  if (queryInFlight) { event.preventDefault(); return; }
  syncFormState(); syncExpressionInput();
  const message = validateExpression();
  if (message) { event.preventDefault(); els.actionPanel.hidden = false; renderExpressionPanel(); els.actionPanel.querySelector(".expression-message").textContent = message; return; }
  const state = composerValues();
  const browseMatch = /^BROWSE\s+(.+?)(?:\s+LIMIT\s+\d+)?$/i.exec(String(state.query || "").trim());
  if (browseMatch) lastBrowsePattern = browseMatch[1].trim() || "*";
  addHistory(els.composerTool.value, composerSummary(), state); setRunning(true);
  announceQueryStatus("Query running");
});

els.runQuery.addEventListener("click", (event) => {
  if (!queryInFlight || cancelInFlight) return;
  event.preventDefault();
  cancelRunningQuery();
});

els.browseKeys.addEventListener("click", () => {
  const queryInput = els.pluginComposer.querySelector('[data-composer-name="query"]');
  if (queryInput) {
    queryInput.value = `BROWSE ${lastBrowsePattern}`;
    queryInput.dispatchEvent(new Event("input", { bubbles: true }));
    saveDraft();
  }
  els.queryForm.requestSubmit();
});

els.historySearch.addEventListener("input", renderHistory);
document.querySelectorAll("[data-history-tool]").forEach((button) => button.addEventListener("click", () => {
  historyToolFilter = button.dataset.historyTool;
  document.querySelectorAll("[data-history-tool]").forEach((item) => item.classList.toggle("is-active", item === button));
  renderHistory();
}));
els.clearOutputs.addEventListener("click", clearOutputs);
window.addEventListener("resize", () => {
  if (!leftMobileQuery.matches) els.operatorMain.classList.remove("mobile-left-open");
  if (!rightMobileQuery.matches) els.operatorMain.classList.remove("mobile-right-open");
  syncPanelControls();
  els.outputArea.querySelectorAll(".cmd-block").forEach((block) => resizeVirtualOutput(block, block.querySelector(".cmd-output")?.classList.contains("expanded")));
});

document.body.addEventListener("htmx:afterRequest", (event) => {
  if (event.detail.elt !== els.queryForm) return;
  setRunning(false); refreshOutputAfterSwap();
});
document.body.addEventListener("htmx:oobAfterSwap", refreshOutputAfterSwap);

els.outputArea.addEventListener("click", async (event) => {
  const block = event.target.closest(".cmd-block"); if (!block) return;
  if (event.target.closest(".cmd-rerun")) return rerunQuery(block);
  if (event.target.closest(".cmd-close")) return removeOutput(block);
  if (event.target.closest(".cmd-copy")) return copyOutput(block, event.target.closest(".cmd-copy"));
  const browseKey = event.target.closest(".browse-key");
  if (browseKey?.dataset.inspect) return inspectBrowseKey(block, browseKey.dataset.inspect);
  const toggle = event.target.closest(".cmd-toggle");
  if (toggle) {
    const output = block.querySelector(".cmd-output");
    const expanded = output?.classList.toggle("expanded") || false;
    toggle.setAttribute("aria-expanded", String(expanded));
    toggle.setAttribute("aria-label", expanded ? "Collapse output" : "Expand output");
    toggle.title = expanded ? "Collapse output" : "Expand output";
    block.classList.toggle("is-expanded", expanded);
    resizeVirtualOutput(block, expanded);
  }
});
