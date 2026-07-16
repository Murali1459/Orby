package aerospike

import (
	"errors"
	"fmt"
	"strings"
	"time"

	as "github.com/aerospike/aerospike-client-go/v8"
	"pluginvm/plugins"
)

type Plugin struct{}

func New() plugins.Plugin { return Plugin{} }

func (Plugin) Metadata() plugins.Metadata {
	comparisons := []plugins.FilterOperator{
		{Value: "eq", Label: "="}, {Value: "ne", Label: "≠"},
		{Value: "gt", Label: ">"}, {Value: "gte", Label: "≥"},
		{Value: "lt", Label: "<"}, {Value: "lte", Label: "≤"},
	}
	equality := comparisons[:2]
	return plugins.Metadata{
		Name: "aerospike", Label: "Aerospike", Badge: "AS", ColorClass: "tool-aql", Icon: "/static/icons/aerospike.svg",
		DefaultFormat: "json", Formats: []string{"json", "table", "raw"},
		Composer: plugins.Composer{
			Elements: []plugins.ComposerElement{
				{Kind: "literal", Text: "SELECT"},
				{Kind: "literal", Text: "*"},
				{Kind: "literal", Text: "FROM"},
				{Kind: "select", Name: "namespace", Options: "namespaces"},
				{Kind: "literal", Text: "."},
				{Kind: "select", Name: "set", Options: "sets", DependsOn: "namespace"},
				{Kind: "literal", Text: "WHERE PK"},
				{Kind: "literal", Text: "="},
				{Kind: "input", Name: "primaryKey", Placeholder: "optional — all records", Grow: true},
				{Kind: "literal", Text: "LIMIT", HideNarrow: true},
				{Kind: "input", Name: "limit", Default: "100", InputType: "number", HideNarrow: true},
				{Kind: "checkbox", Name: "metadata", Text: "Metadata", Default: "false", Placement: "leading", Icon: "/static/icons/database.svg"},
				{Kind: "action", Name: "expression", Action: "expression"},
			},
			Expression: &plugins.ExpressionEditor{
				BinOptions: "bins",
				Types: []plugins.ExpressionType{
					{Value: "string", Label: "string", InputType: "text", Operators: comparisons},
					{Value: "integer", Label: "integer", InputType: "number", Operators: comparisons},
					{Value: "float", Label: "float", InputType: "number", Operators: comparisons},
					{Value: "boolean", Label: "boolean", InputType: "select", Operators: equality},
				},
			},
		},
	}
}

func (Plugin) Connect(request plugins.Request) (plugins.Connection, error) {
	seeds, err := parseSeeds(request.Host, request.Port)
	if err != nil {
		return nil, err
	}
	client, err := clientFactory(seeds)
	if err != nil {
		return nil, fmt.Errorf("Aerospike connection failed: %v", err)
	}
	return &connection{client: client}, nil
}

type connection struct{ client clientAPI }

func (connection *connection) Run(request plugins.Request) (plugins.Result, error) {
	return runWithClient(request, connection.client)
}

func (connection *connection) Options(request plugins.Request, resource string) ([]plugins.Option, error) {
	return optionsWithClient(request, resource, connection.client)
}

func (connection *connection) Close() error {
	connection.client.Close()
	return nil
}

func runWithClient(request plugins.Request, client clientAPI) (plugins.Result, error) {
	started := time.Now()
	command, err := parseCommand(request)
	if err != nil {
		return plugins.Result{}, err
	}
	format := strings.ToLower(strings.TrimSpace(request.Format))
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "table" && format != "raw" {
		return plugins.Result{}, fmt.Errorf("unsupported Aerospike format %q", format)
	}
	var records []map[string]any
	if command.PrimaryKey == "" {
		records, err = client.Scan(command.Namespace, command.Set, command.Filter, command.Limit)
	} else {
		var record map[string]any
		record, err = client.Get(command.Namespace, command.Set, command.PrimaryKey, command.Filter)
		if errors.Is(err, as.ErrKeyNotFound) || errors.Is(err, as.ErrFilteredOut) {
			err = nil
		}
		if record != nil {
			records = []map[string]any{record}
		}
	}
	if err != nil {
		return plugins.Result{}, fmt.Errorf("Aerospike execution failed: %v", err)
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		rows = append(rows, normalizeRecord(record, command.Metadata))
	}
	state := map[string]string{}
	for key, value := range request.Fields {
		state[key] = value
	}
	result := plugins.Result{
		Tool: "aerospike", Query: summary(command), Format: format,
		Profile: plugins.ProfileName(request.ConnectionName), Rows: rows,
		RowCount: len(rows), HasCount: true, DurationMS: time.Since(started).Milliseconds(),
		Succeeded: true, State: state,
	}
	if format == "raw" {
		lines := make([]string, len(rows))
		for index, row := range rows {
			encoded, err := plugins.MarshalJSON(row)
			if err != nil {
				return plugins.Result{}, err
			}
			lines[index] = string(encoded)
		}
		result.Rows, result.Raw, result.IsRaw = nil, strings.Join(lines, "\n"), true
	}
	return result, nil
}

func summary(command command) string {
	value := fmt.Sprintf("SELECT * FROM %s.%s", command.Namespace, command.Set)
	if command.PrimaryKey != "" {
		key := strings.ReplaceAll(command.PrimaryKey, "'", "\\'")
		value += " WHERE PK = '" + key + "'"
	} else {
		value += fmt.Sprintf(" LIMIT %d", command.Limit)
	}
	return value
}

func normalizeRecord(record map[string]any, includeMetadata bool) map[string]any {
	row := map[string]any{}
	if bins, ok := record["bins"].(map[string]any); ok {
		for key, value := range bins {
			row[key] = value
		}
	}
	if includeMetadata {
		row["_key"] = record["key"]
		row["_namespace"] = valueOr(record, "namespace", "")
		row["_set"] = valueOr(record, "set", "")
		row["_generation"] = valueOr(record, "generation", 0)
		row["_expiration"] = valueOr(record, "expiration", 0)
	}
	return row
}

func valueOr(values map[string]any, key string, fallback any) any {
	if value, ok := values[key]; ok {
		return value
	}
	return fallback
}
