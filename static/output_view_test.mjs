import assert from "node:assert/strict";
import test from "node:test";

import { virtualHeight, jsonLineStructure, visibleJsonLines, browseTTL, browseInspectCommand, browseCopyText } from "./output_view.mjs";

test("virtualHeight preserves compact and expanded output caps", () => {
  assert.equal(virtualHeight(100, false, 1000), 100);
  assert.equal(virtualHeight(900, false, 1000), 260);
  assert.equal(virtualHeight(900, true, 1000), 700);
});

test("jsonLineStructure marks opening lines and their matching closers", () => {
  const items = ["{", '  "a": [', "    1", "  ]", '  "b": 2', "}"];
  const structure = jsonLineStructure(items);
  assert.equal(structure[0].opens, true);
  assert.equal(structure[0].closeIndex, 5);
  assert.equal(structure[1].opens, true);
  assert.equal(structure[1].closeIndex, 3);
  assert.equal(structure[4].opens, false);
  assert.equal(structure[4].closeIndex, -1);
});

test("visibleJsonLines skips subtrees of collapsed openers", () => {
  const items = ["{", '  "a": [', "    1", "  ]", '  "b": 2', "}"];
  const structure = jsonLineStructure(items);
  assert.deepEqual(visibleJsonLines(structure, new Set()), [0, 1, 2, 3, 4, 5]);
  assert.deepEqual(visibleJsonLines(structure, new Set([1])), [0, 1, 4, 5]);
  assert.deepEqual(visibleJsonLines(structure, new Set([0])), [0]);
});

test("browseTTL formats seconds as s, m, or h and skips persistent keys", () => {
  assert.equal(browseTTL(45), "45s");
  assert.equal(browseTTL(120), "2m");
  assert.equal(browseTTL(7200), "2h");
  assert.equal(browseTTL(0), "");
  assert.equal(browseTTL(-1), "");
  assert.equal(browseTTL(undefined), "");
});

test("browseInspectCommand maps redis types to read commands", () => {
  assert.equal(browseInspectCommand("string", "user:88"), "GET user:88");
  assert.equal(browseInspectCommand("hash", "user:1042"), "HGETALL user:1042");
  assert.equal(browseInspectCommand("list", "queue:1"), "LRANGE queue:1 0 -1");
  assert.equal(browseInspectCommand("set", "tags"), "SMEMBERS tags");
  assert.equal(browseInspectCommand("zset", "ranks"), "ZRANGE ranks 0 -1");
  assert.equal(browseInspectCommand("unknown", "k"), "");
});

test("browseInspectCommand quotes unsafe key names", () => {
  assert.equal(browseInspectCommand("string", "my key"), 'GET "my key"');
  assert.equal(browseInspectCommand("hash", 'say "hi"'), 'HGETALL "say \\"hi\\""');
});

test("browseCopyText renders a text tree with ttl and more hints", () => {
  const view = {
    pattern: "user:*",
    scanned: 3,
    limited: true,
    groups: [
      { prefix: "user:", total: 3, keys: [{ name: "user:1", type: "hash", ttl: 7200 }, { name: "user:2", type: "string" }] },
    ],
  };
  assert.equal(
    browseCopyText(view),
    [
      "BROWSE user:*",
      "3 keys scanned (scan limited)",
      "user: (3 keys)",
      "  user:1 [hash] (2h)",
      "  user:2 [string]",
    ].join("\n"),
  );
});

test("browseCopyText notes cluster-wide scans", () => {
  const view = { pattern: "user:*", scanned: 2, cluster: true, groups: [] };
  assert.equal(
    browseCopyText(view),
    ["BROWSE user:*", "2 keys scanned (cluster-wide)"].join("\n"),
  );
  assert.equal(
    browseCopyText({ ...view, limited: true }),
    ["BROWSE user:*", "2 keys scanned (scan limited, cluster-wide)"].join("\n"),
  );
});
