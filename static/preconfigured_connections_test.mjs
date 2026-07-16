import assert from "node:assert/strict";
import test from "node:test";

import { connectionGroups, mergeConnections, presetConnections } from "./preconfigured_connections.mjs";

const config = {
  profiles: [
    { id: "local", label: "Local", connections: [{ id: "preset:redis-local", name: "redis-local", tool: "redis", host: "localhost", port: "6379", mode: "single", fields: {} }] },
    { id: "test", label: "Test", connections: [{ id: "preset:aerospike-test", name: "aerospike-test", tool: "aerospike", host: "localhost", port: "3000", mode: "single", fields: {} }] },
  ],
};

test("presets are uniquely named and grouped by source profile", () => {
	const presets = presetConnections(config);
	const names = presets.map((connection) => connection.name);
	assert.equal(new Set(names).size, names.length);
	assert.deepEqual(new Set(presets.map((connection) => connection.profile)), new Set(["local", "test"]));
	assert.ok(presets.every((connection) => connection.preset && connection.host && connection.port));
});

test("saved connections remain first and are not replaced by presets", () => {
  const saved = [{ id: "mine", name: "My Redis", tool: "redis", host: "localhost", port: "6379", mode: "single", fields: {} }];
	const merged = mergeConnections(saved, config);
  assert.equal(merged[0], saved[0]);
  assert.equal(merged.filter((connection) => connection.id === "mine").length, 1);
	assert.equal(merged.length, saved.length + presetConnections(config).length);
});

test("connection groups keep saved and profile presets separate", () => {
	const groups = connectionGroups(mergeConnections([{ id: "mine", name: "Mine", tool: "redis", host: "localhost", port: "6379" }], config), config.profiles);
	assert.deepEqual(groups.map((group) => group.profile), ["saved", "local", "test"]);
	assert.equal(groups[0].connections[0].name, "Mine");
	assert.equal(groups[1].label, "Local");
});
