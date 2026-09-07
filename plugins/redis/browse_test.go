package redis

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"orby/plugins"
)

func TestParseBrowse(t *testing.T) {
	tests := []struct {
		query   string
		pattern string
		limit   int
		wantErr bool
	}{
		{query: "BROWSE", pattern: "*", limit: 200},
		{query: "BROWSE *", pattern: "*", limit: 200},
		{query: "BROWSE user:*", pattern: "user:*", limit: 200},
		{query: "browse session:*", pattern: "session:*", limit: 200},
		{query: "BROWSE user:* LIMIT 50", pattern: "user:*", limit: 50},
		{query: "BROWSE * LIMIT 1000", pattern: "*", limit: 500},
		{query: "BROWSE LIMIT 7", pattern: "*", limit: 7},
		{query: "BROWSE * 20", pattern: "*", limit: 20},
		{query: "BROWSE user:* 50", pattern: "user:*", limit: 50},
		{query: "BROWSE 20", pattern: "*", limit: 20},
		{query: "BROWSE 2026:*", pattern: "2026:*", limit: 200},
		{query: "BROWSE * 1000", pattern: "*", limit: 500},
		{query: "BROWSE 20 user:*", pattern: "user:*", limit: 20},
		{query: "BROWSE * 0", wantErr: true},
		{query: "BROWSE * -5", wantErr: true},
		{query: "BROWSE * 20 30", wantErr: true},
		{query: "BROWSE * LIMIT 20 LIMIT 30", wantErr: true},
		{query: "BROWSE user:* LIMIT", wantErr: true},
		{query: "BROWSE user:* LIMIT 0", wantErr: true},
		{query: "BROWSE LIMIT nope", wantErr: true},
		{query: "BROWSE one two", wantErr: true},
		{query: "GET user:*", wantErr: true},
		{query: "", wantErr: true},
	}
	for _, tt := range tests {
		pattern, limit, err := parseBrowse(tt.query)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("parseBrowse(%q) expected error", tt.query)
			}
			continue
		}
		if err != nil || pattern != tt.pattern || limit != tt.limit {
			t.Fatalf("parseBrowse(%q) = %q, %d, %v", tt.query, pattern, limit, err)
		}
	}
}

func TestRunBrowseGroupsAndEnriches(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{
		scanPages:  [][]string{{"user:1042", "user:88", "session:9"}},
		typesByKey: map[string]string{"user:1042": "hash", "user:88": "string", "session:9": "set"},
		ttlsByKey:  map[string]int64{"user:1042": 7200, "user:88": -1, "session:9": 300},
	}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }

	result, err := runRedis(queryRequest{Query: "BROWSE *", Host: "node", Port: "6379", Fields: map[string]string{"dbIndex": "0"}})
	if err != nil {
		t.Fatal(err)
	}
	wantGroups := []plugins.BrowseGroup{
		{Prefix: "session:", Total: 1, Keys: []plugins.BrowseKey{{Name: "session:9", Type: "set", TTL: 300}}},
		{Prefix: "user:", Total: 2, Keys: []plugins.BrowseKey{
			{Name: "user:1042", Type: "hash", TTL: 7200},
			{Name: "user:88", Type: "string"},
		}},
	}
	if !reflect.DeepEqual(result.Browse.Groups, wantGroups) {
		t.Fatalf("groups = %#v, want %#v", result.Browse.Groups, wantGroups)
	}
	if result.Browse.Pattern != "*" || result.Browse.Scanned != 3 || result.Browse.Limited || result.Browse.Cluster {
		t.Fatalf("browse view = %#v", result.Browse)
	}
	if result.Format != "browse" || !result.HasCount || result.RowCount != 3 || result.CountUnit != "key" || !result.Succeeded {
		t.Fatalf("result = %#v", result)
	}
	if result.State["query"] != "BROWSE *" {
		t.Fatalf("state = %#v", result.State)
	}
	for _, call := range client.calls {
		if strings.HasPrefix(call, "SCAN ") {
			continue
		}
		t.Fatalf("unexpected client call %q (browse must not Execute raw commands)", call)
	}
}

func TestRunBrowseDropsVanishedKeys(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{
		scanPages:  [][]string{{"gone:1"}},
		typesByKey: map[string]string{"gone:1": "string"},
		ttlsByKey:  map[string]int64{"gone:1": -2},
	}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }
	result, err := runRedis(queryRequest{Query: "BROWSE gone:*", Host: "node", Port: "6379"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Browse.Groups) != 1 || len(result.Browse.Groups[0].Keys) != 0 || result.Browse.Groups[0].Total != 1 {
		t.Fatalf("groups = %#v", result.Browse.Groups)
	}
}

func TestRunBrowseCapsGroupAtEight(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	keys := make([]string, 0, 10)
	for index := range 10 {
		keys = append(keys, "user:"+string(rune('a'+index)))
	}
	types := map[string]string{}
	ttls := map[string]int64{}
	for _, key := range keys {
		types[key] = "string"
		ttls[key] = -1
	}
	client := &fakeRedis{scanPages: [][]string{keys}, typesByKey: types, ttlsByKey: ttls}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }
	result, err := runRedis(queryRequest{Query: "BROWSE user:*", Host: "node", Port: "6379"})
	if err != nil {
		t.Fatal(err)
	}
	group := result.Browse.Groups[0]
	if len(group.Keys) != browseGroupCap || group.Total != 10 {
		t.Fatalf("group = %#v", group)
	}
}

func TestRunBrowseMarksLimitedOnCappedScan(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{scanPages: [][]string{{"a:1", "a:2"}, {"b:1"}}}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }
	result, err := runRedis(queryRequest{Query: "BROWSE * LIMIT 2", Host: "node", Port: "6379"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Browse.Limited || result.Browse.Scanned != 2 {
		t.Fatalf("browse view = %#v", result.Browse)
	}
}

func TestRunBrowseStopsExactlyAtLimitWithinPage(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{
		scanPages:  [][]string{{"a:1", "a:2", "a:3", "a:4"}},
		typesByKey: map[string]string{"a:1": "string", "a:2": "string"},
		ttlsByKey:  map[string]int64{"a:1": -1, "a:2": -1},
	}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }
	result, err := runRedis(queryRequest{Query: "BROWSE * LIMIT 2", Host: "node", Port: "6379"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Browse.Scanned != 2 || !result.Browse.Limited {
		t.Fatalf("browse view = %#v (limit must not overshoot a SCAN page)", result.Browse)
	}
}

func TestRunBrowseSkipsEmptyScanPages(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{
		scanPages:  [][]string{{}, {"user:1", "user:2"}, {"session:9"}},
		typesByKey: map[string]string{"user:1": "string", "user:2": "string", "session:9": "set"},
		ttlsByKey:  map[string]int64{"user:1": -1, "user:2": -1, "session:9": -1},
	}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }
	result, err := runRedis(queryRequest{Query: "BROWSE *", Host: "node", Port: "6379"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Browse.Scanned != 3 || result.Browse.Limited {
		t.Fatalf("browse view = %#v (empty first page must not stop the scan)", result.Browse)
	}
	if len(result.Browse.Groups) != 2 {
		t.Fatalf("groups = %#v", result.Browse.Groups)
	}
}

type stalledCursorRedis struct {
	fakeRedis
}

func (client *stalledCursorRedis) Scan(ctx context.Context, cursor string, pattern string, count int64) ([]string, string, error) {
	client.calls = append(client.calls, "SCAN STALLED")
	return []string{"a:1"}, "7", nil
}

func TestRunBrowseStopsOnCursorWithoutProgress(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &stalledCursorRedis{fakeRedis{typesByKey: map[string]string{"a:1": "string"}, ttlsByKey: map[string]int64{"a:1": -1}}}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }
	result, err := runRedis(queryRequest{Query: "BROWSE a:*", Host: "node", Port: "6379"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Browse.Scanned != 1 {
		t.Fatalf("browse view = %#v (stalled cursor must stop after two round trips)", result.Browse)
	}
	if scans := strings.Count(strings.Join(client.calls, " "), "SCAN"); scans != 2 {
		t.Fatalf("expected two SCAN round trips (advance then stall), got %d", scans)
	}
}

func TestRunBrowseClusterMergesAllPages(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{
		scanPages:  [][]string{{"user:1"}, {"order:9"}, {"user:2"}},
		typesByKey: map[string]string{"user:1": "string", "order:9": "hash", "user:2": "string"},
		ttlsByKey:  map[string]int64{"user:1": -1, "order:9": -1, "user:2": -1},
	}
	redisClientFactory = func(cluster bool, addresses []address, db int) (redisClient, error) {
		if !cluster {
			t.Fatalf("cluster browse must create a cluster client")
		}
		return client, nil
	}
	result, err := runRedis(queryRequest{Query: "BROWSE *", Mode: "cluster", Host: "node", Port: "6379"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Browse.Cluster || result.Browse.Scanned != 3 || result.Browse.Limited {
		t.Fatalf("browse view = %#v", result.Browse)
	}
	if len(result.Browse.Groups) != 2 {
		t.Fatalf("groups = %#v", result.Browse.Groups)
	}
}

func TestRunBrowseClusterDedupesAcrossNodes(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{
		scanPages:  [][]string{{"user:1", "user:2"}, {"user:2", "user:3"}},
		typesByKey: map[string]string{"user:1": "string", "user:2": "string", "user:3": "string"},
		ttlsByKey:  map[string]int64{"user:1": -1, "user:2": -1, "user:3": -1},
	}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }
	result, err := runRedis(queryRequest{Query: "BROWSE user:*", Mode: "cluster", Host: "node", Port: "6379"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Browse.Scanned != 3 {
		t.Fatalf("browse view = %#v (duplicate key counted twice)", result.Browse)
	}
}

func TestRunBrowseClusterMarksLimited(t *testing.T) {
	original := redisClientFactory
	defer func() { redisClientFactory = original }()
	client := &fakeRedis{scanPages: [][]string{{"a:1", "a:2"}, {"b:1"}}}
	redisClientFactory = func(bool, []address, int) (redisClient, error) { return client, nil }
	result, err := runRedis(queryRequest{Query: "BROWSE * LIMIT 2", Mode: "cluster", Host: "node", Port: "6379"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Browse.Limited || result.Browse.Scanned != 2 || !result.Browse.Cluster {
		t.Fatalf("browse view = %#v", result.Browse)
	}
}
