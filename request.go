package main

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	pluginapi "orby/plugins"
)

type toolMetadata = pluginapi.Metadata
type queryRequest = pluginapi.Request
type queryResult = pluginapi.Result

var reservedFields = map[string]bool{
	"tool": true, "query": true, "format": true, "connectionId": true, "leaseId": true, "resource": true,
	"connectionName": true, "host": true, "port": true, "mode": true, "environment": true,
}

func resolvePort(args []string, environmentPort string) (int, error) {
	if len(args) > 1 {
		return 0, fmt.Errorf("usage: orby [port]")
	}
	value := environmentPort
	if len(args) == 1 {
		value = args[0]
	} else if value == "" {
		value = "8080"
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("port must be an integer from 1 to 65535")
	}
	return port, nil
}

func pluginFields(form url.Values) map[string]string {
	fields := map[string]string{}
	for name, values := range form {
		if reservedFields[name] || len(values) == 0 {
			continue
		}
		fields[name] = values[0]
	}
	return fields
}

func requestFromValues(values url.Values) queryRequest {
	return queryRequest{
		Query: values.Get("query"), Format: values.Get("format"),
		ConnectionID: values.Get("connectionId"), LeaseID: values.Get("leaseId"),
		ConnectionName: values.Get("connectionName"), Host: values.Get("host"),
		Port: values.Get("port"), Mode: values.Get("mode"), Environment: values.Get("environment"),
		Fields: pluginFields(values),
	}
}

// normalizeEnvironment maps arbitrary client input to "stage" or "prod",
// defaulting anything else (including empty/garbage) to the safe "prod".
func normalizeEnvironment(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), "stage") {
		return "stage"
	}
	return "prod"
}

// isPresetConnectionID reports whether id names a preset (saved,
// server-configured) connection rather than an ad-hoc one. Both the pool's
// acquire-vs-connect-first semantics and the environment trust boundary key
// off this same test, so it has one definition.
func isPresetConnectionID(id string) bool {
	return strings.HasPrefix(id, "preset:")
}

func requestLease(request queryRequest) string {
	if request.LeaseID != "" {
		return request.LeaseID
	}
	return request.ConnectionID
}

// connectionKey identifies the pooled client for a connection request. Host
// and port alone are not enough: redis single and cluster clients are distinct
// even on identical seeds, and a redis DB index selects a different database.
// Including them keeps those connections in separate pool entries.
func connectionKey(tool string, request queryRequest) (string, error) {
	addresses, err := pluginapi.ParseSeeds(request.Host, request.Port, "connection")
	if err != nil {
		return "", err
	}
	labels := make([]string, len(addresses))
	for index, item := range addresses {
		labels[index] = strings.ToLower(net.JoinHostPort(item.Host, strconv.Itoa(item.Port)))
	}
	sort.Strings(labels)
	key := tool + ":" + strings.Join(labels, ",")
	if tool == "redis" {
		mode := strings.ToLower(strings.TrimSpace(request.Mode))
		if mode == "" {
			mode = "single"
		}
		db := strings.TrimSpace(request.Fields["dbIndex"])
		if db == "" {
			db = "0"
		}
		key += ":mode=" + mode + ":db=" + db
	}
	return key, nil
}
