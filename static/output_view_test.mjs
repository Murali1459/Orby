import assert from "node:assert/strict";
import test from "node:test";

import { virtualHeight } from "./output_view.mjs";

test("virtualHeight preserves compact and expanded output caps", () => {
  assert.equal(virtualHeight(100, false, 1000), 100);
  assert.equal(virtualHeight(900, false, 1000), 260);
  assert.equal(virtualHeight(900, true, 1000), 700);
});
