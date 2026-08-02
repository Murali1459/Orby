export function clamp(value, minimum, maximum) {
  return Math.min(maximum, Math.max(minimum, value));
}

export function escapeHTML(value) {
  return String(value ?? "").replaceAll("&", "&amp;").replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#39;");
}

export function sameSet(left, right) {
  return left.size === right.size && [...left].every((value) => right.has(value));
}

export async function copyToClipboard(text) {
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
