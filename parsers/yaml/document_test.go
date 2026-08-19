package yaml_test

import (
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
)

const positionsDoc = `db:
  host: localhost
  port: 5432
tags: [a, b]
`

func TestUnmarshalDocument_RecordsPositions(t *testing.T) {
	data, meta, err := yamlparser.Default.UnmarshalDocument([]byte(positionsDoc))
	if err != nil {
		t.Fatalf("UnmarshalDocument: %v", err)
	}
	db, ok := data["db"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("data[db] = %T, want kongfig.ConfigData", data["db"])
	}
	if got := db["host"]; got != "localhost" {
		t.Errorf("db.host = %v, want localhost", got)
	}

	// Positions point at the value, which is where a bad value lives.
	for path, want := range map[string]kongfig.SourcePosition{
		"db":      {Line: 2, Col: 3}, // first key of the nested mapping
		"db.host": {Line: 2, Col: 9},
		"db.port": {Line: 3, Col: 9},
		"tags":    {Line: 4, Col: 7},
	} {
		got, ok := meta.Positions[path]
		if !ok {
			t.Errorf("no position recorded for %q", path)
			continue
		}
		if got != want {
			t.Errorf("position of %q = %+v, want %+v", path, got, want)
		}
	}
}

func TestUnmarshalDocument_LeavesFileToTheProvider(t *testing.T) {
	_, meta, err := yamlparser.Default.UnmarshalDocument([]byte(positionsDoc))
	if err != nil {
		t.Fatalf("UnmarshalDocument: %v", err)
	}
	// A parser only sees bytes; the provider knows the path and fills it in.
	if f := meta.Positions["db.host"].File; f != "" {
		t.Errorf("File = %q, want empty", f)
	}
}

func TestUnmarshalDocument_ReportsKeyOrder(t *testing.T) {
	_, meta, err := yamlparser.Default.UnmarshalDocument([]byte(positionsDoc))
	if err != nil {
		t.Fatalf("UnmarshalDocument: %v", err)
	}
	if got, want := meta.KeyOrder[""], []string{"db", "tags"}; !equalStrings(got, want) {
		t.Errorf("KeyOrder[\"\"] = %v, want %v", got, want)
	}
	if got, want := meta.KeyOrder["db"], []string{"host", "port"}; !equalStrings(got, want) {
		t.Errorf("KeyOrder[\"db\"] = %v, want %v", got, want)
	}
}

func TestUnmarshalDocument_EmptyDocument(t *testing.T) {
	data, meta, err := yamlparser.Default.UnmarshalDocument(nil)
	if err != nil {
		t.Fatalf("UnmarshalDocument: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("data = %v, want empty", data)
	}
	if len(meta.Positions) != 0 {
		t.Errorf("Positions = %v, want none for an empty document", meta.Positions)
	}
}

func TestUnmarshalDocument_SequenceRootErrors(t *testing.T) {
	// Same as Unmarshal: a config document must have a mapping root.
	if _, _, err := yamlparser.Default.UnmarshalDocument([]byte("- a\n- b\n")); err == nil {
		t.Error("UnmarshalDocument(sequence root) = nil error, want a decode error")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
