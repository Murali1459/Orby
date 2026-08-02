package plugins

import (
	"reflect"
	"testing"
)

// Aerospike's binary unpacker returns CDT map bins as map[any]any (unlike
// encoding/json, which always uses map[string]any). WalkJSON must normalize
// those keys to strings so the result survives json.Marshal.
func TestWalkJSONNormalizesAerospikeCDTMaps(t *testing.T) {
	input := map[string]any{
		"name": "Ada",
		"tags": map[any]any{
			"primary": "engineer",
			1:         "one",
		},
		"nested": map[string]any{
			"list": []any{map[any]any{"k": int64(7)}},
		},
	}
	got := WalkJSON(input, nil)
	want := map[string]any{
		"name": "Ada",
		"tags": map[string]any{
			"primary": "engineer",
			"1":       "one",
		},
		"nested": map[string]any{
			"list": []any{map[string]any{"k": int64(7)}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WalkJSON = %#v, want %#v", got, want)
	}
	if _, err := MarshalJSON(got, "  "); err != nil {
		t.Fatalf("MarshalJSON failed on normalized value: %v", err)
	}
}
