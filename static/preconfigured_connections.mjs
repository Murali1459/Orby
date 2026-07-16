export function presetConnections(config = {}) {
  const profiles = Array.isArray(config.profiles) ? config.profiles : [];
  return profiles.flatMap((profile) => {
    const connections = Array.isArray(profile.connections) ? profile.connections : [];
    return connections.map((connection) => ({ ...connection, profile: profile.id, preset: true }));
  });
}

export function mergeConnections(saved = [], config = {}) {
  const local = Array.isArray(saved) ? saved : [];
  const localIDs = new Set(local.map((connection) => connection.id));
  return [...local, ...presetConnections(config).filter((connection) => !localIDs.has(connection.id))];
}

export function connectionGroups(connections = [], profiles = []) {
  const definitions = [
    { id: "saved", label: "Saved" },
    ...(Array.isArray(profiles) ? profiles.map((profile) => ({ id: profile.id, label: profile.label })) : []),
  ];
  return definitions.map(({ id, label }) => ({
    profile: id,
    label,
    connections: connections.filter((connection) => (connection.profile || "saved") === id),
  })).filter((group) => group.connections.length);
}
