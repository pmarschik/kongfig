package schema_test

import (
	"testing"

	"github.com/pmarschik/kongfig/schema"
)

// sortby is a structural option, so it is decoded into a typed field rather than
// left in Extras for every consumer to re-parse.
func TestParseFieldTag_SortBy(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		want string
	}{
		{"rules,sortby=priority", "priority"},
		{"rules,sortby=-priority", "-priority"},
		{"rules,sortby=meta.priority", "meta.priority"},
		{"rules", ""},
	} {
		ft := schema.ParseFieldTag(tc.tag, "Rules")
		if ft.SortBy != tc.want {
			t.Errorf("ParseFieldTag(%q).SortBy = %q, want %q", tc.tag, ft.SortBy, tc.want)
		}
		if len(ft.Extras) != 0 {
			t.Errorf("ParseFieldTag(%q).Extras = %v, want the option consumed", tc.tag, ft.Extras)
		}
	}
}

type sortByRule struct {
	Name     string `kongfig:"name"`
	Priority int    `kongfig:"priority"`
}

type sortByProfile struct {
	Rules map[string]sortByRule `kongfig:"rules,sortby=-priority"`
}

type sortByConfig struct {
	Rules    map[string]sortByRule    `kongfig:"rules,sortby=priority"`
	Profiles map[string]sortByProfile `kongfig:"profiles"`
	Plain    map[string]sortByRule    `kongfig:"plain"`
	Server   sortByRule               `kongfig:"server,sortby=priority"`
}

// The mark names the map whose entries get sorted, not the entries themselves:
// it is the container's children that are put in order. Marks written inside a
// map value type are reached through a "*" segment, as elsewhere in the schema.
func TestKeySortByPaths(t *testing.T) {
	got := schema.KeySortByPaths[sortByConfig]()

	want := map[string]string{
		"rules":            "priority",
		"profiles.*.rules": "-priority",
	}
	if len(got) != len(want) {
		t.Fatalf("KeySortByPaths = %v, want %v", got, want)
	}
	for path, spec := range want {
		if got[path] != spec {
			t.Errorf("KeySortByPaths[%q] = %q, want %q", path, got[path], spec)
		}
	}
}

// A struct's children are distinct fields, ordered by declaration, so sorting
// them by a value inside them means nothing — the mark is ignored rather than
// producing a path no renderer can use.
func TestKeySortByPaths_IgnoresNonMapFields(t *testing.T) {
	if got := schema.KeySortByPaths[sortByConfig]()["server"]; got != "" {
		t.Errorf("KeySortByPaths[\"server\"] = %q, want it left out", got)
	}
}

// Nothing marked means nothing to carry, so callers can skip the option entirely.
func TestKeySortByPaths_NoMarksIsNil(t *testing.T) {
	type plain struct {
		Rules map[string]sortByRule `kongfig:"rules"`
	}
	if got := schema.KeySortByPaths[plain](); got != nil {
		t.Errorf("KeySortByPaths = %v, want nil", got)
	}
}
