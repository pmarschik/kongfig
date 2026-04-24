// Package render provides renderer implementation utilities for kongfig.
// It exports the helpers that parser and provider renderers need:
// value styling ([Value]), source annotation rendering ([Annotation]),
// annotation column alignment ([AlignAnnotations] / [AnnMarker]),
// render-context accessors, and filter helpers.
//
// Sub-packages that implement [kongfig.Renderer] should import this package
// instead of calling the equivalents on the root kongfig package.
package render

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	kongfig "github.com/pmarschik/kongfig"
)

// AnnMarker is the separator used internally between rendered content and its
// source annotation during [AlignAnnotations] two-pass rendering.
// Embed it between the value and annotation on each line:
//
//	line += AnnMarker + "  " + s.Comment("# ") + ann
//
// Then wrap the render call with [AlignAnnotations].
const AnnMarker = "\x00"

// ansiRe strips ANSI escape codes for visual-width measurement.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// VisualWidth returns the visible character width of s, stripping ANSI codes.
func VisualWidth(s string) int {
	return utf8.RuneCountInString(ansiRe.ReplaceAllString(s, ""))
}

// AlignAnnotations post-processes rendered output lines that contain [AnnMarker],
// padding each value part to the same column before its annotation.
// Lines without [AnnMarker] are written as-is. Trailing newline in raw is consumed.
func AlignAnnotations(raw string, w io.Writer) error {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")

	type entry struct {
		content string
		ann     string
		vw      int
	}
	parsed := make([]entry, len(lines))
	maxVW := 0
	for i, line := range lines {
		parts := strings.SplitN(line, AnnMarker, 2)
		e := entry{content: parts[0]}
		if len(parts) == 2 {
			e.ann = parts[1]
		}
		e.vw = VisualWidth(e.content)
		if e.ann != "" && e.vw > maxVW {
			maxVW = e.vw
		}
		parsed[i] = e
	}

	for _, e := range parsed {
		if e.ann == "" {
			_, _ = fmt.Fprintln(w, e.content)
		} else {
			pad := strings.Repeat(" ", maxVW-e.vw+1)
			_, _ = fmt.Fprintln(w, e.content+pad+e.ann)
		}
	}
	return nil
}

// BaseStyler is a no-op [kongfig.Styler] that returns every token unchanged.
// Embed it in a custom Styler struct to inherit pass-through defaults, then
// override only the methods you need:
//
//	type MyStyler struct{ render.BaseStyler }
//	func (MyStyler) Key(s string) string { return bold(s) }
type BaseStyler struct{}

func (BaseStyler) Key(s string) string           { return s }
func (BaseStyler) String(s string) string        { return s }
func (BaseStyler) Number(s string) string        { return s }
func (BaseStyler) Bool(s string) string          { return s }
func (BaseStyler) Null(s string) string          { return s }
func (BaseStyler) Syntax(s string) string        { return s }
func (BaseStyler) Comment(s string) string       { return s }
func (BaseStyler) Annotation(_, s string) string { return s }
func (BaseStyler) SourceKind(s string) string    { return s }
func (BaseStyler) SourceData(s string) string    { return s }
func (BaseStyler) SourceKey(s string) string     { return s }
func (BaseStyler) Redacted(s string) string      { return s }
func (BaseStyler) Codec(s string) string         { return s }

// Ensure BaseStyler implements kongfig.Styler at compile time.
var _ kongfig.Styler = BaseStyler{}

// Value renders v using s, handling [kongfig.RenderedValue] and dispatching to the
// appropriate typed Styler method based on v's Go type.
// formatted is the renderer's own string representation of v (e.g. TOML-quoted,
// JSON-encoded). If v is a RenderedValue and Redacted is true, formatted is ignored
// and s.Redacted is called instead. If Encoded is true (value was produced by a codec),
// s.Codec is called instead of s.String. Renderers should call this instead of
// s.String(formatted) directly so that redaction, codec styling, and type dispatch
// are centralized.
func Value(s kongfig.Styler, v any, formatted string) string {
	if rv, ok := v.(kongfig.RenderedValue); ok {
		if rv.Redacted {
			return s.Redacted(rv.RedactedDisplay)
		}
		if rv.Encoded {
			return s.Codec(formatted)
		}
		return Value(s, rv.Value, formatted)
	}
	switch v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return s.Number(formatted)
	case bool:
		return s.Bool(formatted)
	case nil:
		return s.Null(formatted)
	default:
		return s.String(formatted)
	}
}

// Annotation renders the source annotation for a RenderedValue.
// Returns "" when rv has no source (zero SourceMeta) or when NoComments is active.
// Renderers do not need to check [NoComments] separately before calling this.
func Annotation(ctx context.Context, rv kongfig.RenderedValue, path string, s kongfig.Styler) string {
	if NoComments(ctx) {
		return ""
	}
	if rv.Source == (kongfig.SourceMeta{}) {
		return ""
	}
	return rv.Source.Layer.RenderAnnotation(ctx, s, path)
}

// --- Context read accessors ---

// NoComments reports whether comments should be suppressed in rendered output.
func NoComments(ctx context.Context) bool {
	v, _ := kongfig.RenderNoCommentsKey.Read(ctx)
	return v
}

// HelpTexts returns the per-path help texts from ctx.
// Returns nil when NoComments is active.
func HelpTexts(ctx context.Context) map[string]string {
	if NoComments(ctx) {
		return nil
	}
	v, _ := kongfig.RenderHelpTextsKey.Read(ctx)
	return v
}

// HelpText returns the help text for path from ctx, or "" if not set or if
// NoComments is active.
func HelpText(ctx context.Context, path string) string {
	texts := HelpTexts(ctx)
	if texts == nil {
		return ""
	}
	return texts[path]
}

// FieldNames returns the path → SourceID → field name map from ctx.
func FieldNames(ctx context.Context) kongfig.PathFieldNames {
	return kongfig.PathFieldNames(kongfig.FieldNamesKey.GetAll(ctx))
}

// FieldName returns the field name for path looked up by the SourceID in ctx.
func FieldName(ctx context.Context, path string) string {
	names, ok := kongfig.FieldNamesKey.Get(ctx, path)
	if !ok {
		return ""
	}
	return names[kongfig.SourceIDFromCtx(ctx)]
}

// FileRawPaths reports whether file source annotations should use raw paths.
func FileRawPaths(ctx context.Context) bool {
	v, _ := kongfig.RenderFileRawPathsKey.Read(ctx)
	return v
}

// VerboseSources returns the per-path verbose source list from ctx.
func VerboseSources(ctx context.Context) map[string][]string {
	v, _ := kongfig.RenderVerboseSourcesKey.Read(ctx)
	return v
}

// TTYSize holds terminal dimensions in columns and rows.
// A zero value (both fields 0) means the terminal size is unknown.
type TTYSize struct {
	Cols int // terminal width in columns; 0 = unknown
	Rows int // terminal height in rows; 0 = unknown
}

// TTYSizeKey is the render option key for terminal dimensions.
var TTYSizeKey = kongfig.NewRenderOptionsKey[TTYSize]()

// WithTTYSize sets the terminal dimensions for renderers that adapt their
// output layout to the terminal (e.g. annotation alignment, line wrapping).
// Pass (0, 0) to explicitly clear a previously set size. Renderers are not
// required to honor this hint.
func WithTTYSize(cols, rows int) kongfig.RenderOption {
	return TTYSizeKey.Bind(TTYSize{Cols: cols, Rows: rows})
}

// AlignSources reports whether source annotation alignment is active (true by default).
func AlignSources(ctx context.Context) bool {
	v, _ := kongfig.RenderNoAlignSourcesKey.Read(ctx)
	return !v
}

// --- Filter helpers ---

// BuildFilterSource constructs a FilterSource slice from a map of layer name → show bool.
// Layers with show=false get a "no-<name>" entry.
func BuildFilterSource(layers map[string]bool) []string {
	var filters []string
	for name, show := range layers {
		if !show {
			filters = append(filters, "no-"+name)
		}
	}
	sort.Strings(filters)
	return filters
}

// sourceMatchesPrefix reports whether source equals prefix or starts with "prefix.".
func sourceMatchesPrefix(source, prefix string) bool {
	return source == prefix || strings.HasPrefix(source, prefix+".")
}

// MatchesFilterSource reports whether source passes the given filter list.
// An empty filter list matches everything.
// A "no-<prefix>" entry excludes any source whose label equals or starts with that prefix.
// Any positive entry acts as an allowlist (prefix match); if any positive entries
// exist, only sources matching at least one pass.
//
// Prefix matching: filter "env" matches sources "env", "env.tag", "env.kong", "env.prefix".
// "no-env" excludes all of them.
func MatchesFilterSource(source string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	hasAllowlist := false
	for _, f := range filters {
		if len(f) >= 3 && f[:3] == "no-" {
			if sourceMatchesPrefix(source, f[3:]) {
				return false
			}
		} else {
			hasAllowlist = true
		}
	}
	if hasAllowlist {
		for _, f := range filters {
			if len(f) < 3 || f[:3] != "no-" {
				if sourceMatchesPrefix(source, f) {
					return true
				}
			}
		}
		return false
	}
	return true
}

// FilterSourceFromCtx returns the effective filter source list by merging
// the value stored in ctx with any additional opts. The opts override the ctx value.
func FilterSourceFromCtx(ctx context.Context, opts ...kongfig.RenderOption) []string {
	return kongfig.RenderFilterSourceFromCtx(ctx, opts...)
}
