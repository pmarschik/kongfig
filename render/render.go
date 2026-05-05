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
	"os"
	"regexp"
	"sort"
	"strconv"
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
// Use [AlignAnnotationsCtx] to enable TTY-width-aware above-line fallback.
func AlignAnnotations(raw string, w io.Writer) error {
	return writeAlignedAnnotations(raw, w, 0)
}

// AlignAnnotationsCtx is like [AlignAnnotations] but reads the terminal width from
// ctx (set via [WithTTYSize]). When the terminal is too narrow to fit the annotation
// inline (maxValueWidth + 1 + maxAnnotationWidth > cols), the annotation is instead
// written as a comment line above the value line, indented to match the value.
func AlignAnnotationsCtx(ctx context.Context, raw string, w io.Writer) error {
	tty, _ := TTYSizeKey.Read(ctx)
	return writeAlignedAnnotations(raw, w, tty.Cols)
}

//nolint:gocognit,cyclop // multi-pass annotation layout algorithm, intentional
func writeAlignedAnnotations(raw string, w io.Writer, cols int) error {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")

	type entry struct {
		content string
		ann     string
		vw      int
		annVW   int
	}
	parsed := make([]entry, len(lines))
	for i, line := range lines {
		parts := strings.SplitN(line, AnnMarker, 2)
		e := entry{content: parts[0]}
		if len(parts) == 2 {
			e.ann = parts[1]
		}
		e.vw = VisualWidth(e.content)
		if e.ann != "" {
			e.annVW = VisualWidth(e.ann)
		}
		parsed[i] = e
	}

	// Per-line decision: a line's annotation goes above when the line itself
	// (without alignment padding) would overflow the terminal.
	above := make([]bool, len(parsed))
	for i, e := range parsed {
		if e.ann != "" {
			above[i] = cols > 0 && e.vw+1+e.annVW > cols
		}
	}

	// Max content width for inline-annotated lines (alignment target).
	maxInlineVW := 0
	for i, e := range parsed {
		if e.ann != "" && !above[i] && e.vw > maxInlineVW {
			maxInlineVW = e.vw
		}
	}

	// Re-check: alignment padding may push a line over the terminal limit.
	for i, e := range parsed {
		if e.ann == "" || above[i] {
			continue
		}
		if cols > 0 && maxInlineVW+1+e.annVW > cols {
			above[i] = true
		}
	}

	// Recompute after re-check (some lines may have moved above).
	maxInlineVW = 0
	maxAnnVW := 0
	for i, e := range parsed {
		if e.ann != "" && !above[i] {
			if e.vw > maxInlineVW {
				maxInlineVW = e.vw
			}
			if e.annVW > maxAnnVW {
				maxAnnVW = e.annVW
			}
		}
	}

	// Annotation start column: when the terminal is wide enough, push annotations
	// to the right edge so the longest one ends at cols. Otherwise fall back to
	// immediately after the longest content.
	alignCol := maxInlineVW + 1
	if cols > 0 {
		if rightCol := cols - maxAnnVW; rightCol > alignCol {
			alignCol = rightCol
		}
	}

	for i, e := range parsed {
		switch {
		case e.ann == "":
			_, _ = fmt.Fprintln(w, e.content)
		case above[i]:
			indent := leadingWhitespace(e.content)
			_, _ = fmt.Fprintln(w, indent+strings.TrimLeft(e.ann, " "))
			_, _ = fmt.Fprintln(w, e.content)
		default:
			_, _ = fmt.Fprintln(w, e.content+strings.Repeat(" ", alignCol-e.vw)+e.ann)
		}
	}
	return nil
}

// leadingWhitespace returns the leading spaces/tabs from s.
func leadingWhitespace(s string) string {
	for i, c := range s {
		if c != ' ' && c != '\t' {
			return s[:i]
		}
	}
	return s
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

// HelpText returns the help text for path from ctx, or "" if not set, not matched,
// or NoComments is active.
//
// Matching prefers an exact key first, then falls back to the longest registered key
// that is a strict prefix of path (separated by "." or "["). This means a help text
// registered for "labels" matches rendered leaf paths like "labels.key1" or
// "labels[0]", making it natural to annotate map and slice fields via
// schema.HelpTextPaths.
//
// When [kongfig.WithRenderHelpTextsOnce] is active, the matched help text key is
// marked as seen on first use; subsequent calls for paths matched by the same key
// return "" so the comment is emitted at most once per render call.
func HelpText(ctx context.Context, path string) string {
	texts := HelpTexts(ctx)
	if len(texts) == 0 {
		return ""
	}
	text, matchedKey := helpTextMatch(texts, path)
	if text == "" {
		return ""
	}
	seenPtr, _ := kongfig.RenderHelpTextsSeenKey.Read(ctx)
	if seenPtr != nil {
		if (*seenPtr)[matchedKey] {
			return ""
		}
		(*seenPtr)[matchedKey] = true
	}
	return text
}

// helpTextMatch returns the help text and the key it was matched under for path.
// Exact match wins; otherwise the longest prefix key wins.
// A key k is a prefix of path when path == k+".<suffix>" or path == k+"[<suffix>".
func helpTextMatch(texts map[string]string, path string) (text, key string) {
	if t, ok := texts[path]; ok {
		return t, path
	}
	for k, t := range texts {
		if !strings.HasPrefix(path, k+".") && !strings.HasPrefix(path, k+"[") {
			continue
		}
		if len(k) > len(key) {
			key, text = k, t
		}
	}
	return text, key
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

// WithTTYSizeCtx returns a context with terminal dimensions injected.
// Use in tests or when calling [AlignAnnotationsCtx] directly without going
// through [kongfig.Kongfig.RenderWith].
func WithTTYSizeCtx(ctx context.Context, cols, rows int) context.Context {
	return TTYSizeKey.WithCtx(ctx, TTYSize{Cols: cols, Rows: rows})
}

// TTYSizeFromEnv reads terminal dimensions from the COLUMNS and ROWS
// environment variables that POSIX shells set. Returns (TTYSize, true)
// when at least one variable is set to a positive integer; the other
// field defaults to 0. Returns (TTYSize{}, false) when neither is set.
func TTYSizeFromEnv() (TTYSize, bool) {
	cols, _ := strconv.Atoi(os.Getenv("COLUMNS")) //nolint:errcheck // 0 on parse failure is the desired default
	rows, _ := strconv.Atoi(os.Getenv("ROWS"))    //nolint:errcheck // 0 on parse failure is the desired default
	if cols <= 0 && rows <= 0 {
		return TTYSize{}, false
	}
	return TTYSize{Cols: cols, Rows: rows}, true
}

// AlignSources reports whether source annotation alignment is active (true by default).
func AlignSources(ctx context.Context) bool {
	v, _ := kongfig.RenderNoAlignSourcesKey.Read(ctx)
	return !v
}

// BlockCollections reports whether renderers should always use block style for
// collections (slices, maps) instead of inline/flow syntax.
func BlockCollections(ctx context.Context) bool {
	v, _ := kongfig.RenderBlockCollectionsKey.Read(ctx)
	return v
}

// KeyOrder returns the ordered child key names for the given parent dot-path from ctx.
// Returns nil when no order is set or the path is not found in the order map.
// Renderers use this to emit keys in struct field order rather than alphabetically.
func KeyOrder(ctx context.Context, prefix string) []string {
	orders, _ := kongfig.RenderKeyOrderKey.Read(ctx)
	return orders[prefix]
}

// OrderedKeys returns the keys of data sorted by the key order for prefix from ctx,
// with any keys not in the order appended alphabetically.
func OrderedKeys(ctx context.Context, prefix string, data kongfig.ConfigData) []string {
	ordered := KeyOrder(ctx, prefix)
	if len(ordered) == 0 {
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}
	seen := make(map[string]bool, len(data))
	result := make([]string, 0, len(data))
	for _, k := range ordered {
		if _, ok := data[k]; ok {
			result = append(result, k)
			seen[k] = true
		}
	}
	var extra []string
	for k := range data {
		if !seen[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	return append(result, extra...)
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
