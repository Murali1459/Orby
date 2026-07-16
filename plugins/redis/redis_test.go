package redis

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"pluginvm/plugins"
)

type fakeRedis struct {
	value  any
	calls  []string
	err    error
	pings  int
	closed bool
}

func runRedis(request queryRequest) (queryResult, error) {
	if _, err := validateRedis(request.Query); err != nil {
		return queryResult{}, err
	}
	connection, err := (Plugin{}).Connect(request)
	if err != nil {
		return queryResult{}, err
	}
	defer connection.Close()
	return connection.Run(request)
}

func TestMetadataProvidesComposer(t *testing.T) {
	metadata := Plugin{}.Metadata()
	want := []plugins.ComposerElement{
		{Kind: "literal", Text: "❯"},
		{Kind: "input", Name: "query", Placeholder: "Enter Redis command", Grow: true},
	}
	if !reflect.DeepEqual(metadata.Composer.Elements, want) {
		t.Fatalf("composer = %#v", metadata.Composer)
	}
}

func (client *fakeRedis) Execute(tokens []string) (any, error) {
	client.calls = append(client.calls, strings.Join(tokens, " "))
	return client.value, client.err
}
func (client *fakeRedis) Ping() error  { client.pings++; return client.err }
func (client *fakeRedis) Close() error { client.closed = true; return nil }

func TestPluginConnectionReusesRedisClientUntilClosed(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{value: "value"}
	creates := 0
	redisClientFactory = func(bool, []address, int) (redisClient, error) {
		creates++
		return client, nil
	}

	connection, err := (Plugin{}).Connect(plugins.Request{Host: "redis.internal", Port: "6379", Fields: map[string]string{"dbIndex": "0"}})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := connection.Run(plugins.Request{Query: "GET key", Host: "redis.internal", Port: "6379", Fields: map[string]string{"dbIndex": "0"}}); err != nil {
			t.Fatal(err)
		}
	}
	if creates != 1 || client.pings != 1 || len(client.calls) != 2 || client.closed {
		t.Fatalf("creates=%d pings=%d calls=%v closed=%v", creates, client.pings, client.calls, client.closed)
	}
	if err := connection.Close(); err != nil || !client.closed {
		t.Fatalf("close err=%v closed=%v", err, client.closed)
	}
}

func TestRunRedisExecutesOnceWithoutRewriting(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{value: []any{[]byte("name"), []byte("Ada")}}
	redisClientFactory = func(cluster bool, addresses []address, db int) (redisClient, error) {
		if cluster || db != 0 || !reflect.DeepEqual(addresses, []address{{Host: "redis.internal", Port: 6380}}) {
			t.Fatalf("factory: %v %#v %d", cluster, addresses, db)
		}
		return client, nil
	}
	result, err := runRedis(queryRequest{Query: "HGETALL people", Format: "raw", Host: "redis.internal", Port: "6380", Fields: map[string]string{"dbIndex": "0"}})
	if err != nil || result.Raw != "name\nAda" || !reflect.DeepEqual(client.calls, []string{"HGETALL people"}) || !client.closed {
		t.Fatalf("result=%#v client=%#v err=%v", result, client, err)
	}
}

func TestRunRedisPassesClusterCommandsToClient(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{value: []any{}}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }
	if _, err := runRedis(queryRequest{Query: "SCAN 0", Mode: "cluster", Host: "node", Port: "6379"}); err != nil || !reflect.DeepEqual(client.calls, []string{"SCAN 0"}) {
		t.Fatalf("calls=%#v err=%v", client.calls, err)
	}
}

func TestRunRedisFormatsScalarAsTable(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{value: "hello"}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }
	result, err := runRedis(queryRequest{Query: "GET greeting", Format: "table", Host: "node", Port: "6379"})
	if err != nil || !reflect.DeepEqual(result.Rows, []map[string]any{{"value": "hello"}}) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestRunRedisTableUsesStoredJSONObject(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{value: []byte(`{"campaign_id":"CMP471746","placement_bids":[{"placement":"SEARCH","cpc":205}]}`)}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }

	result, err := runRedis(queryRequest{Query: "GET campaign", Format: "table", Host: "node", Port: "6379"})
	want := []map[string]any{{
		"campaign_id":    "CMP471746",
		"placement_bids": []any{map[string]any{"placement": "SEARCH", "cpc": float64(205)}},
	}}
	if err != nil || !reflect.DeepEqual(result.Rows, want) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestRunRedisFormatsStructuredJSON(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	value := []any{[]byte(`{"name":"Ada"}`), int64(2)}
	client := &fakeRedis{value: value}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }

	result, err := runRedis(queryRequest{Query: "MGET first second", Format: "json", Host: "node", Port: "6379"})
	want := []any{map[string]any{"name": "Ada"}, int64(2)}
	if err != nil || result.IsRaw || !result.HasJSONValue || !reflect.DeepEqual(result.JSONValue, want) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestRunRedisCountsCollectionsButNotScalars(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }

	client.value = []any{[]byte("one"), []byte("two")}
	result, err := runRedis(queryRequest{Query: "KEYS *", Format: "raw", Host: "node", Port: "6379"})
	if err != nil || !result.HasCount || result.RowCount != 2 {
		t.Fatalf("collection result=%#v err=%v", result, err)
	}

	client.value = []byte("hello")
	result, err = runRedis(queryRequest{Query: "GET greeting", Format: "raw", Host: "node", Port: "6379"})
	if err != nil || result.HasCount {
		t.Fatalf("scalar result=%#v err=%v", result, err)
	}
}

func TestRunRedisJSONDecodesStoredJSON(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{value: []byte(`{"targeting":"KEYWORD_TARGETING","placement_bids":[{"placement":"SEARCH","cpc":205}]}`)}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }

	result, err := runRedis(queryRequest{Query: "GET campaign", Format: "json", Host: "node", Port: "6379"})
	want := map[string]any{
		"targeting":      "KEYWORD_TARGETING",
		"placement_bids": []any{map[string]any{"placement": "SEARCH", "cpc": float64(205)}},
	}
	if err != nil || !reflect.DeepEqual(result.JSONValue, want) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestRunRedisJSONKeepsPlainText(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{value: []byte("not JSON")}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }

	result, err := runRedis(queryRequest{Query: "GET greeting", Format: "json", Host: "node", Port: "6379"})
	if err != nil || result.JSONValue != "not JSON" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestRunRedisFormatsRawLikeRedisCLI(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{value: []any{[]byte("first"), []any{[]byte("second"), int64(3)}}}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }

	result, err := runRedis(queryRequest{Query: "MGET first second", Format: "raw", Host: "node", Port: "6379"})
	if err != nil || !result.IsRaw || result.Raw != "first\nsecond\n3" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestRunRedisRejectsAndSanitizes(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	called := false
	redisClientFactory = func(bool, []address, int) (redisClient, error) { called = true; return &fakeRedis{}, nil }
	if _, err := runRedis(queryRequest{Query: "SET key value", Host: "node", Port: "6379"}); err == nil || called {
		t.Fatal("write reached client")
	}
	client := &fakeRedis{err: errors.New("socket exploded")}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }
	_, err := runRedis(queryRequest{Query: "GET key", Host: "node", Port: "6379"})
	if !errors.Is(err, client.err) || !client.closed {
		t.Fatalf("err=%v closed=%v", err, client.closed)
	}
}

func TestTokenizeRedis(t *testing.T) {
	tests := []struct {
		source string
		want   []string
	}{
		{"  MGET\tfirst\nsecond  ", []string{"MGET", "first", "second"}},
		{`MGET 'first key' "second key"`, []string{"MGET", "first key", "second key"}},
		{`GET one\ two`, []string{"GET", "one two"}},
		{`ECHO "say \"hi\""`, []string{"ECHO", `say "hi"`}},
		{"GET ''", []string{"GET", ""}},
	}
	for _, tt := range tests {
		got, err := tokenizeRedis(tt.source)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("tokenizeRedis(%q) = %#v, %v", tt.source, got, err)
		}
	}
	for _, source := range []string{`GET "unfinished`, "GET 'unfinished", `GET trailing\`} {
		if _, err := tokenizeRedis(source); err == nil {
			t.Fatalf("invalid command accepted: %q", source)
		}
	}
}
