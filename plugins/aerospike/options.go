package aerospike

import (
	"fmt"
	"sort"
	"strings"

	"orby/plugins"
)

func optionsWithClient(request plugins.Request, resource string, client *nativeClient) ([]plugins.Option, error) {
	namespace := strings.TrimSpace(request.Fields["namespace"])
	if resource != "namespaces" && resource != "sets" {
		return nil, fmt.Errorf("unsupported Aerospike option resource %q", resource)
	}
	values, err := client.Info(resource)
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
