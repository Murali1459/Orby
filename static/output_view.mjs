import { chunkText, previewText, tableText, visibleRange, highlightJson } from "./virtual_output.mjs";
import { escapeHTML, copyToClipboard } from "./app_utils.mjs";

export function scheduleVirtualRender(model) {
  if (model.renderPending) return;
  model.renderPending = true;
  window.requestAnimationFrame(() => { model.renderPending = false; model.render(); });
}

function scrollbarHeight(viewport) {
  return Math.max(0, viewport.offsetHeight - viewport.clientHeight);
}

export function virtualHeight(contentHeight, expanded, viewportHeight = globalThis.innerHeight) {
  const limit = expanded ? viewportHeight * .7 : 260;
  return Math.min(Math.max(0, contentHeight), Math.max(0, limit));
}

export function adjustViewportForScrollbar(model, expanded = false) {
  const gap = 14;
  const sb = scrollbarHeight(model.viewport);
  if (sb > 0) {
    model.viewport.style.height = `${virtualHeight(model.contentHeight + sb + gap, expanded)}px`;
  }
}

export function jsonLineStructure(items) {
  const structure = items.map(() => ({ opens: false, closeIndex: -1 }));
  const stack = [];
  items.forEach((line, index) => {
    const trimmed = line.trim();
    if (trimmed.endsWith("{") || trimmed.endsWith("[")) {
      structure[index].opens = true;
      stack.push(index);
    } else if ((trimmed.startsWith("}") || trimmed.startsWith("]")) && stack.length) {
      structure[stack.pop()].closeIndex = index;
    }
  });
  return structure;
}

export function visibleJsonLines(structure, collapsed) {
  const visible = [];
  for (let index = 0; index < structure.length; index++) {
    visible.push(index);
    if (structure[index].opens && collapsed.has(index) && structure[index].closeIndex > index) {
      index = structure[index].closeIndex;
    }
  }
  return visible;
}

export function createTextModel(root, payload) {
  const viewport = root.querySelector(".virtual-scroll");
  const items = chunkText(payload.text || "");
  const itemHeight = 19;
  const canvas = document.createElement("div");
  const content = document.createElement("pre");
  canvas.className = "virtual-text-canvas";
  content.className = `virtual-text-content ${payload.kind === "error" ? "err-out" : payload.kind === "json" ? "json-out" : "raw-out"}`;
  const scrollbarBuffer = 20;
  const structure = payload.kind === "json" ? jsonLineStructure(items) : null;
  const collapsed = new Set();
  const computeVisible = () => structure ? visibleJsonLines(structure, collapsed) : items.map((_, index) => index);
  let visibleLines = computeVisible();
  let contentHeight = Math.max(itemHeight, visibleLines.length * itemHeight) + scrollbarBuffer;
  canvas.style.height = `${contentHeight}px`;
  canvas.style.width = `${Math.max(1, items.reduce((longest, line) => Math.max(longest, line.length), 0) * 7.3 + 20)}px`;
  canvas.append(content);
  viewport.classList.add("virtual-text-scroll");
  viewport.append(canvas);
  const model = {
    viewport, contentHeight, copyText: () => payload.text || "",
    toggleCollapse(index) {
      if (!structure || !structure[index]?.opens) return;
      if (collapsed.has(index)) collapsed.delete(index); else collapsed.add(index);
      visibleLines = computeVisible();
      contentHeight = Math.max(itemHeight, visibleLines.length * itemHeight) + scrollbarBuffer;
      model.contentHeight = contentHeight;
      canvas.style.height = `${contentHeight}px`;
      const expanded = viewport.closest(".cmd-output")?.classList.contains("expanded") || false;
      viewport.style.height = `${virtualHeight(contentHeight, expanded)}px`;
      adjustViewportForScrollbar(model, expanded);
      scheduleVirtualRender(model);
    },
    render() {
      const range = visibleRange(visibleLines.length, itemHeight, viewport.scrollTop, viewport.clientHeight);
      content.style.transform = `translateY(${range.start * itemHeight}px)`;
      if (structure) {
        const html = [];
        for (let position = range.start; position < range.end; position++) {
          const index = visibleLines[position];
          const line = items[index];
          const info = structure[index];
          if (info.opens) {
            const isCollapsed = collapsed.has(index);
            const closer = line.trimEnd().endsWith("{") ? "}" : "]";
            const summary = isCollapsed ? `${line} … ${closer}` : line;
            const toggle = `<button type="button" class="json-toggle" data-line="${index}" aria-expanded="${!isCollapsed}" aria-label="${isCollapsed ? "Expand" : "Collapse"}">${isCollapsed ? "▸" : "▾"}</button>`;
            html.push(`${toggle}${highlightJson(summary)}`);
          } else {
            html.push(`<span class="json-toggle-spacer"></span>${highlightJson(line)}`);
          }
        }
        content.innerHTML = html.join("\n");
      } else {
        content.textContent = visibleLines.slice(range.start, range.end).map((index) => items[index]).join("\n");
      }
    },
  };
  if (structure) {
    content.addEventListener("click", (event) => {
      const toggle = event.target.closest(".json-toggle");
      if (toggle) model.toggleCollapse(Number(toggle.dataset.line));
    });
  }
  viewport.addEventListener("scroll", () => scheduleVirtualRender(model), { passive: true });
  const resizeObserver = new ResizeObserver(() => scheduleVirtualRender(model));
  resizeObserver.observe(viewport);
  model.destroy = () => resizeObserver.disconnect();
  return model;
}

function tryPrettyJson(raw) {
  if (typeof raw !== "string") return null;
  const trimmed = raw.trim();
  if (trimmed[0] !== "{" && trimmed[0] !== "[") return null;
  try { return JSON.stringify(JSON.parse(trimmed), null, 2); } catch { return null; }
}

const ICON_COPY = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>`;
const ICON_CHECK = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#4ade80" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>`;
const ICON_MINIFY = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/><line x1="10" y1="14" x2="21" y2="3"/><line x1="3" y1="21" x2="14" y2="10"/></svg>`;
const ICON_PRETTIFY = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/><line x1="21" y1="3" x2="14" y2="10"/><line x1="3" y1="21" x2="10" y2="14"/></svg>`;
const ICON_CLOSE = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>`;

function openCellPopup(columnName, value) {
  const existing = document.getElementById("cell-value-popup");
  if (existing) existing.remove();
  const raw = value === null || value === undefined ? null : String(value);
  const pretty = raw !== null ? tryPrettyJson(raw) : null;
  const displayText = pretty ?? raw;
  const isJson = pretty !== null;
  const overlay = document.createElement("div");
  overlay.id = "cell-value-popup";
  overlay.className = "cell-popup-overlay";
  overlay.innerHTML = `
    <div class="cell-popup" role="dialog" aria-modal="true" aria-label="Cell value">
      <div class="cell-popup-header">
        <span class="cell-popup-col">${columnName}${isJson ? ' <span class="cell-popup-badge">JSON</span>' : ""}</span>
        <div class="cell-popup-actions">
          ${isJson ? `<button class="cell-popup-toggle" title="Minify" aria-label="Minify JSON">${ICON_MINIFY}</button>` : ""}
          <button class="cell-popup-copy" title="Copy" aria-label="Copy value">${ICON_COPY}</button>
          <button class="cell-popup-close" title="Close" aria-label="Close">${ICON_CLOSE}</button>
        </div>
      </div>
      <div class="cell-popup-body"><pre class="cell-popup-pre${isJson ? " is-json" : ""}">${displayText === null ? "<span class='cell-popup-null'>NULL</span>" : escapeHTML(displayText)}</pre></div>
    </div>`;
  const close = () => overlay.remove();
  overlay.addEventListener("click", (e) => { if (e.target === overlay) close(); });
  overlay.querySelector(".cell-popup-close").addEventListener("click", close);
  const pre = overlay.querySelector(".cell-popup-pre");
  const copyBtn = overlay.querySelector(".cell-popup-copy");

  if (isJson) {
    let isPretty = true;
    const minified = JSON.stringify(JSON.parse(raw));
    const toggleBtn = overlay.querySelector(".cell-popup-toggle");
    toggleBtn.addEventListener("click", () => {
      isPretty = !isPretty;
      pre.textContent = isPretty ? pretty : minified;
      toggleBtn.innerHTML = isPretty ? ICON_MINIFY : ICON_PRETTIFY;
      toggleBtn.title = isPretty ? "Minify" : "Prettify";
      toggleBtn.setAttribute("aria-label", isPretty ? "Minify JSON" : "Prettify JSON");
    });
    copyBtn.addEventListener("click", async () => {
      await copyToClipboard(isPretty ? pretty : minified);
      copyBtn.innerHTML = ICON_CHECK;
      setTimeout(() => { copyBtn.innerHTML = ICON_COPY; }, 1500);
    });
  } else {
    copyBtn.addEventListener("click", async () => {
      await copyToClipboard(raw ?? "");
      copyBtn.innerHTML = ICON_CHECK;
      setTimeout(() => { copyBtn.innerHTML = ICON_COPY; }, 1500);
    });
  }
  document.addEventListener("keydown", function esc(e) { if (e.key === "Escape") { close(); document.removeEventListener("keydown", esc); } });
  document.body.append(overlay);
  overlay.querySelector(".cell-popup").focus();
}

export function createTableModel(root, payload) {
  const viewport = root.querySelector(".virtual-scroll");
  const heads = Array.isArray(payload.heads) ? payload.heads : [];
  const rows = Array.isArray(payload.rows) ? payload.rows : [];
  const rowHeight = 30;
  const headerHeight = 30;
  const minimumColumnWidth = 180;
  const canvas = document.createElement("div");
  const header = document.createElement("div");
  const cells = document.createElement("div");
  const scrollbarBuffer = 20;
  const contentHeight = headerHeight + Math.max(1, rows.length) * rowHeight + scrollbarBuffer;
  canvas.className = "virtual-table-canvas";
  header.className = "virtual-table-header";
  cells.className = "virtual-table-cells";
  canvas.style.height = `${contentHeight}px`;
  canvas.append(header, cells);
  viewport.classList.add("virtual-table-scroll");
  viewport.setAttribute("role", "grid");
  viewport.setAttribute("aria-rowcount", String(rows.length + 1));
  viewport.setAttribute("aria-colcount", String(heads.length));
  viewport.append(canvas);
  const model = {
    viewport, contentHeight, copyText: () => tableText(heads, rows),
    render() {
      const columnWidth = Math.max(minimumColumnWidth, Math.floor(viewport.clientWidth / Math.max(1, heads.length)));
      const canvasWidth = Math.max(viewport.clientWidth, Math.max(1, heads.length) * columnWidth);
      canvas.style.width = `${canvasWidth}px`;
      header.style.width = `${canvasWidth}px`;
      const columns = visibleRange(heads.length, columnWidth, viewport.scrollLeft, viewport.clientWidth);
      const visibleTop = Math.max(0, viewport.scrollTop - headerHeight);
      const visibleHeight = Math.max(0, viewport.clientHeight - headerHeight);
      const rowRange = visibleRange(rows.length, rowHeight, visibleTop, visibleHeight);
      const headerFragment = document.createDocumentFragment();
      header.setAttribute("role", "row");
      header.setAttribute("aria-rowindex", "1");
      for (let column = columns.start; column < columns.end; column++) {
        const cell = document.createElement("div");
        cell.className = "virtual-table-heading";
        cell.style.left = `${column * columnWidth}px`;
        cell.style.width = `${columnWidth}px`;
        cell.setAttribute("role", "columnheader");
        cell.setAttribute("aria-colindex", String(column + 1));
        cell.textContent = previewText(heads[column]);
        headerFragment.append(cell);
      }
      header.replaceChildren(headerFragment);
      const cellFragment = document.createDocumentFragment();
      for (let row = rowRange.start; row < rowRange.end; row++) {
        const rowElement = document.createElement("div");
        rowElement.className = "virtual-table-row";
        rowElement.setAttribute("role", "row");
        rowElement.setAttribute("aria-rowindex", String(row + 2));
        rowElement.style.top = `${headerHeight + row * rowHeight}px`;
        rowElement.style.height = `${rowHeight}px`;
        rowElement.style.width = `${canvasWidth}px`;
        for (let column = columns.start; column < columns.end; column++) {
          const cell = document.createElement("div");
          const value = rows[row]?.[column] || "";
          cell.className = `virtual-table-cell${row % 2 ? " is-even" : ""}`;
          cell.style.left = `${column * columnWidth}px`;
          cell.style.width = `${columnWidth}px`;
          cell.setAttribute("role", "gridcell");
          cell.setAttribute("aria-colindex", String(column + 1));
          cell.style.whiteSpace = "nowrap"; cell.style.overflow = "hidden"; cell.style.textOverflow = "ellipsis"; cell.style.height = `${rowHeight}px`; cell.textContent = value;
          cell.style.cursor = "pointer";
          cell.title = "Click to view full value";
          cell.addEventListener("click", () => openCellPopup(heads[column] ?? `Column ${column + 1}`, rows[row]?.[column]));
          rowElement.append(cell);
        }
        cellFragment.append(rowElement);
      }
      cells.replaceChildren(cellFragment);
    },
  };
  viewport.addEventListener("scroll", () => scheduleVirtualRender(model), { passive: true });
  const resizeObserver = new ResizeObserver(() => scheduleVirtualRender(model));
  resizeObserver.observe(viewport);
  model.destroy = () => resizeObserver.disconnect();
  return model;
}

export function browseTTL(seconds) {
  if (!seconds || seconds < 0) return "";
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  return `${Math.floor(seconds / 3600)}h`;
}

const BROWSE_INSPECT = { string: "GET", hash: "HGETALL", list: "LRANGE", set: "SMEMBERS", zset: "ZRANGE" };

function quoteRedisArg(name) {
  const text = String(name);
  if (/^[A-Za-z0-9_.:*-]+$/.test(text)) return text;
  return `"${text.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

export function browseInspectCommand(type, name) {
  const base = BROWSE_INSPECT[type];
  if (!base) return "";
  const tail = type === "list" || type === "zset" ? " 0 -1" : "";
  return `${base} ${quoteRedisArg(name)}${tail}`;
}

export function browseCopyText(view) {
  const notes = [];
  if (view.limited) notes.push("scan limited");
  if (view.cluster) notes.push("cluster-wide");
  const suffix = notes.length ? ` (${notes.join(", ")})` : "";
  const lines = [`BROWSE ${view.pattern || "*"}`, `${view.scanned} keys scanned${suffix}`];
  for (const group of view.groups || []) {
    lines.push(`${group.prefix} (${group.total} keys)`);
    for (const key of group.keys) {
      const ttl = browseTTL(key.ttl);
      lines.push(`  ${key.name} [${key.type || "?"}]${ttl ? ` (${ttl})` : ""}`);
    }
  }
  return lines.join("\n");
}

function browseGroupHTML(group, index) {
  const rows = group.keys.map((key) => {
    const command = browseInspectCommand(key.type, key.name);
    const inspect = command ? ` data-inspect="${escapeHTML(command)}"` : "";
    const badge = key.type ? `<span class="kbt-type kbt-${escapeHTML(key.type)}">${escapeHTML(key.type)}</span>` : "";
    const ttl = browseTTL(key.ttl) ? `<span class="kbt-ttl">${escapeHTML(browseTTL(key.ttl))}</span>` : "";
    return `<div class="browse-key" role="button" tabindex="0"${inspect}><span class="browse-key-name">${escapeHTML(key.name)}</span>${badge}${ttl}</div>`;
  }).join("");
  const more = group.total > group.keys.length ? `<div class="browse-more">+${group.total - group.keys.length} more</div>` : "";
  const open = index === 0 ? " open" : "";
  return `<details class="browse-group"${open}><summary class="browse-group-head"><span class="browse-chevron" aria-hidden="true"></span><span class="browse-group-prefix">${escapeHTML(group.prefix)}</span><span class="browse-group-count">${group.total} key${group.total === 1 ? "" : "s"}</span></summary><div class="browse-group-keys">${rows}${more}</div></details>`;
}

export function createBrowseModel(root, payload) {
  const viewport = root.querySelector(".virtual-scroll");
  const view = payload.browse || { pattern: "*", groups: [], scanned: 0, limited: false };
  const canvas = document.createElement("div");
  canvas.className = "virtual-browse-canvas";
  const groups = (view.groups || []).map(browseGroupHTML).join("");
  const warn = view.limited ? `<span class="browse-warn">scan limited — narrow the pattern or raise LIMIT</span>` : "";
  const cluster = view.cluster ? `<span class="browse-cluster">cluster-wide</span>` : "";
  const stats = `<div class="browse-stats"><span>pattern <b>${escapeHTML(view.pattern)}</b></span><span><b>${view.scanned}</b> keys scanned</span>${cluster}${warn}</div>`;
  canvas.innerHTML = `${stats}<div class="browse-tree">${groups}</div>`;
  viewport.classList.add("virtual-browse-scroll");
  viewport.append(canvas);
  const contentHeight = Math.max(60, canvas.scrollHeight + 20);
  const model = {
    viewport, contentHeight,
    copyText: () => browseCopyText(view),
    render() {},
  };
  viewport.addEventListener("scroll", () => scheduleVirtualRender(model), { passive: true });
  const resizeObserver = new ResizeObserver(() => scheduleVirtualRender(model));
  resizeObserver.observe(viewport);
  model.destroy = () => resizeObserver.disconnect();
  return model;
}
