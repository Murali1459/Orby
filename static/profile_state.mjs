const storageKey = "pluginvm_collapsed_profiles_v1";

export function loadCollapsedProfiles(storage) {
  try {
    const profiles = JSON.parse(storage?.getItem(storageKey) || "[]");
    return new Set(Array.isArray(profiles) ? profiles.filter((profile) => typeof profile === "string") : []);
  } catch {
    return new Set();
  }
}

export function setProfileCollapsed(storage, profile, collapsed) {
  const profiles = loadCollapsedProfiles(storage);
  if (collapsed) profiles.add(profile);
  else profiles.delete(profile);
  try { storage?.setItem(storageKey, JSON.stringify([...profiles].sort())); } catch { /* Storage is optional. */ }
}
