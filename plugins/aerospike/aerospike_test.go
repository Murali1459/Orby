package aerospike

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	as "github.com/aerospike/aerospike-client-go/v8"
	"pluginvm/plugins"
)

type fakeClient struct {
	method    string
	namespace string
	set       string
	key       string
	filter    *as.Expression
	limit     int
	records   []map[string]any
	info      map[string][]string
	err       error
	closed    bool
}

func run(request plugins.Request) (plugins.Result, error) {
	connection, err := (Plugin{}).Connect(request)
	if err != nil {
		return plugins.Result{}, err
	}
	defer connection.Close()
	return connection.Run(request)
}

func options(request plugins.Request, resource string) ([]plugins.Option, error) {
	connection, err := (Plugin{}).Connect(request)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	return connection.(plugins.ConnectionOptionProvider).Options(request, resource)
}

func (client *fakeClient) Get(namespace, set, key string, filter *as.Expression) (map[string]any, error) {
	client.method, client.namespace, client.set, client.key, client.filter = "get", namespace, set, key, filter
	if len(client.records) == 0 {
		return nil, client.err
	}
	return client.records[0], client.err
}

func (client *fakeClient) Scan(namespace, set string, filter *as.Expression, limit int) ([]map[string]any, error) {
	client.method, client.namespace, client.set, client.filter, client.limit = "scan", namespace, set, filter, limit
	return client.records, client.err
}

func (client *fakeClient) Info(command string) ([]string, error) {
	client.method = "info:" + command
	return client.info[command], client.err
}

func (client *fakeClient) Close() { client.closed = true }

func TestPluginConnectionReusesAerospikeClientForQueriesAndOptions(t *testing.T) {
	original := clientFactory
	defer func() { clientFactory = original }()
	client := &fakeClient{
		records: []map[string]any{{"bins": map[string]any{"name": "Ada"}}},
		info:    map[string][]string{"namespaces": {"test"}},
	}
	creates := 0
	clientFactory = func([]plugins.Address) (clientAPI, error) {
		creates++
		return client, nil
	}

	connection, err := (Plugin{}).Connect(plugins.Request{Host: "aerospike.internal", Port: "3000"})
	if err != nil {
		t.Fatal(err)
	}
	request := plugins.Request{Host: "aerospike.internal", Port: "3000", Fields: map[string]string{"namespace": "test", "set": "users"}}
	for range 2 {
		if _, err := connection.Run(request); err != nil {
			t.Fatal(err)
		}
	}
	provider, ok := connection.(plugins.ConnectionOptionProvider)
	if !ok {
		t.Fatal("Aerospike connection does not provide options")
	}
	if _, err := provider.Options(request, "namespaces"); err != nil {
		t.Fatal(err)
	}
	if creates != 1 || client.closed {
		t.Fatalf("creates=%d closed=%v", creates, client.closed)
	}
	if err := connection.Close(); err != nil || !client.closed {
		t.Fatalf("close err=%v closed=%v", err, client.closed)
	}
}

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

func TestRunUsesPrimaryKeyOrFullScan(t *testing.T) {
	original := clientFactory
	defer func() { clientFactory = original }()
	client := &fakeClient{records: []map[string]any{{"key": "u1", "namespace": "test", "set": "users", "bins": map[string]any{"name": "Ada"}}}}
	clientFactory = func([]plugins.Address) (clientAPI, error) { return client, nil }

	result, err := run(plugins.Request{Host: "node", Port: "3000", Format: "json", Fields: map[string]string{"namespace": "test", "set": "users"}})
	if err != nil || client.method != "scan" || client.limit != 100 || !client.closed || result.RowCount != 1 || result.Query != "SELECT * FROM test.users LIMIT 100" {
		t.Fatalf("scan result=%#v client=%#v err=%v", result, client, err)
	}

	client = &fakeClient{records: []map[string]any{{"key": "u1", "namespace": "test", "set": "users", "bins": map[string]any{"name": "Ada"}}}}
	clientFactory = func([]plugins.Address) (clientAPI, error) { return client, nil }
	result, err = run(plugins.Request{Host: "node", Port: "3000", Fields: map[string]string{"namespace": "test", "set": "users", "primaryKey": "u1"}})
	if err != nil || client.method != "get" || client.key != "u1" || result.Query != "SELECT * FROM test.users WHERE PK = 'u1'" {
		t.Fatalf("get result=%#v client=%#v err=%v", result, client, err)
	}
}

func TestRunOmitsMetadataUnlessRequested(t *testing.T) {
	record := map[string]any{"key": "u1", "namespace": "test", "set": "users", "generation": 2, "expiration": 10, "bins": map[string]any{"name": "Ada"}}
	withoutMetadata := &fakeClient{records: []map[string]any{record}}
	result, err := runWithClient(plugins.Request{Fields: map[string]string{"namespace": "test", "set": "users"}}, withoutMetadata)
	if err != nil || len(result.Rows) != 1 || result.Rows[0]["name"] != "Ada" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	for _, key := range []string{"_key", "_namespace", "_set", "_generation", "_expiration"} {
		if _, exists := result.Rows[0][key]; exists {
			t.Fatalf("metadata %q included by default: %#v", key, result.Rows[0])
		}
	}

	withMetadata := &fakeClient{records: []map[string]any{record}}
	result, err = runWithClient(plugins.Request{Fields: map[string]string{"namespace": "test", "set": "users", "metadata": "true"}}, withMetadata)
	if err != nil || result.Rows[0]["_key"] != "u1" || result.Rows[0]["_generation"] != 2 {
		t.Fatalf("metadata result=%#v err=%v", result, err)
	}
}

func TestMissingOrFilteredPrimaryKeyReturnsNoRows(t *testing.T) {
	original := clientFactory
	defer func() { clientFactory = original }()
	for _, emptyResult := range []error{as.ErrKeyNotFound, as.ErrFilteredOut} {
		client := &fakeClient{err: emptyResult}
		clientFactory = func([]plugins.Address) (clientAPI, error) { return client, nil }
		result, err := run(plugins.Request{Host: "node", Port: "3000", Fields: map[string]string{"namespace": "test", "set": "users", "primaryKey": "missing"}})
		if err != nil || result.RowCount != 0 || len(result.Rows) != 0 {
			t.Fatalf("error=%v result=%#v", emptyResult, result)
		}
	}
}

func TestOptionsParseNamespacesSetsAndBins(t *testing.T) {
	original := clientFactory
	defer func() { clientFactory = original }()
	client := &fakeClient{info: map[string][]string{
		"namespaces": {"bar;test", "test;bar"},
		"sets":       {"ns=test:set=users:objects=2;ns=bar:set=logs:objects=1;ns=test:set=orders:objects=3"},
		"bins/test":  {"num-bin-names=3,bin-names-quota=65535,name,age,status"},
	}}
	clientFactory = func([]plugins.Address) (clientAPI, error) { client.closed = false; return client, nil }
	request := plugins.Request{Host: "node", Port: "3000", Fields: map[string]string{"namespace": "test"}}
	for resource, want := range map[string][]plugins.Option{
		"namespaces": {{Value: "bar", Label: "bar"}, {Value: "test", Label: "test"}},
		"sets":       {{Value: "orders", Label: "orders"}, {Value: "users", Label: "users"}},
		"bins":       {{Value: "age", Label: "age"}, {Value: "name", Label: "name"}, {Value: "status", Label: "status"}},
	} {
		got, err := options(request, resource)
		if err != nil || !reflect.DeepEqual(got, want) || !client.closed {
			t.Fatalf("%s = %#v, %v closed=%v", resource, got, err, client.closed)
		}
	}
}

func TestOptionsRejectsInfoProtocolErrors(t *testing.T) {
	original := clientFactory
	defer func() { clientFactory = original }()
	client := &fakeClient{info: map[string][]string{
		"bins/test": {"ERROR:4:unrecognized command"},
	}}
	clientFactory = func([]plugins.Address) (clientAPI, error) { return client, nil }

	options, err := options(plugins.Request{
		Host:   "node",
		Port:   "3000",
		Fields: map[string]string{"namespace": "test"},
	}, "bins")
	if err == nil || len(options) != 0 || !strings.Contains(err.Error(), "unrecognized command") {
		t.Fatalf("options=%#v err=%v", options, err)
	}
}

func TestRunSanitizesExecutionErrors(t *testing.T) {
	original := clientFactory
	defer func() { clientFactory = original }()
	client := &fakeClient{err: errors.New("socket exploded")}
	clientFactory = func([]plugins.Address) (clientAPI, error) { return client, nil }
	_, err := run(plugins.Request{Host: "node", Port: "3000", Fields: map[string]string{"namespace": "test", "set": "users"}})
	if err == nil || !strings.Contains(err.Error(), "Aerospike execution failed") || !client.closed {
		t.Fatalf("err=%v closed=%v", err, client.closed)
	}
}

func TestRawResultIsJSON(t *testing.T) {
	original := clientFactory
	defer func() { clientFactory = original }()
	client := &fakeClient{records: []map[string]any{
		{"key": "u1", "namespace": "test", "set": "users", "bins": map[string]any{"name": "Ada"}},
		{"key": "u2", "namespace": "test", "set": "users", "bins": map[string]any{"name": "Grace"}},
	}}
	clientFactory = func([]plugins.Address) (clientAPI, error) { return client, nil }
	result, err := run(plugins.Request{Host: "node", Port: "3000", Format: "raw", Fields: map[string]string{"namespace": "test", "set": "users"}})
	lines := strings.Split(result.Raw, "\n")
	var first, second map[string]any
	if err != nil || len(lines) != 2 || json.Unmarshal([]byte(lines[0]), &first) != nil || json.Unmarshal([]byte(lines[1]), &second) != nil || first["name"] != "Ada" || second["name"] != "Grace" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
