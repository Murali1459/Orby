import { buildPortableScript } from "./python_portable_utils.mjs";

const source = document.getElementById("source");
const highlight = document.getElementById("highlight");
const output = document.getElementById("output");
const run = document.getElementById("run");
const copyPortable = document.getElementById("copy-portable");
const portableConnections = document.getElementById("portable-connections");
const runState = document.getElementById("run-state");
const runStateText = document.getElementById("run-state-text");
const editor = source.closest(".editor");
const presetFilter = document.getElementById("preset-filter");
const presetList = document.getElementById("preset-list");

const keywords = new Set("and as assert async await break class continue def del elif else except False finally for from global if import in is lambda None nonlocal not or pass raise return True try while with yield match case".split(" "));
const builtins = new Set("abs all any bool bytes callable chr dict dir enumerate filter float format frozenset getattr hasattr hash help hex id input int isinstance issubclass iter len list map max memoryview min next object oct open ord pow print property range repr reversed round set setattr slice sorted str sum super tuple type vars zip __import__".split(" "));

function escapeCode(value) {
  return value.replace(/[&<>]/g, (char) => char === "&" ? "&amp;" : char === "<" ? "&lt;" : "&gt;");
}

function tokenClass(token) {
  if (token[0] === "#") return "tok-comment";
  if (token[0] === "'" || token[0] === '"') return "tok-string";
  if (token[0] === "@") return "tok-decorator";
  if (/^\d/.test(token)) return "tok-number";
  if (keywords.has(token)) return "tok-keyword";
  if (builtins.has(token)) return "tok-builtin";
  return "";
}

function highlightPython(value) {
  const pattern = /(#[^\n]*|'''[\s\S]*?(?:'''|$)|"""[\s\S]*?(?:"""|$)|'(?:\\.|[^'\\\n])*'|"(?:\\.|[^"\\\n])*"|@[A-Za-z_]\w*|\b(?:and|as|assert|async|await|break|class|continue|def|del|elif|else|except|False|finally|for|from|global|if|import|in|is|lambda|None|nonlocal|not|or|pass|raise|return|True|try|while|with|yield|match|case|abs|all|any|bool|bytes|callable|chr|dict|dir|enumerate|filter|float|format|frozenset|getattr|hasattr|hash|help|hex|id|input|int|isinstance|issubclass|iter|len|list|map|max|memoryview|min|next|object|oct|open|ord|pow|print|property|range|repr|reversed|round|set|setattr|slice|sorted|str|sum|super|tuple|type|vars|zip|__import__)\b|\b\d+(?:\.\d+)?\b)/g;
  let html = "";
  let last = 0;
  let match;
  while ((match = pattern.exec(value)) !== null) {
    html += escapeCode(value.slice(last, match.index));
    html += `<span class="${tokenClass(match[0])}">${escapeCode(match[0])}</span>`;
    last = pattern.lastIndex;
  }
  html += escapeCode(value.slice(last));
  return html + (value.endsWith("\n") ? " " : "");
}

function syncHighlight() {
  highlight.innerHTML = highlightPython(source.value);
  highlight.scrollTop = source.scrollTop;
  highlight.scrollLeft = source.scrollLeft;
}

function presetVariable(name) {
  let variable = String(name || "preset").toLowerCase().replace(/[^a-z0-9_]+/g, "_").replace(/^_+|_+$/g, "");
  if (!variable || /^\d/.test(variable) || keywords.has(variable)) variable = `preset_${variable || "client"}`;
  return variable;
}

function insertPreset(item) {
  if (!item) return;
  const assignment = `${presetVariable(item.dataset.presetName)} = client(${JSON.stringify(item.dataset.presetId)})`;
  const start = source.selectionStart;
  const end = source.selectionEnd;
  const before = source.value.slice(0, start);
  const after = source.value.slice(end);
  const insertion = `${before && !before.endsWith("\n") ? "\n" : ""}${assignment}${after && !after.startsWith("\n") ? "\n" : ""}`;
  source.setRangeText(insertion, start, end, "end");
  source.focus();
  source.dispatchEvent(new Event("input"));
}

function presetItemByID(id) {
  return [...presetList.querySelectorAll(".preset-item")].find((item) => item.dataset.presetId === id);
}

function appendOutput(text) {
  output.textContent += text;
  output.scrollTop = output.scrollHeight;
}

function setRunState(state, label) {
  runState.dataset.state = state;
  runStateText.textContent = label;
  output.dataset.state = state;
}

async function copyPortableScript() {
  const script = buildPortableScript(portableConnections.value, source.value);
  try {
    await navigator.clipboard.writeText(script);
    copyPortable.textContent = "Copied";
  } catch {
    const helper = document.createElement("textarea");
    helper.value = script;
    helper.setAttribute("readonly", "");
    helper.style.position = "fixed";
    helper.style.opacity = "0";
    document.body.appendChild(helper);
    helper.select();
    const copied = document.execCommand("copy");
    helper.remove();
    copyPortable.textContent = copied ? "Copied" : "Copy failed";
  }
  setTimeout(() => { copyPortable.textContent = "Copy portable"; }, 1600);
}

let outputDecoder = new TextDecoder();

function decodeOutputChunk(encoded, final = false) {
  if (!encoded) return outputDecoder.decode(new Uint8Array(), { stream: !final });
  const binary = atob(encoded);
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
  return outputDecoder.decode(bytes, { stream: !final });
}

function dispatchSSE(block) {
  let event = "message";
  const data = [];
  for (const line of block.split(/\r?\n/)) {
    if (line.startsWith("event:")) event = line.slice(6).trim();
    if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
  }
  if (!data.length) return;
  const payload = JSON.parse(data.join("\n"));
  if (event === "start") {
    outputDecoder = new TextDecoder();
    output.textContent = "";
    setRunState("running", "Running");
  }
  if (event === "output") appendOutput(decodeOutputChunk(payload.chunkBase64));
  if (event === "error") {
    setRunState("error", "Failed");
    appendOutput(`${output.textContent && !output.textContent.endsWith("\n") ? "\n" : ""}${payload.message || "Execution failed"}\n`);
  }
  if (event === "done") {
    appendOutput(decodeOutputChunk("", true));
    if (payload.truncated) appendOutput(`${output.textContent && !output.textContent.endsWith("\n") ? "\n" : ""}[output truncated]\n`);
    if (payload.ok && !output.textContent) output.textContent = "Completed.";
    setRunState(payload.ok ? "success" : "error", payload.ok ? "Complete" : "Failed");
  }
}

async function consumeSSE(response) {
  if (!response.ok || !response.body) throw new Error(`Execution request failed (${response.status})`);
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let pending = "";
  for (;;) {
    const { value, done } = await reader.read();
    pending += decoder.decode(value || new Uint8Array(), { stream: !done });
    let boundary;
    while ((boundary = pending.indexOf("\n\n")) >= 0) {
      dispatchSSE(pending.slice(0, boundary));
      pending = pending.slice(boundary + 2);
    }
    if (done) break;
  }
  if (pending.trim()) dispatchSSE(pending);
}

async function execute() {
  run.disabled = true;
  run.textContent = "Running…";
  setRunState("running", "Connecting");
  output.textContent = "Connecting…";
  try {
    const response = await fetch(location.pathname, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
      body: JSON.stringify({ script: source.value }),
    });
    if (response.headers.get("Content-Type")?.includes("text/event-stream")) {
      await consumeSSE(response);
    } else {
      const result = await response.json();
      output.textContent = (result.output || "") + (result.error ? `\n${result.error}` : "");
      setRunState(response.ok ? "success" : "error", response.ok ? "Complete" : "Failed");
    }
  } catch (error) {
    output.textContent = String(error);
    setRunState("error", "Failed");
  } finally {
    run.disabled = false;
    run.textContent = "Run script";
  }
}

run.addEventListener("click", execute);
copyPortable.addEventListener("click", copyPortableScript);
presetFilter?.addEventListener("input", () => {
  const query = presetFilter.value.trim().toLowerCase();
  presetList.querySelectorAll(".preset-item").forEach((item) => {
    item.hidden = Boolean(query) && !`${item.dataset.presetName} ${item.dataset.presetId} ${item.textContent}`.toLowerCase().includes(query);
  });
});
presetList?.addEventListener("click", (event) => insertPreset(event.target.closest(".preset-item")));
presetList?.addEventListener("dragstart", (event) => {
  const item = event.target.closest(".preset-item");
  if (!item) return;
  event.dataTransfer.effectAllowed = "copy";
  event.dataTransfer.setData("application/x-orby-preset", item.dataset.presetId);
  event.dataTransfer.setData("text/plain", `client(${JSON.stringify(item.dataset.presetId)})`);
  item.classList.add("is-dragging");
});
presetList?.addEventListener("dragend", (event) => event.target.closest(".preset-item")?.classList.remove("is-dragging"));
editor.addEventListener("dragover", (event) => {
  if (!event.dataTransfer.types.includes("application/x-orby-preset")) return;
  event.preventDefault();
  event.dataTransfer.dropEffect = "copy";
  editor.classList.add("is-drag-over");
});
editor.addEventListener("dragleave", (event) => {
  if (!editor.contains(event.relatedTarget)) editor.classList.remove("is-drag-over");
});
editor.addEventListener("drop", (event) => {
  event.preventDefault();
  editor.classList.remove("is-drag-over");
  insertPreset(presetItemByID(event.dataTransfer.getData("application/x-orby-preset")));
});
source.addEventListener("input", syncHighlight);
source.addEventListener("scroll", syncHighlight);
source.addEventListener("keydown", (event) => {
  if (event.key === "Tab") {
    event.preventDefault();
    const start = source.selectionStart;
    const end = source.selectionEnd;
    source.setRangeText("    ", start, end, "end");
    source.dispatchEvent(new Event("input"));
    return;
  }
  if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
    event.preventDefault();
    execute();
  }
});
syncHighlight();
