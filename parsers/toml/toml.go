package toml

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	kongfig "github.com/pmarschik/kongfig"
	render "github.com/pmarschik/kongfig/render"
)

// Parser implements [kongfig.Parser] for TOML.
//
// The zero value is ready to use and indents nested tables by two spaces.
// Use [New] with [WithIndent], [WithInlineTables] and [WithInlineMaxKeys] to
// configure layout; the settings apply to both [Parser.Marshal] and rendering.
type Parser struct {
	indent         *string
	inlineMaxKeys  *int
	inlinePatterns []string
}

// defaultIndent is the per-level indentation applied to nested tables when the
// parser does not configure one.
const defaultIndent = "  "

// DefaultInlineMaxKeys is the number of direct keys a marked table may hold and
// still be emitted as an inline table when [WithInlineMaxKeys] is not used.
const DefaultInlineMaxKeys = 3

// effectiveIndent returns the configured indentation, or the default when unset.
func (p Parser) effectiveIndent() string {
	if p.indent == nil {
		return defaultIndent
	}
	return *p.indent
}

// inlinePolicyFor builds the policy shared by Marshal and Render. checkWidth is
// set only on the render path: written files must not depend on the width of the
// terminal that happened to produce them.
func (p Parser) inlinePolicyFor(checkWidth bool) inlinePolicy {
	limit := DefaultInlineMaxKeys
	if p.inlineMaxKeys != nil {
		limit = *p.inlineMaxKeys
	}
	return inlinePolicy{patterns: p.inlinePatterns, defaultMax: limit, checkWidth: checkWidth}
}

// Default is a ready-to-use Parser instance.
var Default = &Parser{}

var (
	_ kongfig.Parser         = Parser{}
	_ kongfig.ParserNamer    = Parser{}
	_ kongfig.OutputProvider = Parser{}
)

// Unmarshal decodes TOML bytes into a map.
func (Parser) Unmarshal(b []byte) (kongfig.ConfigData, error) {
	var out map[string]any
	if err := toml.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return kongfig.ToConfigData(out), nil
}

// UnmarshalWithKeyOrder decodes TOML bytes and also returns the key insertion order
// per parent path from the document. Implements [kongfig.KeyOrderParser].
func (Parser) UnmarshalWithKeyOrder(b []byte) (kongfig.ConfigData, map[string][]string, error) {
	var out map[string]any
	meta, err := toml.Decode(string(b), &out)
	if err != nil {
		return nil, nil, err
	}
	// meta.Keys() returns all keys in document order as dot-delimited paths.
	keyOrder := make(map[string][]string)
	seen := make(map[string]map[string]bool)
	for _, k := range meta.Keys() {
		// toml.Key is a []string (the path segments).
		segments := []string(k)
		if len(segments) == 0 {
			continue
		}
		// The parent path is all segments but the last; the child is the last segment.
		parentSegments := segments[:len(segments)-1]
		child := segments[len(segments)-1]
		parent := strings.Join(parentSegments, ".")
		if seen[parent] == nil {
			seen[parent] = make(map[string]bool)
		}
		if !seen[parent][child] {
			seen[parent][child] = true
			keyOrder[parent] = append(keyOrder[parent], child)
		}
	}
	if len(keyOrder) == 0 {
		keyOrder = nil
	}
	return kongfig.ToConfigData(out), keyOrder, nil
}

// Marshal encodes a map to TOML bytes.
// The returned bytes always end with a trailing newline.
//
// Marshal shares its emitter with [Parser.Bind], so indentation and inline-table
// settings apply equally to written files and to rendered output. It differs
// from rendering in three ways: no comments or source annotations are written,
// terminal width is ignored, and nil values are dropped rather than displayed.
func (p Parser) Marshal(data kongfig.ConfigData) ([]byte, error) {
	return p.MarshalCtx(context.Background(), data)
}

// MarshalCtx is [Parser.Marshal] with a context, so inline-table marks derived
// from ,inline struct tags ([kongfig.InlineTablesKey], injected by
// [kongfig.NewFor]) also apply when writing a config file. Terminal width is
// ignored either way.
func (p Parser) MarshalCtx(ctx context.Context, data kongfig.ConfigData) ([]byte, error) {
	opts := tomlRenderOpts{
		indent:       p.effectiveIndent(),
		inline:       p.inlinePolicyFor(false),
		omitNil:      true,
		omitComments: true,
	}
	opts.inline.ctxPaths = kongfig.InlineTablesKey.GetAll(ctx)
	var buf bytes.Buffer
	if err := renderMap(ctx, &buf, identityStyler{}, kongfig.ToConfigData(data), "", "", 0, opts); err != nil {
		return nil, err
	}
	// The emitter separates top-level sections with a leading blank line, which
	// is noise at the very start of a file.
	return bytes.TrimLeft(buf.Bytes(), "\n"), nil
}

// identityStyler returns every token unchanged, so the emitter produces plain
// TOML for [Parser.Marshal].
type identityStyler struct{}

func (identityStyler) Key(s string) string           { return s }
func (identityStyler) String(s string) string        { return s }
func (identityStyler) Number(s string) string        { return s }
func (identityStyler) Bool(s string) string          { return s }
func (identityStyler) Null(s string) string          { return s }
func (identityStyler) Syntax(s string) string        { return s }
func (identityStyler) Comment(s string) string       { return s }
func (identityStyler) Annotation(_, s string) string { return s }
func (identityStyler) SourceKind(s string) string    { return s }
func (identityStyler) SourceData(s string) string    { return s }
func (identityStyler) SourceKey(s string) string     { return s }
func (identityStyler) Redacted(s string) string      { return s }
func (identityStyler) Codec(s string) string         { return s }

// Format returns the parser's format name for source label composition.
func (Parser) Format() string { return "toml" }

// Extensions returns the file extensions handled by this parser.
func (Parser) Extensions() []string { return []string{".toml"} }

// Bind returns a [kongfig.Renderer] that writes syntax-highlighted TOML.
func (p Parser) Bind(s kongfig.Styler) kongfig.Renderer {
	return &renderer{p: p, s: s}
}

// renderer writes TOML with token-level styling.
type renderer struct {
	s kongfig.Styler
	p Parser
}

// tomlRenderOpts groups the layout options that are computed once per Render call
// and shared across all recursive rendering functions.
type tomlRenderOpts struct {
	indent       string
	inline       inlinePolicy
	cols         int
	forceBlock   bool
	align        bool
	omitNil      bool
	omitComments bool
}

// help returns the help text for path, or "" when comments are suppressed.
func (o tomlRenderOpts) help(ctx context.Context, path string) string {
	if o.omitComments {
		return ""
	}
	return render.HelpText(ctx, path)
}

// ind returns the indentation prefix for content nested depth levels deep.
func (o tomlRenderOpts) ind(depth int) string {
	if o.indent == "" || depth <= 0 {
		return ""
	}
	return strings.Repeat(o.indent, depth)
}

// inlinePolicy decides which tables may collapse into TOML inline tables.
type inlinePolicy struct {
	// ctxPaths are the marks carried in the render context by
	// [kongfig.InlineTablesKey], i.e. those derived from ,inline struct tags.
	// A value of 0 defers to defaultMax.
	ctxPaths map[string]int
	// patterns are the marks configured on the parser via [WithInlineTables].
	patterns   []string
	defaultMax int
	checkWidth bool
}

func (ip inlinePolicy) empty() bool { return len(ip.patterns) == 0 && len(ip.ctxPaths) == 0 }

// maxKeysFor reports whether path is marked for inlining and, if so, how many
// direct keys the table may hold. Patterns are matched segment by segment; a "*"
// segment matches any single segment. When several context marks match, the
// largest explicit limit wins, so the result does not depend on map order.
func (ip inlinePolicy) maxKeysFor(path string) (int, bool) {
	if path == "" {
		return 0, false
	}
	segs := strings.Split(path, ".")

	marked := false
	for _, pat := range ip.patterns {
		if matchPathPattern(strings.Split(pat, "."), segs) {
			marked = true
			break
		}
	}
	explicit := 0
	for pat, n := range ip.ctxPaths {
		if !matchPathPattern(strings.Split(pat, "."), segs) {
			continue
		}
		marked = true
		if n > explicit {
			explicit = n
		}
	}

	if !marked {
		return 0, false
	}
	if explicit > 0 {
		return explicit, true
	}
	return ip.defaultMax, true
}

func matchPathPattern(pat, segs []string) bool {
	if len(pat) != len(segs) {
		return false
	}
	for i, p := range pat {
		if p != "*" && p != segs[i] {
			return false
		}
	}
	return true
}

func (r *renderer) Render(ctx context.Context, w io.Writer, data kongfig.ConfigData) error {
	tty, _ := render.TTYSizeKey.Read(ctx)
	opts := tomlRenderOpts{
		cols:       tty.Cols,
		forceBlock: render.BlockCollections(ctx),
		indent:     r.p.effectiveIndent(),
		inline:     r.p.inlinePolicyFor(true),
	}
	opts.inline.ctxPaths = kongfig.InlineTablesKey.GetAll(ctx)
	if !render.AlignSources(ctx) {
		return renderMap(ctx, w, r.s, data, "", "", 0, opts)
	}
	// Two-pass: render with annotation markers, then align.
	opts.align = true
	var buf bytes.Buffer
	if err := renderMap(ctx, &buf, r.s, data, "", "", 0, opts); err != nil {
		return err
	}
	return render.AlignAnnotationsCtx(ctx, buf.String(), w)
}

// renderMap writes one table level. depth is the number of path segments owned by
// this level: 0 for the document root, 1 for [a], 2 for [a.b]. The table header is
// indented depth-1 levels and the keys it owns depth levels, matching the layout the
// TOML encoder produces for nested tables.
func renderMap(ctx context.Context, w io.Writer, s kongfig.Styler, data kongfig.ConfigData, prefix, tableHeader string, depth int, opts tomlRenderOpts) error {
	keys := render.OrderedKeys(ctx, prefix, data)

	// Scalars first, then tables, then table-arrays (TOML convention: scalars must precede section headers)
	scalars, tables, tableArrs := classifyTOMLKeys(data, keys)
	// Inlined tables and short arrays of tables are key/value lines, so they must
	// join the scalars ahead of any header — anything after a header would belong
	// to that section instead.
	inlines, tables := splitInlineTables(ctx, s, data, tables, prefix, depth, opts)
	inlineArrs, tableArrs := splitTableArrays(data, tableArrs, depth, opts)

	// Every table level owns a header. Top-level tables are preceded by a blank
	// line; nested ones sit directly under their parent.
	if tableHeader != "" {
		if depth <= 1 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s%s\n", opts.ind(depth-1), s.Syntax("[")+s.Key(tomlHeaderPath(tableHeader))+s.Syntax("]"))
	}

	for _, k := range scalars {
		path := buildTOMLPath(prefix, k)
		if err := renderTOMLScalar(ctx, w, s, k, data[k], path, depth, opts); err != nil {
			return err
		}
	}
	for _, in := range inlines {
		renderTOMLInline(ctx, w, s, in, depth, opts)
	}
	for _, k := range inlineArrs {
		path := buildTOMLPath(prefix, k)
		renderTOMLTableArrayInline(ctx, w, s, k, data[k], path, depth, opts)
	}
	for _, k := range tables {
		path := buildTOMLPath(prefix, k)
		if err := renderTOMLTable(ctx, w, s, data[k], path, depth+1, opts); err != nil {
			return err
		}
	}
	for _, k := range tableArrs {
		path := buildTOMLPath(prefix, k)
		if err := renderTOMLTableArrayBlock(ctx, w, s, data[k], path, depth, opts); err != nil {
			return err
		}
	}
	return nil
}

func buildTOMLPath(prefix, k string) string {
	if prefix != "" {
		return prefix + "." + k
	}
	return k
}

func classifyTOMLKeys(data kongfig.ConfigData, keys []string) (scalars, tables, tableArrs []string) {
	for _, k := range keys {
		v := data[k]
		if _, ok := v.(kongfig.ConfigData); ok {
			tables = append(tables, k)
			continue
		}
		if rv, ok := v.(kongfig.RenderedValue); ok {
			if _, isMap := rv.Value.(kongfig.ConfigData); isMap {
				tables = append(tables, k)
				continue
			}
			if isTableArray(rv.Value) {
				tableArrs = append(tableArrs, k)
				continue
			}
		}
		if isTableArray(v) {
			tableArrs = append(tableArrs, k)
			continue
		}
		scalars = append(scalars, k)
	}
	return scalars, tables, tableArrs
}

func renderTOMLScalar(ctx context.Context, w io.Writer, s kongfig.Styler, k string, v any, path string, depth int, opts tomlRenderOpts) error {
	rv, isRV := v.(kongfig.RenderedValue)
	var leafVal any
	if isRV {
		leafVal = rv.Value
	} else {
		leafVal = v
	}

	if leafVal == nil && opts.omitNil {
		return nil
	}

	pad := opts.ind(depth)
	if help := opts.help(ctx, path); help != "" {
		fmt.Fprintf(w, "%s%s\n", pad, s.Comment("# "+help))
	}

	inline := tomlValue(leafVal)
	styledKey := s.Key(tomlKey(k))
	keyW := render.VisualWidth(styledKey) + len(pad)

	if isTOMLArray(leafVal) && (opts.forceBlock || (opts.cols > 0 && keyW+3+render.VisualWidth(inline) > opts.cols)) {
		if isRV && !opts.omitComments {
			if ann := render.Annotation(ctx, rv, path, s); ann != "" {
				fmt.Fprintf(w, "%s%s\n", pad, s.Comment("# ")+ann)
			}
		}
		fmt.Fprintf(w, "%s%s = [\n", pad, styledKey)
		elemPad := pad + "  "
		for _, elem := range toTOMLSlice(leafVal) {
			fmt.Fprintf(w, "%s%s,\n", elemPad, tomlValueStyled(s, elem))
		}
		fmt.Fprintf(w, "%s]\n", pad)
		return nil
	}

	line := pad + styledKey + " = " + render.Value(s, v, inline)
	if isRV {
		line += tomlAnnSuffix(ctx, rv, path, s, opts)
	}
	fmt.Fprintln(w, line)
	return nil
}

// inlineEntry is a table that passed the inline policy, together with the
// already-rendered value so the width check and the emission agree.
type inlineEntry struct {
	key   string
	path  string
	value any
	// rendered is the styled "{k = v, ...}" form, or the redacted placeholder.
	rendered string
}

// splitInlineTables partitions tables into the ones that may be emitted inline
// and the ones that keep their own section header.
func splitInlineTables(ctx context.Context, s kongfig.Styler, data kongfig.ConfigData, tables []string, prefix string, depth int, opts tomlRenderOpts) (inlines []inlineEntry, blocks []string) {
	if opts.inline.empty() {
		return nil, tables
	}
	for _, k := range tables {
		path := buildTOMLPath(prefix, k)
		if e, ok := inlineCandidate(ctx, s, k, data[k], path, depth, opts); ok {
			inlines = append(inlines, e)
			continue
		}
		blocks = append(blocks, k)
	}
	return inlines, blocks
}

// inlineCandidate reports whether the table at path may be written inline, and
// returns its rendered form when it may.
func inlineCandidate(ctx context.Context, s kongfig.Styler, k string, v any, path string, depth int, opts tomlRenderOpts) (inlineEntry, bool) {
	if opts.forceBlock {
		return inlineEntry{}, false
	}
	maxKeys, marked := opts.inline.maxKeysFor(path)
	if !marked {
		return inlineEntry{}, false
	}
	sub, ok := asConfigData(v)
	if !ok || len(sub) > maxKeys {
		return inlineEntry{}, false
	}

	e := inlineEntry{key: k, path: path, value: v}
	if rv, isRV := v.(kongfig.RenderedValue); isRV && rv.Redacted {
		e.rendered = s.Redacted(rv.RedactedDisplay)
	} else {
		if opts.omitNil {
			sub = stripNils(sub)
		}
		e.rendered = tomlInlineTableCtx(ctx, s, path, sub)
	}

	// Width only gates the terminal; a written file must not depend on the size
	// of the terminal that produced it.
	if opts.inline.checkWidth && opts.cols > 0 {
		width := len(opts.ind(depth)) + render.VisualWidth(s.Key(tomlKey(k))) + 3 + render.VisualWidth(e.rendered)
		if width > opts.cols {
			return inlineEntry{}, false
		}
	}
	return e, true
}

func renderTOMLInline(ctx context.Context, w io.Writer, s kongfig.Styler, e inlineEntry, depth int, opts tomlRenderOpts) {
	pad := opts.ind(depth)
	if help := opts.help(ctx, e.path); help != "" {
		fmt.Fprintf(w, "%s%s\n", pad, s.Comment("# "+help))
	}
	line := pad + s.Key(tomlKey(e.key)) + " = " + e.rendered
	if rv, ok := e.value.(kongfig.RenderedValue); ok {
		line += tomlAnnSuffix(ctx, rv, e.path, s, opts)
	}
	fmt.Fprintln(w, line)
}

// asConfigData unwraps v to the table it holds, if any.
func asConfigData(v any) (kongfig.ConfigData, bool) {
	switch val := v.(type) {
	case kongfig.ConfigData:
		return val, true
	case kongfig.RenderedValue:
		if cd, ok := val.Value.(kongfig.ConfigData); ok {
			return cd, true
		}
	}
	return nil, false
}

func renderTOMLTable(ctx context.Context, w io.Writer, s kongfig.Styler, v any, path string, depth int, opts tomlRenderOpts) error {
	var sub kongfig.ConfigData
	switch val := v.(type) {
	case kongfig.RenderedValue:
		if cd, ok := val.Value.(kongfig.ConfigData); ok {
			sub = cd
		}
	case kongfig.ConfigData:
		sub = val
	}
	var buf bytes.Buffer
	if err := renderMap(ctx, &buf, s, sub, path, path, depth, opts); err != nil {
		return err
	}
	if buf.Len() > 0 {
		_, err := buf.WriteTo(w)
		return err
	}
	return nil
}

// tableArraySlice unwraps a table-array value to its slice and its
// [kongfig.RenderedValue] wrapper, if any.
func tableArraySlice(v any) (slice []any, rv kongfig.RenderedValue, isRV, ok bool) {
	rv, isRV = v.(kongfig.RenderedValue)
	raw := v
	if isRV {
		raw = rv.Value
	}
	slice, ok = raw.([]any)
	return slice, rv, isRV, ok
}

// splitTableArrays partitions arrays of tables into the ones written as a
// key/value line and the ones written as [[section]] blocks. The distinction
// decides emission order, not just formatting: a key/value line placed after a
// section header would re-parse as a member of that section.
func splitTableArrays(data kongfig.ConfigData, tableArrs []string, depth int, opts tomlRenderOpts) (inlineArrs, blocks []string) {
	for _, k := range tableArrs {
		slice, rv, isRV, ok := tableArraySlice(data[k])
		switch {
		case !ok:
			continue // not a slice after all; nothing to emit
		case isRV && rv.Redacted:
			// The placeholder is a single value, so it is always a key/value line.
			inlineArrs = append(inlineArrs, k)
		case tableArrayNeedsBlock(slice, depth, opts):
			blocks = append(blocks, k)
		default:
			inlineArrs = append(inlineArrs, k)
		}
	}
	return inlineArrs, blocks
}

// renderTOMLTableArrayInline writes an array of tables as a key/value line:
// aux = [{path = "/aux"}], or one element per line when that does not fit the
// terminal. Callers must emit it before any section header.
func renderTOMLTableArrayInline(ctx context.Context, w io.Writer, s kongfig.Styler, k string, v any, path string, depth int, opts tomlRenderOpts) {
	slice, rv, isRV, ok := tableArraySlice(v)
	if !ok {
		return
	}

	pad := opts.ind(depth)
	if help := opts.help(ctx, path); help != "" {
		fmt.Fprintf(w, "%s%s\n", pad, s.Comment("# "+help))
	}
	styledKey := s.Key(tomlKey(k))
	if isRV && rv.Redacted {
		fmt.Fprintln(w, pad+styledKey+" = "+s.Redacted(rv.RedactedDisplay)+tomlAnnSuffix(ctx, rv, path, s, opts))
		return
	}

	elems := tableArrayElems(ctx, s, path, slice, opts)
	valueStr := "[" + strings.Join(elems, ", ") + "]"
	keyW := len(pad) + render.VisualWidth(styledKey)

	// An array too long for one line still does not need a section header per
	// entry: it is written one entry per line, the way an overflowing array of
	// scalars is. The key stays a key/value line either way, so it keeps its
	// place ahead of any header.
	if opts.cols > 0 && keyW+3+render.VisualWidth(valueStr) > opts.cols {
		if isRV && !opts.omitComments {
			if ann := render.Annotation(ctx, rv, path, s); ann != "" {
				fmt.Fprintf(w, "%s%s\n", pad, s.Comment("# ")+ann)
			}
		}
		fmt.Fprintf(w, "%s%s = [\n", pad, styledKey)
		for _, e := range elems {
			fmt.Fprintf(w, "%s  %s,\n", pad, e)
		}
		fmt.Fprintf(w, "%s]\n", pad)
		return
	}

	line := pad + styledKey + " = " + valueStr
	if isRV {
		line += tomlAnnSuffix(ctx, rv, path, s, opts)
	}
	fmt.Fprintln(w, line)
}

// tableArrayElems renders every element of a table-array as an inline table,
// honoring the key order configured for path so the one-line, per-line and
// [[section]] forms all order their keys the same way.
func tableArrayElems(ctx context.Context, s kongfig.Styler, path string, slice []any, opts tomlRenderOpts) []string {
	elems := make([]string, 0, len(slice))
	for _, elem := range slice {
		if cd, ok := elem.(kongfig.ConfigData); ok {
			if opts.omitNil {
				cd = stripNils(cd)
			}
			elems = append(elems, tomlInlineTableCtx(ctx, s, path, cd))
			continue
		}
		elems = append(elems, tomlValueStyled(s, elem))
	}
	return elems
}

// renderTOMLTableArrayBlock writes an array of tables as [[path]] sections.
func renderTOMLTableArrayBlock(ctx context.Context, w io.Writer, s kongfig.Styler, v any, path string, depth int, opts tomlRenderOpts) error {
	slice, _, _, ok := tableArraySlice(v)
	if !ok {
		return nil
	}
	for _, elem := range slice {
		cd, ok := elem.(kongfig.ConfigData)
		if !ok {
			continue
		}
		fmt.Fprintf(w, "\n%s%s\n", opts.ind(depth), s.Syntax("[[")+s.Key(tomlHeaderPath(path))+s.Syntax("]]"))
		if err := renderMap(ctx, w, s, cd, path, "", depth+1, opts); err != nil {
			return err
		}
	}
	return nil
}

func tomlAnnSuffix(ctx context.Context, rv kongfig.RenderedValue, path string, s kongfig.Styler, opts tomlRenderOpts) string {
	if opts.omitComments {
		return ""
	}
	ann := render.Annotation(ctx, rv, path, s)
	if ann == "" {
		return ""
	}
	if opts.align {
		return render.AnnMarker + "  " + s.Comment("# ") + ann
	}
	return "  " + s.Comment("# ") + ann
}

// isTableArray reports whether v is a []any whose every element is a ConfigData.
// These correspond to TOML's array-of-tables ([[key]]) syntax.
func isTableArray(v any) bool {
	slice, ok := v.([]any)
	if !ok || len(slice) == 0 {
		return false
	}
	for _, elem := range slice {
		if _, ok := elem.(kongfig.ConfigData); !ok {
			return false
		}
	}
	return true
}

// tableArrayNeedsBlock reports whether a table-array has to use [[...]] block
// form rather than a key/value line. Returns true when forceBlock is set, when
// an element contains a nested ConfigData sub-tree — inline TOML cannot express
// a nested table, so those have no other form — or when a single element is too
// wide for a line of its own. The width of the whole array does not decide it:
// an array that overflows is written one element per line instead.
func tableArrayNeedsBlock(slice []any, depth int, opts tomlRenderOpts) bool {
	if opts.forceBlock {
		return true
	}
	for _, elem := range slice {
		cd, ok := elem.(kongfig.ConfigData)
		if !ok {
			continue
		}
		for _, v := range cd {
			if _, ok := v.(kongfig.ConfigData); ok {
				return true
			}
		}
		// "  {...}," on its own line, indented with the key that owns it.
		if opts.cols > 0 && len(opts.ind(depth))+2+render.VisualWidth(tomlValue(cd))+1 > opts.cols {
			return true
		}
	}
	return false
}

// isTOMLArray reports whether v is a slice type for multiline-overflow detection.
// Uses reflection to handle typed slices (e.g. []SomeStruct) beyond []any/[]string.
func isTOMLArray(v any) bool {
	if v == nil {
		return false
	}
	return reflect.TypeOf(v).Kind() == reflect.Slice
}

// toTOMLSlice converts any slice to []any for uniform element iteration.
func toTOMLSlice(v any) []any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return nil
	}
	out := make([]any, rv.Len())
	for i := range rv.Len() {
		out[i] = rv.Index(i).Interface()
	}
	return out
}

// tomlValue formats a value for TOML output.
func tomlValue(v any) string {
	if v == nil {
		return "nil"
	}
	if out, ok := tomlLeaf(v); ok {
		return out
	}
	switch val := v.(type) {
	case []any:
		return tomlArray(val)
	case []string:
		out := make([]any, len(val))
		for i, s := range val {
			out[i] = s
		}
		return tomlArray(out)
	case kongfig.ConfigData:
		return tomlInlineTable(map[string]any(val))
	case map[string]any:
		return tomlInlineTable(val)
	default:
		return tomlValueReflect(val)
	}
}

func tomlValueReflect(val any) string {
	rv := reflect.ValueOf(val)
	switch rv.Kind() {
	case reflect.Slice:
		return tomlArray(toTOMLSlice(val))
	case reflect.Map, reflect.Struct:
		// Marshal to TOML and back to extract as map[string]any.
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(val); err == nil {
			var m map[string]any
			if _, err = toml.Decode(buf.String(), &m); err == nil {
				return tomlInlineTable(m)
			}
		}
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface,
		reflect.Pointer, reflect.String, reflect.UnsafePointer:
		// non-container kind: fall through to quoted string fallback
	}
	return fmt.Sprintf("%q", strings.TrimSpace(fmt.Sprintf("%v", val)))
}

// tomlLeaf formats the scalar kinds that have a direct TOML spelling.
// ok is false for containers, which the caller handles.
func tomlLeaf(v any) (string, bool) {
	switch val := v.(type) {
	case kongfig.RenderedValue:
		return tomlValue(val.Value), true
	case string:
		return tomlString(val), true
	case bool:
		if val {
			return "true", true
		}
		return "false", true
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%v", val), true
	case float32:
		return tomlFloat(float64(val), 32), true
	case float64:
		return tomlFloat(val, 64), true
	case time.Time:
		return val.Format(time.RFC3339Nano), true
	}
	return "", false
}

// tomlArray formats a slice as a TOML inline array: ["v1", "v2"].
func tomlArray(vals []any) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = tomlValue(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// tomlInlineTable formats a map as a TOML inline table: {k = "v"}.
func tomlInlineTable(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, len(keys))
	for i, k := range keys {
		pairs[i] = tomlKey(k) + " = " + tomlValue(m[k])
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

// tomlValueStyled formats a value for TOML output with Styler-applied coloring.
// Used for elements in multiline arrays where keys and values can be individually styled.
func tomlValueStyled(s kongfig.Styler, v any) string {
	if v == nil {
		return s.Null("nil")
	}
	if out, ok := tomlLeafStyled(s, v); ok {
		return out
	}
	switch val := v.(type) {
	case kongfig.ConfigData:
		return tomlInlineTableStyled(s, map[string]any(val))
	case map[string]any:
		return tomlInlineTableStyled(s, val)
	case []any:
		return tomlArrayStyled(s, val)
	case []string:
		out := make([]any, len(val))
		for i, sv := range val {
			out[i] = sv
		}
		return tomlArrayStyled(s, out)
	default:
		return tomlValueStyledReflect(s, val)
	}
}

func tomlValueStyledReflect(s kongfig.Styler, val any) string {
	rv := reflect.ValueOf(val)
	switch rv.Kind() {
	case reflect.Slice:
		return tomlArrayStyled(s, toTOMLSlice(val))
	case reflect.Map, reflect.Struct:
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(val); err == nil {
			var m map[string]any
			if _, err = toml.Decode(buf.String(), &m); err == nil {
				return tomlInlineTableStyled(s, m)
			}
		}
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface,
		reflect.Pointer, reflect.String, reflect.UnsafePointer:
		// non-container kind: fall through to quoted string fallback
	}
	return s.String(fmt.Sprintf("%q", strings.TrimSpace(fmt.Sprintf("%v", val))))
}

// tomlLeafStyled is [tomlLeaf] with the Styler applied.
// ok is false for containers, which the caller handles.
func tomlLeafStyled(s kongfig.Styler, v any) (string, bool) {
	switch val := v.(type) {
	case kongfig.RenderedValue:
		if cd, ok := val.Value.(kongfig.ConfigData); ok && !val.Redacted {
			return tomlInlineTableStyled(s, map[string]any(cd)), true
		}
		return render.Value(s, val, tomlValue(val.Value)), true
	case string:
		return s.String(tomlString(val)), true
	case bool:
		if val {
			return s.Bool("true"), true
		}
		return s.Bool("false"), true
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return s.Number(fmt.Sprintf("%v", val)), true
	case float32:
		return s.Number(tomlFloat(float64(val), 32)), true
	case float64:
		return s.Number(tomlFloat(val, 64)), true
	case time.Time:
		return s.String(val.Format(time.RFC3339Nano)), true
	}
	return "", false
}

// tomlArrayStyled formats a slice as a TOML inline array with styled values.
func tomlArrayStyled(s kongfig.Styler, vals []any) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = tomlValueStyled(s, v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// stripNils drops the keys holding a nil value, recursing into nested tables
// and table arrays. TOML has no null: block tables skip those keys in
// renderTOMLScalar, and an inline table has to skip them before it is
// formatted, or Marshal writes `k = nil` and the document no longer parses.
func stripNils(sub kongfig.ConfigData) kongfig.ConfigData {
	out := make(kongfig.ConfigData, len(sub))
	for k, v := range sub {
		leaf := v
		rv, isRV := v.(kongfig.RenderedValue)
		if isRV {
			leaf = rv.Value
		}
		if leaf == nil {
			continue
		}
		stripped, changed := stripNilsValue(leaf)
		switch {
		case !changed:
			out[k] = v
		case isRV:
			rv.Value = stripped
			out[k] = rv
		default:
			out[k] = stripped
		}
	}
	return out
}

// stripNilsValue rewrites tables and table arrays without their nil keys,
// reporting whether anything had to be rebuilt.
func stripNilsValue(v any) (any, bool) {
	switch val := v.(type) {
	case kongfig.ConfigData:
		return stripNils(val), true
	case []any:
		out := make([]any, len(val))
		changed := false
		for i, elem := range val {
			out[i] = elem
			if sub, ok := elem.(kongfig.ConfigData); ok {
				out[i] = stripNils(sub)
				changed = true
			}
		}
		if !changed {
			return v, false
		}
		return out, true
	default:
		return v, false
	}
}

// tomlInlineTableCtx formats a table as an inline table, honoring the key order
// configured for path.
func tomlInlineTableCtx(ctx context.Context, s kongfig.Styler, path string, sub kongfig.ConfigData) string {
	keys := render.OrderedKeys(ctx, path, sub)
	pairs := make([]string, len(keys))
	for i, k := range keys {
		pairs[i] = s.Key(tomlKey(k)) + " = " + tomlValueStyled(s, sub[k])
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

// tomlKey renders a table key, quoting it when it is not a valid TOML bare key.
func tomlKey(k string) string {
	if k == "" {
		return `""`
	}
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return tomlString(k)
		}
	}
	return k
}

// tomlHeaderPath renders a dot-path as a TOML table header, quoting the segments
// that are not valid bare keys.
func tomlHeaderPath(path string) string {
	segs := strings.Split(path, ".")
	for i, seg := range segs {
		segs[i] = tomlKey(seg)
	}
	return strings.Join(segs, ".")
}

// tomlString quotes s as a TOML basic string. Go's %q emits \xNN for control
// bytes, which TOML does not accept; widen those to \u00NN.
func tomlString(s string) string {
	q := strconv.Quote(s)
	if !strings.Contains(q, `\x`) {
		return q
	}
	return strings.ReplaceAll(q, `\x`, `\u00`)
}

// tomlFloat formats a float as a TOML float, which — unlike Go — always needs a
// fractional part or an exponent to stay distinct from an integer.
func tomlFloat(f float64, bits int) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	out := strconv.FormatFloat(f, 'g', -1, bits)
	if !strings.ContainsAny(out, ".eE") {
		out += ".0"
	}
	return out
}

// tomlInlineTableStyled formats a map as a TOML inline table with s.Key() applied to keys.
func tomlInlineTableStyled(s kongfig.Styler, m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, len(keys))
	for i, k := range keys {
		pairs[i] = s.Key(tomlKey(k)) + " = " + tomlValueStyled(s, m[k])
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}
