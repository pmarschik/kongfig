package toml_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	kongfig "github.com/pmarschik/kongfig"
	tomlparser "github.com/pmarschik/kongfig/parsers/toml"
)

// marshalFixture exercises the value kinds whose TOML spelling differs from Go's
// default formatting: floats need a fractional part, keys need quoting, times
// need RFC 3339, and nil has no TOML spelling at all.
func marshalFixture() kongfig.ConfigData {
	return kongfig.ConfigData{
		"name":    "app",
		"port":    int64(8080),
		"ratio":   2.0,
		"enabled": true,
		"missing": nil,
		"tags":    []any{"a", "b"},
		"server": kongfig.ConfigData{
			"host":      "localhost",
			"weird.key": "quoted",
			"tls": kongfig.ConfigData{
				"enabled": true,
			},
		},
	}
}

func TestMarshal_RoundTrips(t *testing.T) {
	p := tomlparser.New(tomlparser.WithInlineTables("server.tls"))

	b, err := p.Marshal(marshalFixture())
	if err != nil {
		t.Fatal("marshal:", err)
	}
	got, err := p.Unmarshal(b)
	if err != nil {
		t.Fatalf("unmarshal %q: %v", b, err)
	}

	if got["ratio"] != 2.0 {
		t.Errorf("ratio = %#v, want 2.0 (float, not int)", got["ratio"])
	}
	if _, ok := got["missing"]; ok {
		t.Errorf("nil value survived as %#v; want the key dropped", got["missing"])
	}
	server, ok := got["server"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("server = %T, want ConfigData", got["server"])
	}
	if server["weird.key"] != "quoted" {
		t.Errorf("server[\"weird.key\"] = %#v, want \"quoted\"", server["weird.key"])
	}
	tls, ok := server["tls"].(kongfig.ConfigData)
	if !ok {
		t.Fatalf("server.tls = %T, want ConfigData", server["tls"])
	}
	if tls["enabled"] != true {
		t.Errorf("server.tls.enabled = %#v, want true", tls["enabled"])
	}
}

func TestMarshal_TimeRoundTrips(t *testing.T) {
	want := time.Date(2026, 8, 19, 12, 30, 0, 0, time.UTC)

	b, err := tomlparser.Default.Marshal(kongfig.ConfigData{"created": want})
	if err != nil {
		t.Fatal("marshal:", err)
	}
	got, err := tomlparser.Default.Unmarshal(b)
	if err != nil {
		t.Fatalf("unmarshal %q: %v", b, err)
	}

	ts, ok := got["created"].(time.Time)
	if !ok {
		t.Fatalf("created = %T (%q), want time.Time", got["created"], b)
	}
	if !ts.Equal(want) {
		t.Errorf("created = %v, want %v", ts, want)
	}
}

// Marshal and the renderer share one emitter; this pins them together so a
// render-only change cannot silently alter what gets written to disk.
func TestMarshal_MatchesCommentFreePlainRender(t *testing.T) {
	p := tomlparser.New(tomlparser.WithInlineTables("server.tls"))
	data := marshalFixture()
	delete(data, "missing") // the renderer shows nil; Marshal drops it

	b, err := p.Marshal(data)
	if err != nil {
		t.Fatal("marshal:", err)
	}

	ctx := kongfig.RenderNoCommentsKey.WithCtx(context.Background(), true)
	var buf bytes.Buffer
	if err := p.Bind(plainStyler{}).Render(ctx, &buf, data); err != nil {
		t.Fatal("render:", err)
	}

	want := strings.TrimLeft(buf.String(), "\n")
	if string(b) != want {
		t.Errorf("Marshal and plain render diverged\nmarshal:\n%s\nrender:\n%s", b, want)
	}
}
