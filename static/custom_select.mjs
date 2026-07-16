let selectID = 0;
let openState = null;
const states = new WeakMap();

function enabled(options, index) {
  return Boolean(options[index] && !options[index].disabled);
}

export function moveActiveIndex(options, current, direction) {
  if (!options.length || !options.some((option) => !option.disabled)) return -1;
  let index = current;
  for (let count = 0; count < options.length; count += 1) {
    index = (index + direction + options.length) % options.length;
    if (enabled(options, index)) return index;
  }
  return -1;
}

export function findTypeaheadIndex(options, query, current = -1) {
  const normalized = String(query || "").trim().toLocaleLowerCase();
  if (!normalized) return -1;
  for (let offset = 1; offset <= options.length; offset += 1) {
    const index = (current + offset + options.length) % options.length;
    if (!options[index]?.disabled && options[index].label.toLocaleLowerCase().startsWith(normalized)) return index;
  }
  return -1;
}

function selectOptions(select) {
  return [...select.options].map((option) => ({
    label: option.textContent?.trim() || option.value,
    value: option.value,
    disabled: option.disabled || Boolean(option.closest("optgroup")?.disabled),
  }));
}

function selectLabel(select) {
  if (select.getAttribute("aria-label")) return select.getAttribute("aria-label");
  const explicit = select.id ? document.querySelector(`label[for="${CSS.escape(select.id)}"]`) : null;
  if (explicit) return explicit.textContent.trim();
  const implicit = select.closest("label");
  const text = [...(implicit?.childNodes || [])].find((node) => node.nodeType === Node.TEXT_NODE && node.textContent.trim());
  return text?.textContent.trim() || select.name || "Select option";
}

function syncState(state) {
  const option = state.select.selectedOptions[0];
  const text = option?.textContent?.trim() || "Select…";
  if (state.value.textContent !== text) state.value.textContent = text;
  state.trigger.disabled = state.select.disabled;
  state.trigger.setAttribute("aria-label", `${state.label}: ${text}`);
  if (state.select.disabled) closeSelect(state);
  if (state.trigger.getAttribute("aria-expanded") === "true") renderOptions(state);
}

function positionContent(state) {
  const rect = state.trigger.getBoundingClientRect();
  const width = Math.min(Math.max(rect.width, 160), window.innerWidth - 16);
  state.content.style.width = `${width}px`;
  state.content.style.maxHeight = `${Math.min(240, window.innerHeight - 16)}px`;
  const height = Math.min(state.content.scrollHeight, 240);
  const left = Math.min(Math.max(8, rect.left), window.innerWidth - width - 8);
  const below = window.innerHeight - rect.bottom - 8;
  const top = below >= height || below >= rect.top ? rect.bottom + 4 : Math.max(8, rect.top - height - 4);
  state.content.style.left = `${left}px`;
  state.content.style.top = `${top}px`;
}

function setActive(state, index) {
  state.activeIndex = index;
  const items = [...state.content.querySelectorAll(".custom-select-option")];
  items.forEach((item, itemIndex) => item.classList.toggle("is-active", itemIndex === index));
  const active = items[index];
  if (!active) return;
  state.trigger.setAttribute("aria-activedescendant", active.id);
  if (active.offsetTop < state.content.scrollTop) state.content.scrollTop = active.offsetTop;
  else if (active.offsetTop + active.offsetHeight > state.content.scrollTop + state.content.clientHeight) {
    state.content.scrollTop = active.offsetTop + active.offsetHeight - state.content.clientHeight;
  }
}

function choose(state, index) {
  const options = selectOptions(state.select);
  if (!enabled(options, index)) return;
  state.select.selectedIndex = index;
  syncState(state);
  state.select.dispatchEvent(new Event("input", { bubbles: true }));
  state.select.dispatchEvent(new Event("change", { bubbles: true }));
  closeSelect(state);
  state.trigger.focus();
}

function renderOptions(state) {
  const options = selectOptions(state.select);
  const fragment = document.createDocumentFragment();
  options.forEach((option, index) => {
    const item = document.createElement("div");
    item.id = `${state.content.id}-option-${index}`;
    item.className = "custom-select-option";
    item.setAttribute("role", "option");
    item.setAttribute("aria-selected", String(index === state.select.selectedIndex));
    item.setAttribute("aria-disabled", String(option.disabled));
    item.textContent = option.label;
    if (!option.disabled) item.addEventListener("click", () => choose(state, index));
    item.addEventListener("pointermove", () => { if (!option.disabled) setActive(state, index); });
    fragment.append(item);
  });
  state.content.replaceChildren(fragment);
}

function closeSelect(state) {
  if (!state || state.trigger.getAttribute("aria-expanded") !== "true") return;
  state.trigger.setAttribute("aria-expanded", "false");
  state.trigger.removeAttribute("aria-activedescendant");
  state.root.classList.remove("is-open");
  state.content.remove();
  if (openState === state) openState = null;
}

function openSelect(state) {
  if (state.select.disabled) return;
  if (openState && openState !== state) closeSelect(openState);
  renderOptions(state);
  document.body.append(state.content);
  state.trigger.setAttribute("aria-expanded", "true");
  state.root.classList.add("is-open");
  openState = state;
  positionContent(state);
  const options = selectOptions(state.select);
  const selected = enabled(options, state.select.selectedIndex)
    ? state.select.selectedIndex
    : moveActiveIndex(options, -1, 1);
  setActive(state, selected);
}

function handleKeydown(state, event) {
  const open = state.trigger.getAttribute("aria-expanded") === "true";
  const options = selectOptions(state.select);
  if (event.key === "Escape") { closeSelect(state); return; }
  if (event.key === "Tab") { closeSelect(state); return; }
  if (event.key === "Enter" || event.key === " ") {
    event.preventDefault();
    if (open) choose(state, state.activeIndex);
    else openSelect(state);
    return;
  }
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    if (!open) openSelect(state);
    else setActive(state, moveActiveIndex(options, state.activeIndex, event.key === "ArrowDown" ? 1 : -1));
    return;
  }
  if (open && (event.key === "Home" || event.key === "End")) {
    event.preventDefault();
    const start = event.key === "Home" ? -1 : 0;
    setActive(state, moveActiveIndex(options, start, event.key === "Home" ? 1 : -1));
    return;
  }
  if (event.key.length !== 1 || event.ctrlKey || event.metaKey || event.altKey) return;
  const now = Date.now();
  state.search = now - state.searchAt < 600 ? state.search + event.key : event.key;
  state.searchAt = now;
  const index = findTypeaheadIndex(options, state.search, open ? state.activeIndex : state.select.selectedIndex);
  if (index < 0) return;
  event.preventDefault();
  if (open) setActive(state, index);
  else choose(state, index);
}

function enhanceSelect(select) {
  if (states.has(select)) { syncState(states.get(select)); return; }
  if (select.hidden || select.getAttribute("aria-hidden") === "true") return;

  const iconPicker = Boolean(select.closest(".tool-picker, .format-picker"));
  const composerSelect = select.classList.contains("composer-select");
  const label = selectLabel(select);
  const root = document.createElement("span");
  root.className = `custom-select-root${iconPicker ? " custom-select-icon" : ""}${composerSelect ? " custom-select-composer" : ""}`;
  const trigger = document.createElement("button");
  trigger.type = "button";
  trigger.className = "custom-select-trigger";
  trigger.setAttribute("role", "combobox");
  trigger.setAttribute("aria-haspopup", "listbox");
  trigger.setAttribute("aria-expanded", "false");
  const value = document.createElement("span");
  value.className = "custom-select-value";
  const chevron = document.createElement("span");
  chevron.className = "custom-select-chevron";
  chevron.setAttribute("aria-hidden", "true");
  trigger.append(value, chevron);
  const content = document.createElement("div");
  content.id = `custom-select-${++selectID}`;
  content.className = "custom-select-content";
  content.setAttribute("role", "listbox");
  trigger.setAttribute("aria-controls", content.id);

  select.before(root);
  root.append(select, trigger);
  select.classList.add("custom-select-native");
  select.setAttribute("aria-hidden", "true");
  select.tabIndex = -1;
  const state = { select, root, trigger, value, chevron, content, label, activeIndex: -1, search: "", searchAt: 0 };
  states.set(select, state);
  trigger.addEventListener("click", () => trigger.getAttribute("aria-expanded") === "true" ? closeSelect(state) : openSelect(state));
  trigger.addEventListener("keydown", (event) => handleKeydown(state, event));
  select.addEventListener("change", () => syncState(state));
  const observer = new MutationObserver(() => syncState(state));
  observer.observe(select, { childList: true, subtree: true, attributes: true, attributeFilter: ["disabled"] });
  syncState(state);
}

export function refreshCustomSelects(scope = document) {
  if (openState && !openState.root.isConnected) closeSelect(openState);
  if (scope instanceof HTMLSelectElement) { enhanceSelect(scope); return; }
  if (scope?.matches?.("select")) enhanceSelect(scope);
  scope?.querySelectorAll?.("select").forEach(enhanceSelect);
}

if (typeof document !== "undefined") {
  document.addEventListener("pointerdown", (event) => {
    if (!openState || openState.root.contains(event.target) || openState.content.contains(event.target)) return;
    closeSelect(openState);
  });
  window.addEventListener("resize", () => { if (openState) positionContent(openState); });
  window.addEventListener("scroll", () => { if (openState) positionContent(openState); }, true);
}
