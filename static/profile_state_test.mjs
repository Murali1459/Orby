import assert from "node:assert/strict";
import test from "node:test";

import { loadCollapsedProfiles, setProfileCollapsed } from "./profile_state.mjs";

function memoryStorage(initial = {}) {
  const values = new Map(Object.entries(initial));
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
  };
}

test("profiles default to expanded when no state was saved", () => {
  assert.deepEqual([...loadCollapsedProfiles(memoryStorage())], []);
});

test("collapsed profile state is persisted independently", () => {
  const storage = memoryStorage();
  setProfileCollapsed(storage, "k8-ci", true);
  setProfileCollapsed(storage, "docker", true);
  setProfileCollapsed(storage, "k8-ci", false);
  assert.deepEqual([...loadCollapsedProfiles(storage)], ["docker"]);
});

test("invalid stored state is ignored", () => {
  assert.deepEqual([...loadCollapsedProfiles(memoryStorage({ orby_collapsed_profiles_v1: "{" }))], []);
});
