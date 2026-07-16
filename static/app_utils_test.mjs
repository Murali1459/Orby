import assert from "node:assert/strict";
import test from "node:test";

import { clamp, escapeHTML, sameSet } from "./app_utils.mjs";

test("clamp keeps values inside inclusive bounds", () => {
  assert.equal(clamp(5, 10, 20), 10);
  assert.equal(clamp(15, 10, 20), 15);
  assert.equal(clamp(25, 10, 20), 20);
});

test("escapeHTML safely encodes text and attributes", () => {
  assert.equal(escapeHTML(`<&>"'`), "&lt;&amp;&gt;&quot;&#39;");
  assert.equal(escapeHTML(null), "");
});

test("sameSet compares values without depending on insertion order", () => {
  assert.equal(sameSet(new Set(["a", "b"]), new Set(["b", "a"])), true);
  assert.equal(sameSet(new Set(["a"]), new Set(["a", "b"])), false);
  assert.equal(sameSet(new Set(["a"]), new Set(["b"])), false);
});
