//go:build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	as "github.com/aerospike/aerospike-client-go/v8"
	redis "github.com/redis/go-redis/v9"
)

const (
	largeRedisKeys       = 50000
	largeRedisListItems  = 50000
	largeRedisHashFields = 25000
	largeAerospikeRows   = 5000
)

func TestDockerLargeOutputs(t *testing.T) {
	redisHost, redisPort := envOr("PLUGINVM_REDIS_HOST", "127.0.0.1"), envOr("PLUGINVM_REDIS_PORT", "16379")
	aerospikeHost, aerospikePortText := envOr("PLUGINVM_AEROSPIKE_HOST", "127.0.0.1"), envOr("PLUGINVM_AEROSPIKE_PORT", "13000")
	aerospikePort, err := strconv.Atoi(aerospikePortText)
	if err != nil {
		t.Fatal(err)
	}

	seedLargeRedis(t, redisHost, redisPort)
	seedLargeAerospike(t, aerospikeHost, aerospikePort)
	app := startDockerTestServer(t)

	redisBase := url.Values{
		"tool": {"redis"}, "host": {redisHost}, "port": {redisPort}, "mode": {"single"},
		"connectionName": {"Large Docker Redis"}, "dbIndex": {"0"},
	}
	redisCases := []struct {
		name, query, format, tail string
	}{
		{"keys raw", "KEYS *", "raw", "bulk:key:49999"},
		{"keys json", "KEYS *", "json", "bulk:key:49999"},
		{"keys table", "KEYS *", "table", "bulk:key:49999"},
		{"list raw", "LRANGE large:list 0 -1", "raw", "list-item-49999"},
		{"list json", "LRANGE large:list 0 -1", "json", "list-item-49999"},
		{"list table", "LRANGE large:list 0 -1", "table", "list-item-49999"},
		{"hash raw", "HGETALL large:hash", "raw", "hash-value-24999"},
		{"hash json", "HGETALL large:hash", "json", "hash-value-24999"},
		{"hash table", "HGETALL large:hash", "table", "hash-value-24999"},
		{"large JSON raw", "GET large:json", "raw", "json-tail-marker"},
		{"large JSON json", "GET large:json", "json", "json-tail-marker"},
		{"large JSON table", "GET large:json", "table", "json-tail-marker"},
	}
	for _, test := range redisCases {
		t.Run("redis "+test.name, func(t *testing.T) {
			values := cloneValues(redisBase)
			values.Set("query", test.query)
			values.Set("format", test.format)
			assertLargeResponse(t, app.URL, values, test.tail)
		})
	}

	aerospikeBase := url.Values{
		"tool": {"aerospike"}, "host": {aerospikeHost}, "port": {aerospikePortText}, "mode": {"cluster"},
		"connectionName": {"Large Docker Aerospike"}, "namespace": {"test"}, "set": {"large_records"},
	}
	for _, format := range []string{"raw", "json", "table"} {
		t.Run("aerospike scan "+format, func(t *testing.T) {
			values := cloneValues(aerospikeBase)
			values.Set("format", format)
			body := assertLargeResponse(t, app.URL, values, "record-04999")
			// Every row contains the marker in both the user key and the name bin.
			if count := strings.Count(body, "record-"); count != largeAerospikeRows*2 {
				t.Fatalf("expected %d records (%d markers), found %d markers", largeAerospikeRows, largeAerospikeRows*2, count)
			}
		})
	}
}

func TestDockerRedisReadTimeout(t *testing.T) {
	host, port := envOr("PLUGINVM_DELAYED_REDIS_HOST", "127.0.0.1"), envOr("PLUGINVM_DELAYED_REDIS_PORT", "18479")
	app := startDockerTestServer(t)
	started := time.Now()
	body, status := postQuery(t, app.URL, url.Values{
		"tool": {"redis"}, "query": {"GET greeting"}, "format": {"raw"},
		"host": {host}, "port": {port}, "mode": {"single"}, "connectionName": {"Delayed Redis"}, "dbIndex": {"0"},
	})
	duration := time.Since(started)
	t.Logf("delayed Redis: duration=%s response_bytes=%d status=%s", duration.Round(time.Millisecond), len(body), status)
	if status != "error" || !strings.Contains(strings.ToLower(body), "timeout") {
		t.Fatalf("expected timeout error, status=%q body=%s", status, shortBody(body))
	}
	if duration < 9*time.Second || duration > 13*time.Second {
		t.Fatalf("expected the configured 10s read timeout, got %s", duration)
	}
}

func assertLargeResponse(t *testing.T, baseURL string, values url.Values, tail string) string {
	t.Helper()
	started := time.Now()
	body, status := postQuery(t, baseURL, values)
	duration := time.Since(started)
	t.Logf("query=%q format=%s duration=%s response_bytes=%d", values.Get("query"), values.Get("format"), duration.Round(time.Millisecond), len(body))
	if status != "success" {
		t.Fatalf("status=%q body=%s", status, shortBody(body))
	}
	if !strings.Contains(body, tail) {
		t.Fatalf("response was truncated or missing tail marker %q; bytes=%d", tail, len(body))
	}
	return body
}

func shortBody(body string) string {
	if len(body) > 500 {
		return body[:500] + "…"
	}
	return body
}

func seedLargeRedis(t *testing.T, host, port string) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: host + ":" + port})
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	for offset := 0; offset < largeRedisKeys; offset += 1000 {
		pipeline := client.Pipeline()
		for index := offset; index < offset+1000 && index < largeRedisKeys; index++ {
			pipeline.Set(ctx, fmt.Sprintf("bulk:key:%05d", index), "value", 0)
		}
		if _, err := pipeline.Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for offset := 0; offset < largeRedisListItems; offset += 1000 {
		items := make([]any, 0, 1000)
		for index := offset; index < offset+1000 && index < largeRedisListItems; index++ {
			items = append(items, fmt.Sprintf("list-item-%05d", index))
		}
		if err := client.RPush(ctx, "large:list", items...).Err(); err != nil {
			t.Fatal(err)
		}
	}
	for offset := 0; offset < largeRedisHashFields; offset += 1000 {
		items := make([]any, 0, 2000)
		for index := offset; index < offset+1000 && index < largeRedisHashFields; index++ {
			items = append(items, fmt.Sprintf("field-%05d", index), fmt.Sprintf("hash-value-%05d", index))
		}
		if err := client.HSet(ctx, "large:hash", items...).Err(); err != nil {
			t.Fatal(err)
		}
	}
	largeJSON, err := json.Marshal(map[string]any{
		"id": "large-json", "payload": strings.Repeat("x", 5*1024*1024), "tail": "json-tail-marker",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, "large:json", largeJSON, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, "greeting", "hello", 0).Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded Redis: keys=%d list=%d hash=%d json_bytes=%d duration=%s", largeRedisKeys, largeRedisListItems, largeRedisHashFields, len(largeJSON), time.Since(started).Round(time.Millisecond))
}

func seedLargeAerospike(t *testing.T, host string, port int) {
	t.Helper()
	client, err := as.NewClient(host, port)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	policy := as.NewWritePolicy(0, 0)
	policy.SendKey = true
	started := time.Now()
	jobs := make(chan int)
	errors := make(chan error, 8)
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				name := fmt.Sprintf("record-%05d", index)
				key, err := as.NewKey("test", "large_records", name)
				if err == nil {
					err = client.Put(policy, key, as.BinMap{"name": name, "group": index % 20, "active": index%2 == 0, "payload": strings.Repeat("a", 128)})
				}
				if err != nil {
					select {
					case errors <- err:
					default:
					}
					return
				}
			}
		}()
	}
	for index := 0; index < largeAerospikeRows; index++ {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	select {
	case err := <-errors:
		t.Fatal(err)
	default:
	}
	t.Logf("seeded Aerospike: records=%d duration=%s", largeAerospikeRows, time.Since(started).Round(time.Millisecond))
}

func TestDockerRedisFunctionality(t *testing.T) {
	host, port := envOr("PLUGINVM_REDIS_HOST", "127.0.0.1"), envOr("PLUGINVM_REDIS_PORT", "16379")
	seedRedis(t, host, port)
	app := startDockerTestServer(t)

	assertConnection(t, app.URL, "redis", host, port)

	tests := []struct {
		name, query, format string
		contains, excludes  []string
		status              string
	}{
		{name: "raw string", query: "GET greeting", format: "raw", contains: []string{">hello<"}, status: "success"},
		{name: "quoted key", query: `GET "spaced key"`, format: "raw", contains: []string{">quoted value<"}, status: "success"},
		{name: "stored JSON", query: "GET campaign:1", format: "json", contains: []string{`class="json-out"`, "campaign_id", "CMP-1", "placement_bids"}, status: "success"},
		{name: "stored JSON table", query: "GET campaign:1", format: "table", contains: []string{"campaign_id", "placement_bids", "CMP-1", `[{&#34;cpc&#34;:205,&#34;placement&#34;:&#34;SEARCH&#34;}]`}, status: "success"},
		{name: "multiple values", query: "MGET greeting missing", format: "json", contains: []string{"hello", "null"}, status: "success"},
		{name: "hash", query: "HGETALL profile:1", format: "raw", contains: []string{"name", "Ada", "age", "36"}, status: "success"},
		{name: "list", query: "LRANGE queue 0 -1", format: "raw", contains: []string{"first\nsecond"}, status: "success"},
		{name: "set", query: "SMEMBERS tags", format: "json", contains: []string{"alpha", "beta"}, status: "success"},
		{name: "sorted set", query: "ZRANGE scores 0 -1 WITHSCORES", format: "table", contains: []string{"Ada", "Grace"}, status: "success"},
		{name: "stream", query: "XRANGE events - +", format: "json", contains: []string{"created", "CMP-1"}, status: "success"},
		{name: "scan", query: "SCAN 0 MATCH campaign:* COUNT 10", format: "raw", contains: []string{"campaign:1"}, status: "success"},
		{name: "keys", query: "KEYS campaign:*", format: "raw", contains: []string{"campaign:1"}, status: "success"},
		{name: "write rejected", query: "SET forbidden value", format: "raw", contains: []string{"read-only mode rejected"}, status: "error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, status := postQuery(t, app.URL, url.Values{
				"tool": {"redis"}, "query": {test.query}, "format": {test.format},
				"host": {host}, "port": {port}, "mode": {"single"}, "connectionName": {"Docker Redis"}, "dbIndex": {"0"},
			})
			if status != test.status {
				t.Fatalf("status=%q body=%s", status, body)
			}
			for _, expected := range test.contains {
				if !strings.Contains(body, expected) {
					t.Fatalf("missing %q in %s", expected, body)
				}
			}
			for _, forbidden := range test.excludes {
				if strings.Contains(body, forbidden) {
					t.Fatalf("unexpected %q in %s", forbidden, body)
				}
			}
		})
	}

	client := redis.NewClient(&redis.Options{Addr: host + ":" + port})
	defer client.Close()
	if value, err := client.Get(context.Background(), "forbidden").Result(); err != redis.Nil || value != "" {
		t.Fatalf("rejected SET changed Redis: value=%q err=%v", value, err)
	}
}

func TestDockerRedisClusterFunctionality(t *testing.T) {
	addresses := []string{"127.0.0.1:17000", "127.0.0.1:17001", "127.0.0.1:17002"}
	client := redis.NewClusterClient(&redis.ClusterOptions{Addrs: addresses})
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	if err := client.Set(ctx, "cluster:greeting", "hello cluster", 0).Err(); err != nil {
		t.Fatal(err)
	}
	app := startDockerTestServer(t)
	hosts := strings.Join(addresses, ",")
	assertConnectionMode(t, app.URL, "redis", hosts, "17000", "cluster")

	body, status := postQuery(t, app.URL, url.Values{
		"tool": {"redis"}, "query": {"GET cluster:greeting"}, "format": {"raw"},
		"host": {hosts}, "port": {"17000"}, "mode": {"cluster"}, "dbIndex": {"0"},
	})
	if status != "success" || !strings.Contains(body, "hello cluster") {
		t.Fatalf("status=%q body=%s", status, body)
	}

	body, status = postQuery(t, app.URL, url.Values{
		"tool": {"redis"}, "query": {"GET cluster:greeting"}, "format": {"raw"},
		"host": {hosts}, "port": {"17000"}, "mode": {"cluster"}, "dbIndex": {"1"},
	})
	if status != "error" || !strings.Contains(body, "cluster supports DB index 0 only") {
		t.Fatalf("status=%q body=%s", status, body)
	}
}

func TestDockerAerospikeFunctionality(t *testing.T) {
	host, portText := envOr("PLUGINVM_AEROSPIKE_HOST", "127.0.0.1"), envOr("PLUGINVM_AEROSPIKE_PORT", "13000")
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	seedAerospike(t, host, port)
	app := startDockerTestServer(t)

	assertConnection(t, app.URL, "aerospike", host, portText)
	t.Run("namespace options", func(t *testing.T) {
		assertOptions(t, app.URL, "/plugin-options?tool=aerospike&resource=namespaces&host="+host+"&port="+portText, "test")
	})
	t.Run("set options", func(t *testing.T) {
		assertOptions(t, app.URL, "/plugin-options?tool=aerospike&resource=sets&host="+host+"&port="+portText+"&namespace=test", "users")
	})
	t.Run("bin options", func(t *testing.T) {
		for _, bin := range []string{"name", "age", "active", "score"} {
			assertOptions(t, app.URL, "/plugin-options?tool=aerospike&resource=bins&host="+host+"&port="+portText+"&namespace=test", bin)
		}
	})

	base := url.Values{
		"tool": {"aerospike"}, "host": {host}, "port": {portText}, "mode": {"cluster"},
		"connectionName": {"Docker Aerospike"}, "namespace": {"test"}, "set": {"users"},
	}

	t.Run("scan JSON", func(t *testing.T) {
		values := cloneValues(base)
		values.Set("format", "json")
		body, status := postQuery(t, app.URL, values)
		if status != "success" || !containsAll(body, `class="json-out"`, "Ada", "Grace", "Linus", "_key") {
			t.Fatalf("status=%q body=%s", status, body)
		}
	})

	t.Run("primary key", func(t *testing.T) {
		values := cloneValues(base)
		values.Set("format", "json")
		values.Set("primaryKey", "u1")
		body, status := postQuery(t, app.URL, values)
		if status != "success" || !strings.Contains(body, "Ada") || strings.Contains(body, "Grace") {
			t.Fatalf("status=%q body=%s", status, body)
		}
	})

	t.Run("missing primary key", func(t *testing.T) {
		values := cloneValues(base)
		values.Set("format", "json")
		values.Set("primaryKey", "missing")
		body, status := postQuery(t, app.URL, values)
		if status != "success" || !strings.Contains(body, `<pre class="json-out">[]</pre>`) {
			t.Fatalf("status=%q body=%s", status, body)
		}
	})

	t.Run("expression filter", func(t *testing.T) {
		values := cloneValues(base)
		values.Set("format", "json")
		values.Set("expression", `{"kind":"condition","bin":"age","type":"integer","operator":"gt","value":"30"}`)
		body, status := postQuery(t, app.URL, values)
		if status != "success" || !containsAll(body, "Grace", "Linus") || strings.Contains(body, "Ada") {
			t.Fatalf("status=%q body=%s", status, body)
		}
	})

	t.Run("boolean filter", func(t *testing.T) {
		values := cloneValues(base)
		values.Set("format", "table")
		values.Set("expression", `{"kind":"condition","bin":"active","type":"boolean","operator":"eq","value":"true"}`)
		body, status := postQuery(t, app.URL, values)
		if status != "success" || !containsAll(body, "Ada", "Grace", "active") || strings.Contains(body, "Linus") {
			t.Fatalf("status=%q body=%s", status, body)
		}
	})

	t.Run("raw", func(t *testing.T) {
		values := cloneValues(base)
		values.Set("format", "raw")
		values.Set("primaryKey", "u2")
		body, status := postQuery(t, app.URL, values)
		if status != "success" || !strings.Contains(body, `&#34;name&#34;:&#34;Grace&#34;`) || strings.Contains(body, "Ada") {
			t.Fatalf("status=%q body=%s", status, body)
		}
	})

	t.Run("invalid expression", func(t *testing.T) {
		values := cloneValues(base)
		values.Set("format", "json")
		values.Set("expression", `{"kind":"condition","bin":"age","type":"integer","operator":"gt","value":"bad"}`)
		body, status := postQuery(t, app.URL, values)
		if status != "error" || !strings.Contains(body, "requires an integer value") {
			t.Fatalf("status=%q body=%s", status, body)
		}
	})
}

func seedRedis(t *testing.T, host, port string) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: host + ":" + port})
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	commands := []redis.Cmder{
		client.Set(ctx, "greeting", "hello", 0),
		client.Set(ctx, "spaced key", "quoted value", 0),
		client.Set(ctx, "campaign:1", `{"campaign_id":"CMP-1","placement_bids":[{"placement":"SEARCH","cpc":205}]}`, 0),
		client.HSet(ctx, "profile:1", "name", "Ada", "age", 36),
		client.RPush(ctx, "queue", "first", "second"),
		client.SAdd(ctx, "tags", "alpha", "beta"),
		client.ZAdd(ctx, "scores", redis.Z{Score: 1, Member: "Ada"}, redis.Z{Score: 2, Member: "Grace"}),
		client.XAdd(ctx, &redis.XAddArgs{Stream: "events", Values: map[string]any{"event": "created", "campaign": "CMP-1"}}),
	}
	for _, command := range commands {
		if err := command.Err(); err != nil {
			t.Fatal(err)
		}
	}
}

func seedAerospike(t *testing.T, host string, port int) {
	t.Helper()
	client, err := as.NewClient(host, port)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	policy := as.NewWritePolicy(0, 0)
	records := []struct {
		key  string
		bins as.BinMap
	}{
		{"u1", as.BinMap{"name": "Ada", "age": 29, "active": true, "score": 9.5}},
		{"u2", as.BinMap{"name": "Grace", "age": 37, "active": true, "score": 8.75}},
		{"u3", as.BinMap{"name": "Linus", "age": 45, "active": false, "score": 7.0}},
	}
	for _, record := range records {
		key, err := as.NewKey("test", "users", record.key)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Put(policy, key, record.bins); err != nil {
			t.Fatal(err)
		}
	}
}

func startDockerTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler, err := newServer()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(func() { server.Close(); handler.Close() })
	return server
}

func assertConnection(t *testing.T, baseURL, tool, host, port string) {
	assertConnectionMode(t, baseURL, tool, host, port, "single")
}

func assertConnectionMode(t *testing.T, baseURL, tool, host, port, mode string) {
	t.Helper()
	response, err := http.PostForm(baseURL+"/connect", url.Values{"tool": {tool}, "host": {host}, "port": {port}, "mode": {mode}, "connectionId": {tool + "-integration"}})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"reachable":true`) {
		t.Fatalf("connect=%d %s", response.StatusCode, body)
	}
}

func assertOptions(t *testing.T, baseURL, path, expected string) {
	t.Helper()
	response, err := http.Get(baseURL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"value":"`+expected+`"`) {
		t.Fatalf("options=%d %s", response.StatusCode, body)
	}
}

func postQuery(t *testing.T, baseURL string, values url.Values) (string, string) {
	t.Helper()
	response, err := http.PostForm(baseURL+"/query", values)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return string(body), response.Header.Get("X-PluginVM-Result")
}

func cloneValues(source url.Values) url.Values {
	copy := url.Values{}
	for key, values := range source {
		copy[key] = append([]string(nil), values...)
	}
	return copy
}

func containsAll(value string, expected ...string) bool {
	for _, item := range expected {
		if !strings.Contains(value, item) {
			return false
		}
	}
	return true
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func waitFor(t *testing.T, description string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if err := check(); err == nil {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("%s: %v", description, err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func dockerAddress(host, port string) string { return fmt.Sprintf("%s:%s", host, port) }
