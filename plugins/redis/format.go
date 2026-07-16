package redis

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func redisCollectionCount(value any) (int, bool) {
	switch value := value.(type) {
	case []any:
		return len(value), true
	case map[string]any:
		return 1, true
	default:
		return 0, false
	}
}

func redisJSON(value any) any {
	switch value := value.(type) {
	case string:
		var decoded any
		if json.Unmarshal([]byte(value), &decoded) == nil {
			return decoded
		}
		return value
	case []any:
		items := make([]any, len(value))
		for index, item := range value {
			items[index] = redisJSON(item)
		}
		return items
	case map[string]any:
		items := make(map[string]any, len(value))
		for key, item := range value {
			items[key] = redisJSON(item)
		}
		return items
	default:
		return value
	}
}

func normalizeRedis(value any) any {
	switch value := value.(type) {
	case []byte:
		return string(value)
	case []any:
		output := make([]any, len(value))
		for index, item := range value {
			output[index] = normalizeRedis(item)
		}
		return output
	case map[any]any:
		output := map[string]any{}
		for key, item := range value {
			output[fmt.Sprint(normalizeRedis(key))] = normalizeRedis(item)
		}
		return output
	case map[string]any:
		output := map[string]any{}
		for key, item := range value {
			output[key] = normalizeRedis(item)
		}
		return output
	default:
		return value
	}
}

func redisRows(value any) []map[string]any {
	if row, ok := value.(map[string]any); ok {
		return []map[string]any{row}
	}
	items, ok := value.([]any)
	if !ok {
		return []map[string]any{{"value": value}}
	}
	rows := make([]map[string]any, 0, len(items))
	for index, item := range items {
		if row, ok := item.(map[string]any); ok {
			rows = append(rows, row)
		} else {
			rows = append(rows, map[string]any{"index": index, "value": item})
		}
	}
	return rows
}

func redisRaw(value any) string {
	switch value := value.(type) {
	case nil:
		return ""
	case []any:
		items := make([]string, len(value))
		for index, item := range value {
			items[index] = redisRaw(item)
		}
		return strings.Join(items, "\n")
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		items := make([]string, 0, len(keys)*2)
		for _, key := range keys {
			items = append(items, key, redisRaw(value[key]))
		}
		return strings.Join(items, "\n")
	default:
		return fmt.Sprint(value)
	}
}
