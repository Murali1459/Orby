function pythonString(value) {
  return JSON.stringify(String(value));
}

function findConnection(connections, identifier) {
  return connections.find((item) => item.id === identifier || item.name === identifier);
}

function redisInitialization(variable, config) {
  const fields = config.fields || {};
  if (config.mode === "cluster") {
    const nodes = config.hosts.map((host) => `_redis.cluster.ClusterNode(${pythonString(host.host)}, ${host.port})`).join(", ");
    return `${variable} = _redis.RedisCluster(startup_nodes=[${nodes}], decode_responses=True)`;
  }
  const host = config.hosts[0];
  return `${variable} = _redis.Redis(host=${pythonString(host.host)}, port=${host.port}, db=${Number.parseInt(fields.dbIndex || "0", 10)}, decode_responses=True)`;
}

function aerospikeInitialization(variable, config) {
  const hosts = config.hosts.map((host) => `(${pythonString(host.host)}, ${host.port})`).join(", ");
  const fields = config.fields || {};
  const options = [`"hosts": [${hosts}]`];
  if (fields.user) options.push(`"user": ${pythonString(fields.user)}`);
  if (fields.password) options.push(`"password": ${pythonString(fields.password)}`);
  return `${variable} = _aerospike.client({${options.join(", ")}}).connect()`;
}

export function buildPortableScript(connectionJSON, source) {
  let connections;
  try {
    connections = JSON.parse(connectionJSON);
  } catch {
    return source;
  }

  const imports = new Set();
  const variables = new Map();
  let script = source.replace(/^\s*from\s+orby\s+import\s+[^\n]*(?:\n|$)/gm, "");
  script = script.replace(/^(\s*)([A-Za-z_]\w*)\s*=\s*client\(\s*(["'])(.*?)\3\s*\)\s*$/gm,
    (line, indent, variable, quote, identifier) => {
      const config = findConnection(connections, identifier);
      if (!config || !config.hosts?.length) return line;
      variables.set(variable, config.tool);
      if (config.tool === "redis") {
        imports.add("import redis as _redis");
        return indent + redisInitialization(variable, config);
      }
      if (config.tool === "aerospike") {
        imports.add("import aerospike as _aerospike");
        return indent + aerospikeInitialization(variable, config);
      }
      return line;
    });

  for (const [variable, tool] of variables) {
    const escaped = variable.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    if (tool === "redis") {
      script = script.replace(new RegExp(`\\b${escaped}\\.execute\\(`, "g"), `${variable}.execute_command(`);
    }
    if (tool === "aerospike" && new RegExp(`\\b${escaped}\\.run\\(`).test(script)) {
      script = script.replace(new RegExp(`\\b${escaped}\\.run\\(`, "g"), `_aerospike_run(${variable}, `);
      imports.add(`def _aerospike_run(client, namespace, set, primaryKey=None, limit=100, **_):\n    if primaryKey not in (None, ""):\n        return [dict(client.get((namespace, set, primaryKey))[2] or {})]\n    rows = []\n    client.query(namespace, set).foreach(lambda record: rows.append(dict(record[2] or {})) or len(rows) < int(limit))\n    return rows`);
    }
  }

  if (/\bconnections\s*\(\s*\)/.test(script)) {
    script = script.replace(/\bconnections\s*\(\s*\)/g, "_connections");
    imports.add(`_connections = ${JSON.stringify(connections)}`);
  }

  const header = [...imports].join("\n\n");
  return `${header}${header ? "\n\n" : ""}${script.trimStart()}`.trimEnd() + "\n";
}
