package yaml_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
)

// Go's %v prints nil as "<nil>", which is not YAML. Unlike TOML, YAML has a
// null literal, so the key stays — it just has to be spelled the way Marshal
// spells it, so rendered output pastes back into a config file.
func TestRender_NilLeafIsNull(t *testing.T) {
	data := kongfig.ConfigData{
		"top": nil,
		"db":  kongfig.ConfigData{"host": "h", "port": nil},
	}

	var buf bytes.Buffer
	if err := yamlparser.Default.Bind(plainStyler{}).Render(context.Background(), &buf, data); err != nil {
		t.Fatal("render:", err)
	}

	out := buf.String()
	if strings.Contains(out, "<nil>") {
		t.Errorf("render wrote Go's nil spelling:\n%s", out)
	}
	got, err := yamlparser.Default.Unmarshal(buf.Bytes())
	if err != nil {
		t.Fatalf("rendered output does not parse: %v\n%s", err, out)
	}
	if v, ok := got["top"]; !ok || v != nil {
		t.Errorf("top = %#v (present=%v), want nil:\n%s", v, ok, out)
	}
	db, ok := got["db"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("db = %T, want ConfigData:\n%s", got["db"], out)
	}
	if v, present := db["port"]; !present || v != nil {
		t.Errorf("db.port = %#v (present=%v), want nil:\n%s", v, present, out)
	}
}

// A nil carried in a RenderedValue takes the same path.
func TestRender_WrappedNilLeafIsNull(t *testing.T) {
	data := kongfig.ConfigData{"link": kongfig.RenderedValue{Value: nil}}

	var buf bytes.Buffer
	if err := yamlparser.Default.Bind(plainStyler{}).Render(context.Background(), &buf, data); err != nil {
		t.Fatal("render:", err)
	}
	if strings.Contains(buf.String(), "<nil>") {
		t.Errorf("render wrote Go's nil spelling:\n%s", buf.String())
	}
	if _, err := yamlparser.Default.Unmarshal(buf.Bytes()); err != nil {
		t.Fatalf("rendered output does not parse: %v\n%s", err, buf.String())
	}
}

// A redacted placeholder has a nil Value but stands in for one, so it must keep
// its placeholder rather than becoming null.
func TestRender_RedactedPlaceholderIsNotNull(t *testing.T) {
	data := kongfig.ConfigData{
		"token": kongfig.RenderedValue{Redacted: true, RedactedDisplay: "***"},
	}

	var buf bytes.Buffer
	if err := yamlparser.Default.Bind(plainStyler{}).Render(context.Background(), &buf, data); err != nil {
		t.Fatal("render:", err)
	}
	if !strings.Contains(buf.String(), "***") {
		t.Errorf("redacted placeholder lost:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "null") {
		t.Errorf("redacted placeholder rendered as null:\n%s", buf.String())
	}
}
