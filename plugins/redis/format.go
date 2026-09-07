package redis

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"orby/plugins"
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
	return plugins.WalkJSON(value, func(item any) any {
		text, ok := item.(string)
		if !ok {
			if raw, isBytes := item.([]byte); isBytes {
				text = string(raw)
			} else {
				return item
			}
		}
		var decoded any
		if json.Unmarshal([]byte(text), &decoded) == nil {
			return decoded
		}
		return text
	})
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
	if !redisRawStructured(value) {
		return redisRawScalar(value, false)
	}
	lines := redisRawCollection(value, 0)
	rendered := make([]string, 0, len(lines))
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		text := strings.Repeat("   ", line.depth) + line.text
		for line.container && index+1 < len(lines) && lines[index+1].depth == line.depth+1 {
			index++
			line = lines[index]
			text += " " + line.text
		}
		rendered = append(rendered, text)
	}
	return strings.Join(rendered, "\n")
}

type redisRawLine struct {
	depth     int
	text      string
	container bool
}

func redisRawStructured(value any) bool {
	if value == nil {
		return false
	}
	reflected := reflect.ValueOf(value)
	kind := reflected.Kind()
	if kind == reflect.Slice && reflected.Type().Elem().Kind() == reflect.Uint8 {
		return false
	}
	return kind == reflect.Array || kind == reflect.Slice || kind == reflect.Map
}

func redisRawCollection(value any, depth int) []redisRawLine {
	reflected := reflect.ValueOf(value)
	if reflected.Len() == 0 {
		empty := "(empty array)"
		if reflected.Kind() == reflect.Map {
			empty = "(empty map)"
		}
		return []redisRawLine{{depth: depth, text: empty}}
	}
	if reflected.Kind() != reflect.Map {
		lines := make([]redisRawLine, 0, reflected.Len())
		width := len(strconv.Itoa(reflected.Len()))
		for index := range reflected.Len() {
			lines = append(lines, redisRawItem(reflected.Index(index).Interface(), depth, index+1, width)...)
		}
		return lines
	}

	type mapEntry struct {
		key, value any
		sortKey    string
	}
	entries := make([]mapEntry, 0, reflected.Len())
	iterator := reflected.MapRange()
	for iterator.Next() {
		key, value := iterator.Key().Interface(), iterator.Value().Interface()
		entries = append(entries, mapEntry{key: key, value: value, sortKey: fmt.Sprint(key)})
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].sortKey < entries[right].sortKey })
	lines := make([]redisRawLine, 0, len(entries)*3)
	width := len(strconv.Itoa(len(entries)))
	for index, entry := range entries {
		lines = append(lines, redisRawLine{depth: depth, text: redisRawIndex(index+1, width), container: true})
		lines = append(lines, redisRawItem(entry.key, depth+1, 1, 1)...)
		lines = append(lines, redisRawItem(entry.value, depth+1, 2, 1)...)
	}
	return lines
}

func redisRawItem(value any, depth, index, width int) []redisRawLine {
	prefix := redisRawIndex(index, width)
	if !redisRawStructured(value) {
		return []redisRawLine{{depth: depth, text: prefix + " " + redisRawScalar(value, true)}}
	}
	reflected := reflect.ValueOf(value)
	if reflected.Len() == 0 {
		empty := "(empty array)"
		if reflected.Kind() == reflect.Map {
			empty = "(empty map)"
		}
		return []redisRawLine{{depth: depth, text: prefix + " " + empty}}
	}
	return append([]redisRawLine{{depth: depth, text: prefix, container: true}}, redisRawCollection(value, depth+1)...)
}

func redisRawIndex(index, width int) string {
	return fmt.Sprintf("%*d)", width, index)
}

func redisRawScalar(value any, quoted bool) string {
	if value == nil {
		if quoted {
			return "(nil)"
		}
		return ""
	}
	var text string
	switch value := value.(type) {
	case []byte:
		text = string(value)
	case string:
		text = value
	default:
		return fmt.Sprint(value)
	}
	if quoted {
		return strconv.Quote(text)
	}
	return text
}
