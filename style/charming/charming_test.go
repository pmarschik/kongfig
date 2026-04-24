package charming_test

import (
	"strings"
	"testing"

	charming "github.com/pmarschik/kongfig/style/charming"
	"github.com/pmarschik/lipmark/theme"
)

func makeSet() theme.Set {
	reg := theme.NewWithOptions(theme.WithDefaults())
	reg.RegisterStruct("test", charming.LayerStyleDefs{
		Flags:    theme.StyleDef{Foreground: "#9ece6a", Bold: true},
		Env:      theme.StyleDef{Foreground: "#7dcfff"},
		File:     theme.StyleDef{Foreground: "#bb9af7"},
		Defaults: theme.StyleDef{Foreground: "#565f89"},
	})
	return reg.Resolve("test")
}

func TestNew(t *testing.T) {
	s := charming.New(makeSet())
	if s == nil {
		t.Fatal("New returned nil")
	}
}

func TestKeyValueComment(t *testing.T) {
	s := charming.New(makeSet())

	// These should return non-empty strings (lipgloss may add ANSI escapes or not).
	if got := s.Key("host"); !strings.Contains(got, "host") {
		t.Errorf("Key: expected 'host' in %q", got)
	}
	if got := s.String("localhost"); !strings.Contains(got, "localhost") {
		t.Errorf("Value: expected 'localhost' in %q", got)
	}
	if got := s.Comment("# note"); !strings.Contains(got, "# note") {
		t.Errorf("Comment: expected '# note' in %q", got)
	}
}

func TestAnnotationSourceDiff(t *testing.T) {
	s := charming.New(makeSet())

	// Different sources should produce different renderings (different colors).
	flags := s.Annotation("flags", "flags")
	env := s.Annotation("env", "flags")
	if flags == env {
		t.Errorf("Annotation: 'flags' and 'env' sources produced identical output: %q", flags)
	}
}

func TestAnnotationUnknownSource(t *testing.T) {
	s := charming.New(makeSet())

	// Unknown source should still return the string (plain style fallback).
	got := s.Annotation("unknown-source", "val")
	if !strings.Contains(got, "val") {
		t.Errorf("Annotation: expected 'val' in unknown-source output %q", got)
	}
}
