import { chunkText, fitHeight, previewText, tableText, visibleRange } from "./virtual_output.mjs";

export function scheduleVirtualRender(model) {
  if (model.renderPending) return;
  model.renderPending = true;
  window.requestAnimationFrame(() => { model.renderPending = false; model.render(); });
}

export function virtualHeight(contentHeight, expanded, viewportHeight = globalThis.innerHeight) {
  const limit = expanded ? viewportHeight * .7 : 260;
  return fitHeight(contentHeight, limit);
}

export function createTextModel(root, payload) {
  const viewport = root.querySelector(".virtual-scroll");
  const items = chunkText(payload.text || "");
  const itemHeight = 19;
  const canvas = document.createElement("div");
  const content = document.createElement("pre");
  canvas.className = "virtual-text-canvas";
  content.className = `virtual-text-content ${payload.kind === "error" ? "err-out" : payload.kind === "json" ? "json-out" : "raw-out"}`;
  canvas.style.height = `${Math.max(itemHeight, items.length * itemHeight)}px`;
  canvas.style.width = `${Math.max(1, items.reduce((longest, line) => Math.max(longest, line.length), 0) * 7.3 + 20)}px`;
  canvas.append(content);
  viewport.classList.add("virtual-text-scroll");
  viewport.append(canvas);
  const model = {
    viewport, contentHeight: Math.max(itemHeight, items.length * itemHeight), copyText: () => payload.text || "",
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
  const contentHeight = headerHeight + Math.max(1, rows.length) * rowHeight;
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
        rowElement.style.width = `${canvasWidth}px`;
        for (let column = columns.start; column < columns.end; column++) {
          const cell = document.createElement("div");
          const value = rows[row]?.[column] || "";
          cell.className = `virtual-table-cell${row % 2 ? " is-even" : ""}`;
          cell.style.left = `${column * columnWidth}px`;
          cell.style.width = `${columnWidth}px`;
          cell.setAttribute("role", "gridcell");
          cell.setAttribute("aria-colindex", String(column + 1));
          cell.textContent = previewText(value);
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
