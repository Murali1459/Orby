package aerospike

import (
	"reflect"
	"testing"

	as "github.com/aerospike/aerospike-client-go/v8"
	"orby/plugins"
)

func TestMetadataProvidesVisualCommand(t *testing.T) {
	metadata := Plugin{}.Metadata()
	var texts []string
	for _, element := range metadata.Composer.Elements {
		texts = append(texts, element.Text)
	}
	if metadata.Name != "aerospike" || !reflect.DeepEqual(texts, []string{"SELECT", "*", "FROM", "", ".", "", "WHERE PK", "=", "", "LIMIT", "", "Metadata", ""}) {
		t.Fatalf("metadata = %#v", metadata)
	}
	if metadata.Composer.Elements[3].Options != "namespaces" || metadata.Composer.Elements[5].DependsOn != "namespace" || metadata.Composer.Expression == nil {
		t.Fatalf("composer = %#v", metadata.Composer)
	}
	if !metadata.Composer.Elements[8].Grow {
		t.Fatal("primary key input must consume the remaining composer space")
	}
	if metadata.Composer.Elements[10].Name != "limit" || metadata.Composer.Elements[10].Default != "100" || metadata.Composer.Elements[10].InputType != "number" {
		t.Fatalf("limit composer = %#v", metadata.Composer.Elements[10])
	}
	if !metadata.Composer.Elements[9].HideNarrow || !metadata.Composer.Elements[10].HideNarrow {
		t.Fatalf("limit controls must be hidden together at constrained widths: %#v", metadata.Composer.Elements[9:11])
	}
	if metadata.Composer.Elements[11].Kind != "checkbox" || metadata.Composer.Elements[11].Name != "metadata" || metadata.Composer.Elements[11].Default != "false" || metadata.Composer.Elements[11].Placement != "leading" || metadata.Composer.Elements[11].Icon != "/static/icons/database.svg" {
		t.Fatalf("metadata composer = %#v", metadata.Composer.Elements[11])
	}
}

func TestParseCommandDefaultsLimitAndMakesMetadataOptional(t *testing.T) {
	command, err := parseCommand(plugins.Request{Fields: map[string]string{"namespace": "test", "set": "users"}})
	if err != nil || command.Limit != 100 || command.Metadata {
		t.Fatalf("command=%#v err=%v", command, err)
	}
	command, err = parseCommand(plugins.Request{Fields: map[string]string{"namespace": "test", "set": "users", "limit": "-1", "metadata": "true"}})
	if err != nil || command.Limit != -1 || !command.Metadata {
		t.Fatalf("command=%#v err=%v", command, err)
	}
	for _, limit := range []string{"0", "-2", "nope"} {
		if _, err := parseCommand(plugins.Request{Fields: map[string]string{"namespace": "test", "set": "users", "limit": limit}}); err == nil {
			t.Fatalf("invalid limit %q accepted", limit)
		}
	}
}

func TestParseCommandAndCompileExpression(t *testing.T) {
	expression := `{"kind":"group","logic":"and","children":[{"kind":"condition","bin":"status","type":"string","operator":"eq","value":"active"},{"kind":"group","logic":"or","children":[{"kind":"condition","bin":"age","type":"integer","operator":"gt","value":"18"},{"kind":"condition","bin":"enabled","type":"boolean","operator":"eq","value":"true"}]}]}`
	command, err := parseCommand(plugins.Request{Fields: map[string]string{"namespace": "test", "set": "users", "primaryKey": "  ", "expression": expression}})
	if err != nil || command.Namespace != "test" || command.Set != "users" || command.PrimaryKey != "" || command.Filter == nil {
		t.Fatalf("command = %#v, %v", command, err)
	}
	for _, fields := range []map[string]string{
		{"set": "users"},
		{"namespace": "test"},
		{"namespace": "test", "set": "users", "expression": "{"},
		{"namespace": "test", "set": "users", "expression": `{"kind":"condition","bin":"age","operator":"gt","value":"x"}`},
		{"namespace": "test", "set": "users", "expression": `{"kind":"condition","bin":"age","type":"integer","operator":"gt","value":"x"}`},
		{"namespace": "test", "set": "users", "expression": `{"kind":"condition","bin":"enabled","type":"boolean","operator":"gt","value":"true"}`},
		{"namespace": "test", "set": "users", "expression": `{"kind":"group","logic":"and","children":[]}`},
	} {
		if _, err := parseCommand(plugins.Request{Fields: fields}); err == nil {
			t.Fatalf("invalid fields accepted: %#v", fields)
		}
	}
}

func TestSingleConditionGroupCompilesAsCondition(t *testing.T) {
	condition := expressionNode{
		Kind: "condition", Bin: "age", Type: "integer", Operator: "gt", Value: "30",
	}
	direct, err := compileExpression(condition)
	if err != nil {
		t.Fatal(err)
	}
	grouped, err := compileExpression(expressionNode{
		Kind: "group", Logic: "and", Children: []expressionNode{condition},
	})
	if err != nil {
		t.Fatal(err)
	}
	directBase64, err := direct.Base64()
	if err != nil {
		t.Fatal(err)
	}
	groupedBase64, err := grouped.Base64()
	if err != nil {
		t.Fatal(err)
	}
	if groupedBase64 != directBase64 {
		t.Fatalf("single-condition group compiled as %q, want %q", groupedBase64, directBase64)
	}
}

func TestRecordToRowOmitsMetadataUnlessRequested(t *testing.T) {
	key, _ := as.NewKey("test", "users", "u1")
	record := &as.Record{Key: key, Bins: as.BinMap{"name": "Ada"}, Generation: 2, Expiration: 10}
	row := recordToRow(record, false)
	if row["name"] != "Ada" {
		t.Fatalf("bins not copied: %#v", row)
	}
	if _, exists := row["key"]; exists {
		t.Fatalf("metadata included without flag: %#v", row)
	}
	if _, exists := row["namespace"]; exists {
		t.Fatalf("metadata included without flag: %#v", row)
	}

	row = recordToRow(record, true)
	if row["name"] != "Ada" || row["key"] != "u1" || row["namespace"] != "test" || row["generation"] != 2 {
		t.Fatalf("metadata not fully included: %#v", row)
	}
	if recordToRow(nil, false) != nil {
		t.Fatal("nil record should return nil")
	}
}

func TestSummaryBuildsCorrectQueryString(t *testing.T) {
	got := summary(command{Namespace: "test", Set: "users", Limit: 50})
	if got != "SELECT * FROM test.users LIMIT 50" {
		t.Fatalf("scan summary = %q", got)
	}
	got = summary(command{Namespace: "test", Set: "users", PrimaryKey: "u1"})
	if got != "SELECT * FROM test.users WHERE PK = u1" {
		t.Fatalf("get summary = %q", got)
	}
}
