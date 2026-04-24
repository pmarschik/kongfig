package kongfig_test

import (
	"context"
	"io"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

// Compile-time interface satisfaction checks.
// These will fail to compile if the interfaces change incompatibly.

// mockProvider implements Provider.
type mockProvider struct{}

func (mockProvider) Load(_ context.Context) (kongfig.ConfigData, error) { return nil, nil }
func (mockProvider) ProviderInfo() kongfig.ProviderInfo                 { return kongfig.ProviderInfo{Name: "mock"} }

// mockByteProvider implements ByteProvider.
type mockByteProvider struct{}

func (mockByteProvider) LoadBytes(_ context.Context) ([]byte, error) { return nil, nil }

// mockWatchProvider implements WatchProvider.
type mockWatchProvider struct{}

func (mockWatchProvider) Load(_ context.Context) (kongfig.ConfigData, error) { return nil, nil }
func (mockWatchProvider) ProviderInfo() kongfig.ProviderInfo {
	return kongfig.ProviderInfo{Name: "mock.watch"}
}

func (mockWatchProvider) Watch(_ context.Context, _ kongfig.WatchFunc) error {
	return nil
}

// mockParser implements Parser.
type mockParser struct{}

func (mockParser) Unmarshal(_ []byte) (kongfig.ConfigData, error) { return nil, nil }
func (mockParser) Marshal(_ kongfig.ConfigData) ([]byte, error)   { return nil, nil }

// mockStyler implements Styler.
type mockStyler struct{}

func (mockStyler) Key(s string) string           { return s }
func (mockStyler) String(s string) string        { return s }
func (mockStyler) Number(s string) string        { return s }
func (mockStyler) Bool(s string) string          { return s }
func (mockStyler) Null(s string) string          { return s }
func (mockStyler) Syntax(s string) string        { return s }
func (mockStyler) Comment(s string) string       { return s }
func (mockStyler) Annotation(_, s string) string { return s }
func (mockStyler) SourceKind(s string) string    { return s }
func (mockStyler) SourceData(s string) string    { return s }
func (mockStyler) SourceKey(s string) string     { return s }
func (mockStyler) Redacted(s string) string      { return s }
func (mockStyler) Codec(s string) string         { return s }

// mockRenderer implements Renderer.
type mockRenderer struct{}

func (mockRenderer) Render(_ context.Context, _ io.Writer, _ kongfig.ConfigData) error {
	return nil
}

// mockOutputProvider implements OutputProvider.
type mockOutputProvider struct{}

func (mockOutputProvider) Bind(_ kongfig.Styler) kongfig.Renderer { return nil }

var (
	_ kongfig.Provider       = mockProvider{}
	_ kongfig.ByteProvider   = mockByteProvider{}
	_ kongfig.WatchProvider  = mockWatchProvider{}
	_ kongfig.Parser         = mockParser{}
	_ kongfig.Styler         = mockStyler{}
	_ kongfig.Renderer       = mockRenderer{}
	_ kongfig.OutputProvider = mockOutputProvider{}
	_ kongfig.GetOption      = kongfig.Strict()
	_ kongfig.GetOption      = kongfig.At("foo")
)

func TestInterfacesCompile(_ *testing.T) {
	// If this file compiles, all interface checks pass.
}
