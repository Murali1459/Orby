import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import test from "node:test";

const modulePath = new URL("./virtual_output.mjs", import.meta.url);

test("virtual output module exists", () => {
  assert.equal(existsSync(modulePath), true, "virtual_output.mjs is missing");
});

if (existsSync(modulePath)) {
  const { chunkText, normalizePayload, previewText, tableText, visibleRange } = await import(modulePath);

  test("normalizePayload renders an empty table as a compact message", () => {
    assert.deepEqual(normalizePayload({ kind: "table", heads: [], rows: [] }), { kind: "raw", text: "No records" });
    assert.deepEqual(normalizePayload({ kind: "table", heads: ["name"], rows: [["Ada"]] }), { kind: "table", heads: ["name"], rows: [["Ada"]] });
  });

  test("visibleRange renders only the viewport and overscan", () => {
    assert.deepEqual(visibleRange(1000, 32, 3200, 320, 5), { start: 95, end: 115 });
    assert.deepEqual(visibleRange(0, 32, 0, 320, 5), { start: 0, end: 0 });
  });

  test("chunkText bounds long lines without changing the copy source", () => {
    assert.deepEqual(chunkText("abc\n\n123456789", 4), ["abc", "", "1234", "5678", "9"]);
  });

  test("tableText preserves all headers and rows for copy", () => {
    assert.equal(tableText(["name", "age"], [["Ada", "36"], ["Grace", "40"]]), "name\tage\nAda\t36\nGrace\t40");
  });

  test("previewText keeps multi-megabyte table cells out of the DOM", () => {
    assert.equal(previewText("123456789", 4), "1234… (9 chars)");
    assert.equal(previewText("small", 10), "small");
  });
}
