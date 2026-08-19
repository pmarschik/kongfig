package toml_test

import (
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
)

// Marshal drops nil-valued keys because TOML has no null. Block tables already
// did; inline tables emitted "k = nil", which is not parseable TOML.
func TestMarshal_InlineTableOmitsNilValues(t *testing.T) {
	p := tomlparser.New(tomlparser.WithInlineTables("fields.*"))
	data := kongfig.ConfigData{
		"fields": kongfig.ConfigData{
			"summary": kongfig.ConfigData{"jira": "summary", "link": nil, "type": "text"},
		},
	}

	b, err := p.Marshal(data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if strings.Contains(string(b), "nil") {
		t.Errorf("inline table wrote a nil value:\n%s", b)
	}
	if _, err := p.Unmarshal(b); err != nil {
		t.Fatalf("written TOML does not parse back: %v\n%s", err, b)
	}
}

// The same holds for a nil nested one level deeper, and for a nil carried in a
// RenderedValue wrapper.
func TestMarshal_InlineTableOmitsNestedAndWrappedNil(t *testing.T) {
	p := tomlparser.New(tomlparser.WithInlineTables("fields.*"))
	data := kongfig.ConfigData{
		"fields": kongfig.ConfigData{
			"summary": kongfig.ConfigData{
				"jira":  "summary",
				"link":  kongfig.RenderedValue{Value: nil},
				"extra": kongfig.ConfigData{"kept": 1, "gone": nil},
			},
		},
	}

	b, err := p.Marshal(data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if strings.Contains(string(b), "nil") {
		t.Errorf("inline table wrote a nil value:\n%s", b)
	}
	got, err := p.Unmarshal(b)
	if err != nil {
		t.Fatalf("written TOML does not parse back: %v\n%s", err, b)
	}

	fields, ok := got["fields"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("fields = %T, want ConfigData:\n%s", got["fields"], b)
	}
	summary, ok := fields["summary"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("fields.summary = %T, want ConfigData:\n%s", fields["summary"], b)
	}
	if summary["jira"] != "summary" {
		t.Errorf("fields.summary.jira = %v, want \"summary\":\n%s", summary["jira"], b)
	}
	extra, ok := summary["extra"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("fields.summary.extra = %T, want ConfigData:\n%s", summary["extra"], b)
	}
	if _, present := extra["gone"]; present {
		t.Errorf("nested nil key survived:\n%s", b)
	}
	if extra["kept"] != int64(1) {
		t.Errorf("fields.summary.extra.kept = %#v, want 1:\n%s", extra["kept"], b)
	}
}

// Arrays of tables written as key/value lines take the same path.
func TestMarshal_InlineTableArrayOmitsNilValues(t *testing.T) {
	data := kongfig.ConfigData{
		"rules": []any{kongfig.ConfigData{"match": "a", "org": nil}},
	}

	b, err := tomlparser.Default.Marshal(data)
	if err != nil {
		t.Fatal("marshal:", err)
	}
	if strings.Contains(string(b), "nil") {
		t.Errorf("inline table array wrote a nil value:\n%s", b)
	}
	if _, err := tomlparser.Default.Unmarshal(b); err != nil {
		t.Fatalf("written TOML does not parse back: %v\n%s", err, b)
	}
}
