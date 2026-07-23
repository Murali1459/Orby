package aerospike

import (
	"fmt"
	"maps"
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
		DefaultFormat: "json", Formats: []string{"json", "table"},
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
	client, err := newNativeClient(seeds)
	if err != nil {
		return nil, fmt.Errorf("Aerospike connection failed: %v", err)
	}
	return &connection{client: *client}, nil
}

type connection struct{ client nativeClient }

func (connection *connection) Run(request plugins.Request) (plugins.Result, error) {
	return runWithClient(request, &connection.client)
}

func (connection *connection) Options(request plugins.Request, resource string) ([]plugins.Option, error) {
	return optionsWithClient(request, resource, &connection.client)
}

func (connection *connection) Close() error {
	connection.client.Close()
	return nil
}

func runWithClient(request plugins.Request, client *nativeClient) (plugins.Result, error) {
	started := time.Now()
	command, err := parseCommand(request)
	if err != nil {
		return plugins.Result{}, err
	}
	format := strings.ToLower(strings.TrimSpace(request.Format))
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "table" {
		return plugins.Result{}, fmt.Errorf("unsupported Aerospike format %q", format)
	}
	var records []*as.Record
	if command.PrimaryKey == "" {
		recordset, scanErr := client.Scan(command.Namespace, command.Set, command.Filter, command.Limit)
		if scanErr != nil {
			err = scanErr
		} else {
			defer recordset.Close()
			for result := range recordset.Results() {
				if result.Err != nil {
					err = result.Err
					break
				}
				records = append(records, result.Record)
			}
		}
	} else {
		record, getErr := client.Get(command.Namespace, command.Set, command.PrimaryKey, command.Filter)
		if getErr != nil {
			err = getErr
		} else if record != nil {
			records = []*as.Record{record}
		}
	}
	if err != nil {
		return plugins.Result{}, fmt.Errorf("Aerospike execution failed: %v", err)
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		rows = append(rows, sanitizeJSON(recordToRow(record, command.Metadata)).(map[string]any))
	}
	state := map[string]string{}
	maps.Copy(state, request.Fields)
	result := plugins.Result{
		Tool: "aerospike", Query: summary(command), Format: format,
		Profile: plugins.ProfileName(request.ConnectionName), Rows: rows,
		RowCount: len(rows), HasCount: true, DurationMS: time.Since(started).Milliseconds(),
		Succeeded: true, State: state,
	}
	return result, nil
}

func summary(command command) string {
	value := fmt.Sprintf("SELECT * FROM %s.%s", command.Namespace, command.Set)
	if command.PrimaryKey != "" {
		key := strings.ReplaceAll(command.PrimaryKey, "'", "\\'")
		value += " WHERE PK = " + key
	} else {
		value += fmt.Sprintf(" LIMIT %d", command.Limit)
	}
	return value
}

func recordToRow(record *as.Record, includeMetadata bool) map[string]any {
	if record == nil {
		return nil
	}
	row := map[string]any{}
	maps.Copy(row, record.Bins)
	if includeMetadata {
		if record.Key != nil {
			row["namespace"] = record.Key.Namespace()
			row["set"] = record.Key.SetName()
			if record.Key.Value() != nil {
				row["key"] = record.Key.Value().GetObject()
			}
		}
		row["generation"] = int(record.Generation)
		row["expiration"] = int(record.Expiration)
	}
	return row
}

func sanitizeJSON(v any) any {
	switch value := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[fmt.Sprint(k)] = sanitizeJSON(v)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k] = sanitizeJSON(v)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, v := range value {
			out[i] = sanitizeJSON(v)
		}
		return out
	default:
		return v
	}
}
