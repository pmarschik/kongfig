package kongfig_test

import (
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

// A decoder is free to hand back a slice typed more precisely than []any —
// github.com/BurntSushi/toml returns []map[string]any for an array of tables.
// Normalization has to reach the maps inside it, or the same data parses to two
// different shapes depending on whether the author wrote [[aux]] blocks or an
// inline array of inline tables, and every consumer has to type-switch on both.
func TestToConfigData_NormalizesTypedMapSlices(t *testing.T) {
	for _, tc := range []struct {
		in   any
		name string
	}{
		{name: "[]map[string]any", in: []map[string]any{{"name": "a"}, {"name": "b"}}},
		{name: "[]ConfigData", in: []kongfig.ConfigData{{"name": "a"}, {"name": "b"}}},
		{name: "[]any", in: []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := kongfig.ToConfigData(map[string]any{"aux": tc.in})["aux"].([]any)
			if !ok {
				t.Fatalf("aux is %T, want []any", kongfig.ToConfigData(map[string]any{"aux": tc.in})["aux"])
			}
			if len(got) != 2 {
				t.Fatalf("aux has %d elements, want 2", len(got))
			}
			for i, want := range []string{"a", "b"} {
				elem, ok := got[i].(kongfig.ConfigData)
				if !ok {
					t.Fatalf("aux[%d] is %T, want kongfig.ConfigData", i, got[i])
				}
				if elem["name"] != want {
					t.Errorf("aux[%d][name] = %v, want %v", i, elem["name"], want)
				}
			}
		})
	}
}

// Only slices that carry maps are rebuilt. A []string stays the slice the caller
// gave us: rebuilding it as []any would cost an allocation per leaf and hand
// renderers a shape they would have to reflect over anyway.
func TestToConfigData_LeavesScalarSlicesAlone(t *testing.T) {
	got := kongfig.ToConfigData(map[string]any{"tags": []string{"a", "b"}})["tags"]
	if _, ok := got.([]string); !ok {
		t.Errorf("tags is %T, want []string", got)
	}
}

// Maps nested deeper than one slice are reached too.
func TestToConfigData_NormalizesNestedTypedSlices(t *testing.T) {
	data := kongfig.ToConfigData(map[string]any{
		"groups": [][]map[string]any{{{"name": "a"}}},
	})
	outer, ok := data["groups"].([]any)
	if !ok {
		t.Fatalf("groups is %T, want []any", data["groups"])
	}
	inner, ok := outer[0].([]any)
	if !ok {
		t.Fatalf("groups[0] is %T, want []any", outer[0])
	}
	if _, ok := inner[0].(kongfig.ConfigData); !ok {
		t.Fatalf("groups[0][0] is %T, want kongfig.ConfigData", inner[0])
	}
}

// A map keyed by something other than string is data, not a sub-tree: ConfigData
// cannot hold it, so it is left as it is rather than half-converted.
func TestToConfigData_LeavesNonStringKeyedMapsAlone(t *testing.T) {
	got := kongfig.ToConfigData(map[string]any{"byPort": map[int]string{80: "http"}})["byPort"]
	if _, ok := got.(map[int]string); !ok {
		t.Errorf("byPort is %T, want map[int]string", got)
	}
}
