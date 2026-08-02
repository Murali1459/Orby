package redis

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"orby/plugins"
)

type fakeRedis struct {
	value  any
	calls  []string
	err    error
	pings  int
	closed bool

	scanPages  [][]string // keys per Scan page, in order
	scanCalls  int
	typesByKey map[string]string
	ttlsByKey  map[string]int64
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
		{Kind: "literal", Text: "❯", Decorative: true},
		{Kind: "input", Name: "query", Placeholder: "Enter Redis command", Grow: true},
	}
	if !reflect.DeepEqual(metadata.Composer.Elements, want) {
		t.Fatalf("composer = %#v", metadata.Composer)
	}
	names := make([]string, len(metadata.Commands))
	for index, command := range metadata.Commands {
		names[index] = command.Name
	}
	if !reflect.DeepEqual(names, redisCommandNames()) {
		t.Fatalf("metadata commands = %#v, want %#v", names, redisCommandNames())
	}
}

func redisCommandNames() []string {
	names := make([]string, len(redisCommands))
	for index, command := range redisCommands {
		names[index] = command.Name
	}
	return names
}

func TestMetadataCommandsMatchAllowList(t *testing.T) {
	commands := Plugin{}.Metadata().Commands
	allowed := redisReadCommands
	for _, command := range commands {
		if !allowed[command.Name] {
			t.Fatalf("metadata command %q is not in the execution allow-list", command.Name)
		}
	}
	for name := range allowed {
		matched := false
		for _, command := range commands {
			if command.Name == name {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("allow-listed command %q has no metadata entry", name)
		}
	}
}

func (client *fakeRedis) Execute(tokens []string) (any, error) {
	client.calls = append(client.calls, strings.Join(tokens, " "))
	return client.value, client.err
}
func (client *fakeRedis) Ping() error  { client.pings++; return client.err }
func (client *fakeRedis) Close() error { client.closed = true; return nil }

func (client *fakeRedis) Scan(cursor string, pattern string, count int64) ([]string, string, error) {
	client.calls = append(client.calls, "SCAN "+cursor+" MATCH "+pattern+" COUNT "+strconv.FormatInt(count, 10))
	if client.err != nil {
		return nil, "", client.err
	}
	page := client.scanCalls
	if page >= len(client.scanPages) {
		return nil, "0", nil
	}
	client.scanCalls++
	next := "0"
	if page+1 < len(client.scanPages) {
		next = strconv.Itoa(page + 1)
	}
	return client.scanPages[page], next, nil
}

func (client *fakeRedis) ScanCluster(pattern string, count int64, limit int) ([]string, bool, error) {
	client.calls = append(client.calls, "CLUSTER SCAN MATCH "+pattern+" COUNT "+strconv.FormatInt(count, 10))
	if client.err != nil {
		return nil, false, client.err
	}
	seen := map[string]struct{}{}
	keys := []string{}
	for _, page := range client.scanPages { // each page stands in for one master's page
		for _, key := range page {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
			if len(keys) >= limit {
				return keys, true, nil
			}
		}
	}
	return keys, false, nil
}

func (client *fakeRedis) Types(keys []string) ([]string, error) {
	types := make([]string, len(keys))
	for index, key := range keys {
		types[index] = client.typesByKey[key]
	}
	return types, nil
}

func (client *fakeRedis) TTLs(keys []string) ([]int64, error) {
	ttls := make([]int64, len(keys))
	for index, key := range keys {
		ttls[index] = client.ttlsByKey[key]
	}
	return ttls, nil
}

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
	if _, err := runRedis(queryRequest{Query: "GET key", Mode: "cluster", Host: "node", Port: "6379"}); err != nil || !reflect.DeepEqual(client.calls, []string{"GET key"}) {
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
	result, err := runRedis(queryRequest{Query: "SMEMBERS people", Format: "raw", Host: "node", Port: "6379"})
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

func TestRunRedisRejectsScanCommands(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	called := false
	redisClientFactory = func(bool, []address, int) (redisClient, error) { called = true; return &fakeRedis{}, nil }
	for _, query := range []string{"SCAN 0", "KEYS *", "SSCAN people 0", "HSCAN people 0", "ZSCAN people 0"} {
		_, err := runRedis(queryRequest{Query: query, Host: "node", Port: "6379"})
		if err == nil || called {
			t.Fatalf("scan command %q reached client", query)
		}
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
