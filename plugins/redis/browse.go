package redis

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"orby/plugins"
)

const (
	browseDefaultLimit = 200
	browseMaxLimit     = 500
	browseBatchSize    = 100
	browseGroupCap     = 8 // keys shown per group before "+N more"
)

// parseBrowse extracts the SCAN pattern and optional LIMIT count from a BROWSE query.
func parseBrowse(query string) (pattern string, limit int, err error) {
	tokens, err := tokenizeRedis(query)
	if err != nil {
		return "", 0, err
	}
	if len(tokens) == 0 || !strings.EqualFold(tokens[0], "BROWSE") {
		return "", 0, fmt.Errorf("BROWSE expects a pattern like \"BROWSE user:*\"")
	}
	pattern = "*"
	limit = browseDefaultLimit
	limitSeen := false
	args := tokens[1:]
	for index := 0; index < len(args); index++ {
		switch strings.ToUpper(args[index]) {
		case "LIMIT":
			if index+1 >= len(args) {
				return "", 0, fmt.Errorf("BROWSE LIMIT requires a count")
			}
			index++
			value, parseErr := browseLimitCount(args[index])
			if parseErr != nil {
				return "", 0, parseErr
			}
			if limitSeen {
				return "", 0, fmt.Errorf("BROWSE accepts one pattern and an optional LIMIT count")
			}
			limit, limitSeen = value, true
		default:
			if value, atoiErr := strconv.Atoi(args[index]); atoiErr == nil {
				if value < 1 {
					return "", 0, fmt.Errorf("BROWSE LIMIT must be a positive integer")
				}
				if limitSeen {
					return "", 0, fmt.Errorf("BROWSE accepts one pattern and an optional LIMIT count")
				}
				if value > browseMaxLimit {
					value = browseMaxLimit
				}
				limit, limitSeen = value, true
				continue
			}
			if pattern != "*" {
				return "", 0, fmt.Errorf("BROWSE accepts one pattern and an optional LIMIT count")
			}
			pattern = args[index]
		}
	}
	return pattern, limit, nil
}

func browseLimitCount(text string) (int, error) {
	value, err := strconv.Atoi(text)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("BROWSE LIMIT must be a positive integer")
	}
	if value > browseMaxLimit {
		value = browseMaxLimit
	}
	return value, nil
}

// browsePrefix groups a key under the text up to and including its first colon.
// Keys without a colon are grouped under "(other)".
func browsePrefix(key string) string {
	if index := strings.IndexByte(key, ':'); index >= 0 {
		return key[:index+1]
	}
	return "(other)"
}

func runBrowse(client redisClient, request queryRequest) (queryResult, error) {
	pattern, limit, err := parseBrowse(request.Query)
	if err != nil {
		return queryResult{}, err
	}
	cluster := strings.EqualFold(strings.TrimSpace(request.Mode), "cluster")
	started := time.Now()
	var scanned []string
	var limited bool
	if cluster {
		scanned, limited, err = client.ScanCluster(pattern, browseBatchSize, limit)
	} else {
		scanned, limited, err = scanKeys(client, pattern, limit)
	}
	if err != nil {
		return queryResult{}, err
	}
	groups, err := browseGroups(scanned, client)
	if err != nil {
		return queryResult{}, err
	}
	result := queryResult{
		Tool: "redis", Query: request.Query, Format: "browse", Profile: plugins.ProfileName(request.ConnectionName),
		DurationMS: time.Since(started).Milliseconds(), State: map[string]string{"query": request.Query},
		Succeeded: true, CountUnit: "key", RowCount: len(scanned), HasCount: true,
		Browse: &plugins.BrowseView{Pattern: pattern, Groups: groups, Scanned: len(scanned), Limited: limited, Cluster: cluster},
	}
	return result, nil
}

// scanKeys walks a single host's keyspace with cursor-based SCAN pages.
func scanKeys(client redisClient, pattern string, limit int) ([]string, bool, error) {
	scanned := []string{}
	seen := map[string]struct{}{}
	limited := false
	cursor := "0"
	for len(scanned) < limit {
		keys, next, scanErr := client.Scan(cursor, pattern, browseBatchSize)
		if scanErr != nil {
			return nil, false, scanErr
		}
		for _, key := range keys {
			if _, exists := seen[key]; exists {
				continue // SCAN may return a key on multiple pages
			}
			seen[key] = struct{}{}
			scanned = append(scanned, key)
			if len(scanned) >= limit {
				break
			}
		}
		if len(scanned) >= limit {
			limited = true
			break
		}
		if next == "0" || next == cursor {
			break // full keyspace visited, or a cursor that made no progress
		}
		cursor = next
	}
	return scanned, limited, nil
}

// browseGroups groups scanned keys by prefix and enriches the displayed subset
// with pipelined TYPE/TTL lookups (two round trips total).
func browseGroups(scanned []string, client redisClient) ([]plugins.BrowseGroup, error) {
	grouped := map[string][]string{}
	for _, key := range scanned {
		prefix := browsePrefix(key)
		grouped[prefix] = append(grouped[prefix], key)
	}
	prefixes := make([]string, 0, len(grouped))
	for prefix := range grouped {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	groups := make([]plugins.BrowseGroup, 0, len(prefixes))
	shown := []string{}
	for _, prefix := range prefixes {
		keys := grouped[prefix]
		group := plugins.BrowseGroup{Prefix: prefix, Total: len(keys)}
		if len(keys) > browseGroupCap {
			keys = keys[:browseGroupCap]
		}
		for _, key := range keys {
			group.Keys = append(group.Keys, plugins.BrowseKey{Name: key})
			shown = append(shown, key)
		}
		groups = append(groups, group)
	}
	if len(shown) == 0 {
		return groups, nil
	}
	types, err := client.Types(shown)
	if err != nil {
		return nil, err
	}
	ttls, err := client.TTLs(shown)
	if err != nil {
		return nil, err
	}
	index := 0
	for groupIndex := range groups {
		kept := groups[groupIndex].Keys[:0]
		for keyIndex := range groups[groupIndex].Keys {
			keyType, ttl := types[index], ttls[index]
			index++
			if ttl == -2 {
				continue // key vanished between scan and inspection
			}
			groups[groupIndex].Keys[keyIndex].Type = keyType
			if ttl >= 0 {
				groups[groupIndex].Keys[keyIndex].TTL = ttl
			}
			kept = append(kept, groups[groupIndex].Keys[keyIndex])
		}
		groups[groupIndex].Keys = kept
	}
	return groups, nil
}
