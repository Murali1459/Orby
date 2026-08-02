import { escapeHTML } from "./app_utils.mjs";

export function visibleRange(count, itemHeight, scrollTop, viewportHeight, overscan = 8) {
  if (count <= 0) return { start: 0, end: 0 };
  const first = Math.floor(Math.max(0, scrollTop) / itemHeight);
  const visible = Math.ceil(Math.max(itemHeight, viewportHeight) / itemHeight);
  return {
    start: Math.max(0, first - overscan),
    end: Math.min(count, first + visible + overscan),
  };
}

export function normalizePayload(payload) {
  if (payload?.kind === "table" && (!Array.isArray(payload.rows) || payload.rows.length === 0)) {
    return { kind: "raw", text: "No records" };
  }
  return payload;
}

export function chunkText(text, maxLength = 4096) {
  const size = Math.max(1, maxLength);
  return String(text ?? "").split("\n").flatMap((line) => {
    if (!line.length) return [""];
    const chunks = [];
    for (let offset = 0; offset < line.length; offset += size) chunks.push(line.slice(offset, offset + size));
    return chunks;
  });
}

export function tableText(heads, rows) {
  return [heads, ...rows].map((row) => row.join("\t")).join("\n");
}

export function previewText(value, maxLength = 240) {
  const text = String(value ?? "");
  return text.length > maxLength ? `${text.slice(0, maxLength)}… (${text.length} chars)` : text;
}

const jsonTokenRe = /("(?:[^"\\]|\\.)*")\s*(:)?|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)\b|(true|false|null)\b|([{}[\],:])/g;

export function highlightJson(text) {
  return String(text ?? "").replace(jsonTokenRe, (match, str, colon, num, bool, punct) => {
    if (str) return `<span class="js-str">${escapeHTML(str)}</span>${colon ? ":" : ""}`;
    if (num) return `<span class="js-num">${match}</span>`;
    if (bool) return `<span class="js-bool">${match}</span>`;
    if (punct) return `<span class="js-punct">${match}</span>`;
    return escapeHTML(match);
  });
}
