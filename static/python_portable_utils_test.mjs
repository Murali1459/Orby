import assert from "node:assert/strict";
import test from "node:test";

import { buildPortableScript } from "./python_portable_utils.mjs";

const connections = JSON.stringify([
  { id: "preset:redis", name: "redis-local", tool: "redis", mode: "single", hosts: [{ host: "redis.internal", port: 6379 }], fields: { dbIndex: "4" } },
  { id: "preset:aerospike", name: "aerospike-local", tool: "aerospike", mode: "single", hosts: [{ host: "as-a", port: 3000 }, { host: "as-b", port: 3001 }], fields: {} },
]);

test("portable Redis code uses the native client and command API", () => {
  const result = buildPortableScript(connections, `from orby import client\nredis = client("preset:redis")\nprint(redis.execute("GET", "key"))`);
  assert.match(result, /^import redis as _redis/);
  assert.match(result, /redis = _redis\.Redis\(host="redis\.internal", port=6379, db=4/);
  assert.match(result, /redis\.execute_command\("GET", "key"\)/);
  assert.doesNotMatch(result, /from orby|class OrbyClient/);
});

test("portable Aerospike code initializes the native client", () => {
  const result = buildPortableScript(connections, `from orby import client\naerospike = client('aerospike-local')\nprint(aerospike.run(namespace="test", set="demo", primaryKey="key-1"))`);
  assert.match(result, /^import aerospike as _aerospike/);
  assert.match(result, /"hosts": \[\("as-a", 3000\), \("as-b", 3001\)\]/);
  assert.match(result, /_aerospike_run\(aerospike, namespace="test"/);
  assert.doesNotMatch(result, /from orby|class OrbyClient/);
});

test("connections call becomes embedded connection data without an Orby module", () => {
  const result = buildPortableScript(connections, "from orby import connections\nprint(connections())");
  assert.match(result, /^_connections = \[/);
  assert.match(result, /print\(_connections\)/);
  assert.doesNotMatch(result, /from orby/);
});
