package file_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	yamlparser "github.com/pmarschik/kongfig/parsers/yaml"
	fileprovider "github.com/pmarschik/kongfig/providers/file"
)

// docParser is a minimal [kongfig.DocumentParser]: it ignores the input and
// reports a fixed document so the provider's plumbing is what's under test.
type docParser struct{ calls *int }

func (docParser) Unmarshal([]byte) (kongfig.ConfigData, error) {
	return kongfig.ConfigData{}, nil
}

func (docParser) Marshal(kongfig.ConfigData) ([]byte, error) { return nil, nil }

func (p docParser) UnmarshalDocument([]byte) (kongfig.ConfigData, kongfig.DocumentMeta, error) {
	if p.calls != nil {
		*p.calls++
	}
	return kongfig.ConfigData{"db": kongfig.ConfigData{"host": "localhost"}},
		kongfig.DocumentMeta{
			KeyOrder:  map[string][]string{"": {"db"}, "db": {"host"}},
			Positions: map[string]kongfig.SourcePosition{"db.host": {Line: 2, Col: 9}},
		}, nil
}

// plainParser implements only [kongfig.Parser] — no document metadata at all.
type plainParser struct{}

func (plainParser) Unmarshal([]byte) (kongfig.ConfigData, error) {
	return kongfig.ConfigData{"db": kongfig.ConfigData{"host": "localhost"}}, nil
}

func (plainParser) Marshal(kongfig.ConfigData) ([]byte, error) { return nil, nil }

func writeConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("db:\n  host: localhost\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestProvider_ProviderData_ReportsPositions(t *testing.T) {
	path := writeConfig(t)
	p := fileprovider.New(path, docParser{})
	if _, err := p.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	ps, ok := p.ProviderData().(kongfig.PositionSupport)
	if !ok {
		t.Fatalf("ProviderData() = %T, want a kongfig.PositionSupport", p.ProviderData())
	}
	got := ps.PositionOf("db.host")
	if got == nil {
		t.Fatal("PositionOf(db.host) = nil, want a position")
	}
	// The provider fills in the file name the parser could not know.
	want := kongfig.SourcePosition{File: path, Line: 2, Col: 9}
	if *got != want {
		t.Errorf("PositionOf(db.host) = %+v, want %+v", *got, want)
	}
	if p := ps.PositionOf("db.port"); p != nil {
		t.Errorf("PositionOf(db.port) = %+v, want nil", *p)
	}
}

func TestProvider_ProviderData_PositionsUseDisplayPathWhenSet(t *testing.T) {
	path := writeConfig(t)
	p := fileprovider.New(path, docParser{})
	if _, err := p.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Positions must point at a path a tool can open, not a "$xdg"-style label.
	ps, ok := p.ProviderData().(kongfig.PositionSupport)
	if !ok {
		t.Fatalf("ProviderData() = %T, want a kongfig.PositionSupport", p.ProviderData())
	}
	if got := ps.PositionOf("db.host").File; got != path {
		t.Errorf("File = %q, want the canonical path %q", got, path)
	}
}

func TestProvider_ProviderData_NoPositionsWithoutDocumentParser(t *testing.T) {
	path := writeConfig(t)
	p := fileprovider.New(path, plainParser{})
	if _, err := p.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	ps, ok := p.ProviderData().(kongfig.PositionSupport)
	if !ok {
		return // no position support at all is a valid answer
	}
	if pos := ps.PositionOf("db.host"); pos != nil {
		t.Errorf("PositionOf(db.host) = %+v, want nil for a parser without positions", *pos)
	}
}

func TestProvider_Load_ParsesOnceWithDocumentParser(t *testing.T) {
	calls := 0
	p := fileprovider.New(writeConfig(t), docParser{calls: &calls})
	if _, err := p.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if calls != 1 {
		t.Errorf("UnmarshalDocument called %d times, want 1", calls)
	}
	if order := p.KeyOrder()["db"]; len(order) != 1 || order[0] != "host" {
		t.Errorf("KeyOrder[db] = %v, want [host] from the same parse", order)
	}
}

func TestPositions_EndToEndThroughProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	doc := "db:\n  host: localhost\n  port: 5432\n"
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	kf := kongfig.New()
	if err := kf.Load(context.Background(), fileprovider.New(path, yamlparser.Default)); err != nil {
		t.Fatalf("Load: %v", err)
	}

	src, ok := kf.SourceFor("db.port")
	if !ok {
		t.Fatal("SourceFor(db.port) reported no source")
	}
	pos := src.Layer.PositionOf("db.port")
	if pos == nil {
		t.Fatal("PositionOf(db.port) = nil, want a position")
	}
	if want := path + ":3:9"; pos.String() != want {
		t.Errorf("PositionOf(db.port) = %q, want %q", pos.String(), want)
	}
}

func TestProvider_Load_StalePositionsCleared(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	p := fileprovider.New(path, docParser{})
	if _, err := p.Load(context.Background()); err != nil {
		t.Fatalf("Load (missing file): %v", err)
	}
	if ps, ok := p.ProviderData().(kongfig.PositionSupport); ok {
		if pos := ps.PositionOf("db.host"); pos != nil {
			t.Errorf("PositionOf(db.host) = %+v, want nil when the file does not exist", *pos)
		}
	}
}
