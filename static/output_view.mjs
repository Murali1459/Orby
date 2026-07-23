import { chunkText, fitHeight, previewText, tableText, visibleRange } from "./virtual_output.mjs";

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
  return fitHeight(contentHeight, limit);
}

export function adjustViewportForScrollbar(model, expanded = false) {
  const gap = 14;
  const sb = scrollbarHeight(model.viewport);
  if (sb > 0) {
    model.viewport.style.height = `${virtualHeight(model.contentHeight + sb + gap, expanded)}px`;
  }
}

export function createTextModel(root, payload) {
  const viewport = root.querySelector(".virtual-scroll");
  const items = chunkText(payload.text || "");
  const itemHeight = 19;
  const canvas = document.createElement("div");
  const content = document.createElement("pre");
  canvas.className = "virtual-text-canvas";
  content.className = `virtual-text-content ${payload.kind === "error" ? "err-out" : payload.kind === "json" ? "json-out" : "raw-out"}`;
  const contentHeight = Math.max(itemHeight, items.length * itemHeight);
  const scrollbarBuffer = 20;
  canvas.style.height = `${contentHeight + scrollbarBuffer}px`;
  canvas.style.width = `${Math.max(1, items.reduce((longest, line) => Math.max(longest, line.length), 0) * 7.3 + 20)}px`;
  canvas.append(content);
  viewport.classList.add("virtual-text-scroll");
  viewport.append(canvas);
  const model = {
    viewport, contentHeight: contentHeight + scrollbarBuffer, copyText: () => payload.text || "",
    render() {
      const range = visibleRange(items.length, itemHeight, viewport.scrollTop, viewport.clientHeight);
      content.style.transform = `translateY(${range.start * itemHeight}px)`;
      content.textContent = items.slice(range.start, range.end).join("\n");
    },
  };
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
      <div class="cell-popup-body"><pre class="cell-popup-pre${isJson ? " is-json" : ""}">${displayText === null ? "<span class='cell-popup-null'>NULL</span>" : escapeHtml(displayText)}</pre></div>
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

function escapeHtml(str) {
  return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

async function copyToClipboard(text) {
  try { await navigator.clipboard.writeText(text); }
  catch {
    const textarea = document.createElement("textarea");
    textarea.value = text;
    textarea.style.cssText = "position:fixed;left:-9999px;top:-9999px;opacity:0";
    document.body.append(textarea);
    document.activeElement?.blur();
    textarea.focus();
    textarea.select();
    document.execCommand("copy");
    textarea.remove();
  }
}

export function createTableModel(root, payload) {
  const viewport = root.querySelector(".virtual-scroll");
  const heads = Array.isArray(payload.heads) ? payload.heads : [];
  const rows = Array.isArray(payload.rows) ? payload.rows : [];
  const rowHeight = 34;
  const headerHeight = 32;
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
