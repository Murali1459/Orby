package aerospike

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"pluginvm/plugins"
)

func optionsWithClient(request plugins.Request, resource string, client *nativeClient) ([]plugins.Option, error) {
	namespace := strings.TrimSpace(request.Fields["namespace"])
	command := resource
	if resource == "bins" {
		if namespace == "" {
			return nil, fmt.Errorf("namespace is required for bins")
		}
		command = "bins/" + namespace
	} else if resource != "namespaces" && resource != "sets" {
		return nil, fmt.Errorf("unsupported Aerospike option resource %q", resource)
	}
	values, err := client.Info(command)
	if err != nil {
		return nil, fmt.Errorf("Aerospike metadata failed: %v", err)
	}
	for _, value := range values {
		if value = strings.TrimSpace(value); strings.HasPrefix(value, "ERROR:") {
			return nil, fmt.Errorf("Aerospike metadata failed: %s", value)
		}
	}
	return parseOptions(resource, namespace, values), nil
}

func parseOptions(resource, namespace string, values []string) []plugins.Option {
	unique := map[string]bool{}
	for _, raw := range values {
		switch resource {
		case "namespaces":
			for _, item := range strings.FieldsFunc(raw, func(char rune) bool { return char == ';' || char == ',' || char == '\n' }) {
				if item = strings.TrimSpace(item); item != "" {
					unique[item] = true
				}
			}
		case "sets":
			for _, record := range strings.FieldsFunc(raw, func(char rune) bool { return char == ';' || char == '\n' }) {
				fields := map[string]string{}
				for token := range strings.SplitSeq(record, ":") {
					key, value, ok := strings.Cut(token, "=")
					if ok {
						fields[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
					}
				}
				if fields["ns"] == namespace && fields["set"] != "" {
					unique[fields["set"]] = true
				}
			}
		case "bins":
			for item := range strings.SplitSeq(raw, ",") {
				item = strings.TrimSpace(item)
				if item != "" && !strings.Contains(item, "=") {
					unique[item] = true
				}
			}
		}
	}
	names := make([]string, 0, len(unique))
	for name := range unique {
		names = append(names, name)
	}
	sort.Strings(names)
	options := make([]plugins.Option, len(names))
	for index, name := range names {
		options[index] = plugins.Option{Value: name, Label: name}
	}
	return options
}

func parseSeeds(hosts, defaultPort string) ([]plugins.Address, error) {
	port, err := strconv.Atoi(strings.TrimSpace(defaultPort))
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("valid Aerospike port is required")
	}
	seeds := []plugins.Address{}
	for raw := range strings.SplitSeq(hosts, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		seed, err := plugins.ParseAddress(raw, port)
		if err != nil {
			return nil, fmt.Errorf("invalid Aerospike seed")
		}
		seeds = append(seeds, seed)
	}
	if len(seeds) == 0 {
		return nil, fmt.Errorf("Aerospike host is required")
	}
	return seeds, nil
}
