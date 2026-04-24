package kongfig_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	envprovider "github.com/pmarschik/kongfig/providers/env"
	"github.com/pmarschik/kongfig/schema"
)

type splitConfig struct {
	Name string   `kongfig:"name"`
	Tags []string `kongfig:"tags,sep=','"`
	Path []string `kongfig:"search.path,sep=':'"`
}

type splitNested struct {
	Inner splitConfig `kongfig:"cfg"`
}

func TestSplitPaths_Basic(t *testing.T) {
	got := schema.SplitPaths[splitConfig]()
	if got["tags"] != "," {
		t.Errorf("tags sep: got %q, want %q", got["tags"], ",")
	}
	if got["search.path"] != ":" {
		t.Errorf("search.path sep: got %q, want %q", got["search.path"], ":")
	}
	if _, ok := got["name"]; ok {
		t.Error("name should not be in split paths")
	}
}

func TestSplitPaths_NonSliceIgnored(t *testing.T) {
	type cfg struct {
		Name string `kongfig:"name,sep=','"`
	}
	got := schema.SplitPaths[cfg]()
	if len(got) != 0 {
		t.Errorf("expected no splits for non-slice field, got %v", got)
	}
}

func TestSplitPaths_StructSliceIgnored(t *testing.T) {
	type inner struct {
		Host string `kongfig:"host"`
	}
	type cfg struct {
		Servers []inner `kongfig:"servers,sep=','"`
	}
	got := schema.SplitPaths[cfg]()
	if _, ok := got["servers"]; ok {
		t.Error("slice of structs should not be registered as a split path")
	}
}

func TestSplitPaths_Nested(t *testing.T) {
	got := schema.SplitPaths[splitNested]()
	if got["cfg.tags"] != "," {
		t.Errorf("cfg.tags sep: got %q, want %q", got["cfg.tags"], ",")
	}
	if got["cfg.search.path"] != ":" {
		t.Errorf("cfg.search.path sep: got %q, want %q", got["cfg.search.path"], ":")
	}
}

func TestNewFor_SplitTransform(t *testing.T) {
	type cfg struct {
		Tags []string `kongfig:"tags,sep=','"`
	}
	kf := kongfig.NewFor[cfg]()
	kf.MustLoad(context.Background(), &staticProvider{
		data:   map[string]any{"tags": "foo,bar,baz"},
		source: "env.prefix",
	})

	got, err := kongfig.Get[cfg](kf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 3 || got.Tags[0] != "foo" || got.Tags[1] != "bar" || got.Tags[2] != "baz" {
		t.Errorf("Tags: got %v, want [foo bar baz]", got.Tags)
	}
}

func TestNewFor_SplitTransform_NonStringPassthrough(t *testing.T) {
	// Pre-parsed slice (e.g. from YAML) should pass through unchanged.
	type cfg struct {
		Tags []string `kongfig:"tags,sep=','"`
	}
	kf := kongfig.NewFor[cfg]()
	kf.MustLoad(context.Background(), &staticProvider{
		data:   map[string]any{"tags": []string{"a", "b"}},
		source: "file",
	})

	got, err := kongfig.Get[cfg](kf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "a" || got.Tags[1] != "b" {
		t.Errorf("Tags: got %v, want [a b]", got.Tags)
	}
}

func TestNewFor_SplitTransform_EscapedSep(t *testing.T) {
	// \, in the value is a literal comma, not a split boundary.
	type cfg struct {
		Tags []string `kongfig:"tags,sep=','"`
	}
	kf := kongfig.NewFor[cfg]()
	kf.MustLoad(context.Background(), &staticProvider{
		data:   map[string]any{"tags": `foo\,bar,baz`},
		source: "env.prefix",
	})

	got, err := kongfig.Get[cfg](kf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "foo,bar" || got.Tags[1] != "baz" {
		t.Errorf("Tags: got %v, want [foo,bar baz]", got.Tags)
	}
}

func TestEnvRenderer_SliceJoin(t *testing.T) {
	p := envprovider.Provider("APP", "_")
	r := p.Bind(plainStyler{})

	ctx := kongfig.SplitSepKey.WithCtx(context.Background(), map[string]string{
		"tags": ",",
	})

	data := kongfig.ConfigData{"tags": []string{"foo", "bar", "baz"}}
	var buf bytes.Buffer
	if err := r.Render(ctx, &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "TAGS=foo,bar,baz") {
		t.Errorf("expected TAGS=foo,bar,baz in output, got: %s", out)
	}
}

func TestEnvRenderer_SliceJoin_DefaultSep(t *testing.T) {
	// No SplitSepKey registered → falls back to ","
	p := envprovider.Provider("APP", "_")
	r := p.Bind(plainStyler{})

	data := kongfig.ConfigData{"tags": []string{"x", "y"}}
	var buf bytes.Buffer
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "TAGS=x,y") {
		t.Errorf("expected TAGS=x,y in output, got: %s", out)
	}
}

func TestMapSplitPaths_Basic(t *testing.T) {
	type cfg struct {
		Labels map[string]string `kongfig:"labels,sep=',',kvsep='='"`
		Env    map[string]string `kongfig:"env,sep=';',kvsep='='"`
		Name   string            `kongfig:"name"`
	}
	got := schema.MapSplitPaths[cfg]()
	if got["labels"].Sep != "," || got["labels"].KVSep != "=" {
		t.Errorf("labels: got %+v", got["labels"])
	}
	if got["env"].Sep != ";" || got["env"].KVSep != "=" {
		t.Errorf("env: got %+v", got["env"])
	}
	if _, ok := got["name"]; ok {
		t.Error("name (non-map) should not appear in map split paths")
	}
}

func TestMapSplitPaths_StructValueIgnored(t *testing.T) {
	type inner struct{ X string }
	type cfg struct {
		M map[string]inner `kongfig:"m,sep=',',kvsep='='"`
	}
	got := schema.MapSplitPaths[cfg]()
	if _, ok := got["m"]; ok {
		t.Error("map of structs should not be registered as a map split path")
	}
}

func TestNewFor_MapSplitTransform(t *testing.T) {
	type cfg struct {
		Labels map[string]string `kongfig:"labels,sep=',',kvsep='='"`
	}
	kf := kongfig.NewFor[cfg]()
	kf.MustLoad(context.Background(), &staticProvider{
		data:   map[string]any{"labels": "env=prod,region=us-east"},
		source: "env.prefix",
	})

	got, err := kongfig.Get[cfg](kf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Labels["env"] != "prod" || got.Labels["region"] != "us-east" {
		t.Errorf("Labels: got %v", got.Labels)
	}
}

func TestNewFor_MapSplitTransform_NonStringPassthrough(t *testing.T) {
	type cfg struct {
		Labels map[string]string `kongfig:"labels,sep=',',kvsep='='"`
	}
	kf := kongfig.NewFor[cfg]()
	kf.MustLoad(context.Background(), &staticProvider{
		data:   map[string]any{"labels": map[string]any{"env": "prod"}},
		source: "file",
	})

	got, err := kongfig.Get[cfg](kf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Labels["env"] != "prod" {
		t.Errorf("Labels: got %v", got.Labels)
	}
}

func TestNewFor_MapSplitTransform_EscapedKeySep(t *testing.T) {
	// \, in the value is a literal comma within a key, not a pair boundary.
	type cfg struct {
		Labels map[string]string `kongfig:"labels,sep=',',kvsep='='"`
	}
	kf := kongfig.NewFor[cfg]()
	kf.MustLoad(context.Background(), &staticProvider{
		data:   map[string]any{"labels": `env=prod,region\,eu=us-east`},
		source: "env.prefix",
	})

	got, err := kongfig.Get[cfg](kf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Labels["env"] != "prod" || got.Labels["region,eu"] != "us-east" {
		t.Errorf("Labels: got %v", got.Labels)
	}
}

func TestEnvRenderer_MapJoin(t *testing.T) {
	p := envprovider.Provider("APP", "_")
	r := p.Bind(plainStyler{})

	ctx := kongfig.MapSplitSpecKey.WithCtx(context.Background(), map[string]schema.MapSplitSpec{
		"labels": {Sep: ",", KVSep: "="},
	})

	data := kongfig.ConfigData{"labels": map[string]string{"env": "prod", "region": "us-east"}}
	var buf bytes.Buffer
	if err := r.Render(ctx, &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "LABELS=env=prod,region=us-east") {
		t.Errorf("expected LABELS=env=prod,region=us-east in output, got: %s", out)
	}
}

func TestEnvRenderer_MapJoin_DefaultSep(t *testing.T) {
	p := envprovider.Provider("APP", "_")
	r := p.Bind(plainStyler{})

	data := kongfig.ConfigData{"labels": map[string]string{"a": "1", "b": "2"}}
	var buf bytes.Buffer
	if err := r.Render(context.Background(), &buf, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "LABELS=a=1,b=2") {
		t.Errorf("expected LABELS=a=1,b=2 in output, got: %s", out)
	}
}

type plainStyler struct{}

func (plainStyler) Key(s string) string           { return s }
func (plainStyler) String(s string) string        { return s }
func (plainStyler) Number(s string) string        { return s }
func (plainStyler) Bool(s string) string          { return s }
func (plainStyler) Null(s string) string          { return s }
func (plainStyler) Syntax(s string) string        { return s }
func (plainStyler) Comment(s string) string       { return s }
func (plainStyler) Annotation(_, s string) string { return s }
func (plainStyler) SourceKind(s string) string    { return s }
func (plainStyler) SourceData(s string) string    { return s }
func (plainStyler) SourceKey(s string) string     { return s }
func (plainStyler) Redacted(s string) string      { return s }
func (plainStyler) Codec(s string) string         { return s }
