// Package charming provides a lipgloss-based [kongfig.Styler] backed by a [theme.Set].
//
// Styles are resolved once at construction for efficient rendering.
// Customize colors by registering [ConfigStyleDefs] and/or [LayerStyleDefs] via
// [theme.Registry.RegisterStruct] before resolving the theme set.
package charming

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/lipmark/theme"
)

// Style name constants used by this package.
// All config-specific names use the "config_" prefix to avoid collisions with
// theme builtins. Layer annotation names use the "layer_" prefix.
const (
	ConfigKey           = "config_key"
	ConfigValue         = "config_value"
	ConfigNumber        = "config_number"
	ConfigBool          = "config_bool"
	ConfigNull          = "config_null"
	ConfigComment       = "config_comment"
	ConfigDerived       = "config_derived"
	ConfigRedacted      = "config_redacted"
	ConfigAnnotationKey = "config_annotation_key"
	ConfigCodecValue    = "config_codec_value"
	LayerFlags          = "layer_flags"
	LayerEnv            = "layer_env"
	LayerFile           = "layer_file"
	LayerXDG            = "layer_xdg"
	LayerWorkdir        = "layer_workdir"
	LayerDefaults       = "layer_defaults"
	LayerDerived        = "layer_derived"
)

// ConfigStyleDefs holds style definitions for config rendering.
// Register with [theme.Registry.RegisterStruct] to customize key/value/comment
// and per-source annotation colors.
//
//	reg.RegisterStruct("auto", charming.ConfigStyleDefs{
//	    Key:       theme.StyleDef{Foreground: "#7aa2f7", Bold: true},
//	    Derived:   theme.StyleDef{Foreground: "#e0af68"},
//	    CodecValue: theme.StyleDef{Foreground: "#bb9af7", Italic: true},
//	})
type ConfigStyleDefs struct {
	Key    theme.StyleDef `style:"config_key"`
	Value  theme.StyleDef `style:"config_value"`
	Number theme.StyleDef `style:"config_number"`
	// Bool styles boolean leaf values (true / false). Defaults to theme.Info.
	Bool theme.StyleDef `style:"config_bool"`
	// Null styles null/nil leaf values. Defaults to theme.Muted.
	Null          theme.StyleDef `style:"config_null"`
	Comment       theme.StyleDef `style:"config_comment"`
	Derived       theme.StyleDef `style:"config_derived"`
	Redacted      theme.StyleDef `style:"config_redacted"`
	AnnotationKey theme.StyleDef `style:"config_annotation_key"`
	// CodecValue styles leaf values that were encoded by a registered [kongfig.Codec].
	// Defaults to the config_value style with italic, indicating the displayed string
	// is the codec's canonical representation and may differ from the raw loaded value.
	CodecValue theme.StyleDef `style:"config_codec_value"`
}

// LayerStyleDefs holds per-layer annotation color definitions.
// Names use the "layer_" prefix to avoid conflicts with theme builtins.
//
//	reg.RegisterStruct("auto", charming.LayerStyleDefs{
//	    Flags:    theme.StyleDef{Foreground: "#9ece6a", Bold: true},
//	    Env:      theme.StyleDef{Foreground: "#7dcfff"},
//	    File:     theme.StyleDef{Foreground: "#bb9af7"},
//	    Defaults: theme.StyleDef{Foreground: "#565f89"},
//	})
type LayerStyleDefs struct {
	Flags    theme.StyleDef `style:"layer_flags"`
	Env      theme.StyleDef `style:"layer_env"`
	File     theme.StyleDef `style:"layer_file"`
	XDG      theme.StyleDef `style:"layer_xdg"`
	Workdir  theme.StyleDef `style:"layer_workdir"`
	Defaults theme.StyleDef `style:"layer_defaults"`
	Derived  theme.StyleDef `style:"layer_derived"`
}

// stylerStyles holds pre-resolved lipgloss styles for all rendering concerns.
// Built once at construction by [New]; zero allocation per Render call.
type stylerStyles struct {
	Key           lipgloss.Style
	String        lipgloss.Style
	Number        lipgloss.Style
	Bool          lipgloss.Style
	Null          lipgloss.Style
	Comment       lipgloss.Style
	Derived       lipgloss.Style
	Redacted      lipgloss.Style
	AnnotationKey lipgloss.Style
	CodecValue    lipgloss.Style
	Flags         lipgloss.Style
	Env           lipgloss.Style
	File          lipgloss.Style // also covers xdg and workdir
	Defaults      lipgloss.Style
}

func newStylerStyles(t theme.Set) stylerStyles {
	stringStyle := styleOr(t, ConfigValue, theme.Value)
	// CodecValue defaults to the string style with italic unless explicitly themed.
	codecStyle := func() lipgloss.Style {
		if slices.Contains(t.Names(), ConfigCodecValue) {
			return t.Get(ConfigCodecValue)
		}
		return stringStyle.Italic(true)
	}()
	return stylerStyles{
		Key:           styleOr(t, ConfigKey, theme.Command),
		String:        stringStyle,
		Number:        styleOr(t, ConfigNumber, theme.Value),
		Bool:          styleOr(t, ConfigBool, theme.Info),
		Null:          styleOr(t, ConfigNull, theme.Muted),
		Comment:       styleOr(t, ConfigComment, theme.Muted),
		Derived:       styleOr(t, ConfigDerived, theme.Warning),
		Redacted:      styleOr(t, ConfigRedacted, theme.Error),
		AnnotationKey: styleOr(t, ConfigAnnotationKey, theme.Muted),
		CodecValue:    codecStyle,
		Flags:         styleOr(t, LayerFlags, theme.Success),
		Env:           styleOr(t, LayerEnv, theme.Info),
		File:          styleOr(t, LayerFile, theme.Info),
		Defaults:      styleOr(t, LayerDefaults, theme.Dim),
	}
}

// annotationStyle returns the pre-resolved style for a provenance annotation.
// Handles layer aliasing (xdg/workdir → file, "derived" substring → derived).
func (s stylerStyles) annotationStyle(source string) lipgloss.Style {
	if strings.Contains(source, "derived") {
		return s.Derived
	}
	layer := source
	if before, _, ok := strings.Cut(source, ":"); ok {
		layer = strings.TrimSpace(before)
	}
	switch layer {
	case "flags":
		return s.Flags
	case "env":
		return s.Env
	case "defaults", "default":
		return s.Defaults
	case "xdg", "workdir":
		return s.File
	default:
		return s.File
	}
}

// styleOr returns the resolved style for name if registered in t, otherwise
// falls back to the fallback style name.
func styleOr(t theme.Set, name, fallback string) lipgloss.Style {
	if slices.Contains(t.Names(), name) {
		return t.Get(name)
	}
	return t.Get(fallback)
}

// Styler implements [kongfig.Styler] using pre-resolved lipgloss styles.
type Styler struct {
	styles stylerStyles
}

// New creates a Styler backed by the given theme.Set.
// All styles are resolved at construction time.
func New(t theme.Set) *Styler {
	return &Styler{styles: newStylerStyles(t)}
}

// Key renders a config key token using the configured key style.
func (s *Styler) Key(str string) string { return s.styles.Key.Render(str) }

// String renders a string leaf value using the configured value style.
func (s *Styler) String(str string) string { return s.styles.String.Render(str) }

// Number renders a numeric leaf value using the configured number style.
func (s *Styler) Number(str string) string { return s.styles.Number.Render(str) }

// Bool renders a boolean leaf value using the configured bool style.
func (s *Styler) Bool(str string) string { return s.styles.Bool.Render(str) }

// Null renders a null/nil leaf value using the configured null style.
func (s *Styler) Null(str string) string { return s.styles.Null.Render(str) }

// Syntax renders a structural syntax token (brackets, colons, section markers) using the comment/muted style.
func (s *Styler) Syntax(str string) string { return s.styles.Comment.Render(str) }

// Comment renders a comment token using the configured comment/muted style.
func (s *Styler) Comment(str string) string { return s.styles.Comment.Render(str) }

// Redacted renders a hidden/sensitive value using the configured redacted style.
func (s *Styler) Redacted(str string) string { return s.styles.Redacted.Render(str) }

// Codec renders a codec-encoded leaf value using the configured codec value style.
func (s *Styler) Codec(str string) string { return s.styles.CodecValue.Render(str) }

// Annotation renders str using the pre-resolved style for source.
// xdg and workdir fall back to the file style; "derived" substring matches the derived style.
func (s *Styler) Annotation(source, str string) string {
	return s.styles.annotationStyle(source).Render(str)
}

// SourceKind renders the kind token of a source annotation (e.g. "file", "env")
// using the annotation style for that kind.
func (s *Styler) SourceKind(str string) string {
	return s.styles.annotationStyle(str).Render(str)
}

// SourceData renders the data portion of a source annotation (e.g. a file path)
// using the comment/muted style.
func (s *Styler) SourceData(str string) string {
	return s.styles.Comment.Render(str)
}

// SourceKey renders a source-specific key reference in a source annotation
// (e.g. "$APP_DB_HOST", "--log-level") using the annotation key style.
func (s *Styler) SourceKey(str string) string {
	return s.styles.AnnotationKey.Render(str)
}

var _ kongfig.Styler = (*Styler)(nil)
