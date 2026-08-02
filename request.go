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
	"connectionName": true, "host": true, "port": true, "mode": true,
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
		Port: values.Get("port"), Mode: values.Get("mode"), Fields: pluginFields(values),
	}
}

func requestLease(request queryRequest) string {
	if request.LeaseID != "" {
		return request.LeaseID
	}
	return request.ConnectionID
}

func connectionKey(tool, host, port string) (string, error) {
	addresses, err := pluginapi.ParseSeeds(host, port, "connection")
	if err != nil {
		return "", err
	}
	labels := make([]string, len(addresses))
	for index, item := range addresses {
		labels[index] = strings.ToLower(net.JoinHostPort(item.Host, strconv.Itoa(item.Port)))
	}
	sort.Strings(labels)
	return tool + ":" + strings.Join(labels, ","), nil
}
