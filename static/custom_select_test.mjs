import assert from "node:assert/strict";
import test from "node:test";

import { findTypeaheadIndex, moveActiveIndex } from "./custom_select.mjs";

const options = [
  { label: "Alpha", disabled: false },
  { label: "Beta", disabled: true },
  { label: "Gamma", disabled: false },
];

test("moveActiveIndex skips disabled options and wraps", () => {
  assert.equal(moveActiveIndex(options, 0, 1), 2);
  assert.equal(moveActiveIndex(options, 2, 1), 0);
  assert.equal(moveActiveIndex(options, 0, -1), 2);
});

test("findTypeaheadIndex searches after the current option and wraps", () => {
  assert.equal(findTypeaheadIndex(options, "g", 0), 2);
  assert.equal(findTypeaheadIndex(options, "a", 2), 0);
  assert.equal(findTypeaheadIndex(options, "b", 0), -1);
});
