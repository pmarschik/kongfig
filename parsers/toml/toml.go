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
// Use [New] with [WithIndent], [WithInlineTables], [WithInlineMaxKeys] and
// [WithInlineOverflow] to configure layout; the settings apply to both
// [Parser.Marshal] and rendering.
type Parser struct {
	indent           *string
	inlineMaxKeys    *int
	inlineWrap       *bool
	inlinePatterns   []string
	overflowPatterns []string
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
	wrap := true
	if p.inlineWrap != nil {
		wrap = *p.inlineWrap
	}
	return inlinePolicy{
		patterns:         p.inlinePatterns,
		overflowPatterns: p.overflowPatterns,
		defaultMax:       limit,
		checkWidth:       checkWidth,
		wrap:             wrap,
	}
}

// writeOpts are the layout options for producing a file rather than a view of
// one: no comments, no nil values, and terminal width left out of it, so what is
// written does not depend on the terminal that wrote it. Both [Parser.MarshalCtx]
// and [Parser.EditDocument] start here, which is what makes a key an edit adds
// take the shape Marshal would have given it.
func (p Parser) writeOpts() tomlRenderOpts {
	return tomlRenderOpts{
		indent:       p.effectiveIndent(),
		inline:       p.inlinePolicyFor(false),
		omitNil:      true,
		omitComments: true,
	}
}

// Default is a ready-to-use Parser instance.
var Default = &Parser{}

var (
	_ kongfig.Parser         = Parser{}
	_ kongfig.ParserNamer    = Parser{}
	_ kongfig.OutputProvider = Parser{}
	_ kongfig.KeyOrderParser = Parser{}
	_ kongfig.CtxMarshaler   = Parser{}
	_ kongfig.DocumentEditor = Parser{}
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

// MarshalCtx is [Parser.Marshal] with a context — the [kongfig.CtxMarshaler]
// implementation — so the render options in ctx also apply when writing a config
// file: inline-table marks derived from ,inline struct tags
// ([kongfig.InlineTablesKey], injected by [kongfig.NewFor]) and the key order
// under [kongfig.RenderKeyOrderKey], which lets a rewritten file keep the order
// it was parsed in instead of coming back alphabetized, plus the sort hooks that
// order keys by a value inside them ([kongfig.KeySortByKey],
// [kongfig.RenderKeySortKey]). Terminal width is ignored either way. See
// [kongfig.WithRenderKeyOrderCtx] and [kongfig.WithRenderKeySortCtx] for building
// such a context.
func (p Parser) MarshalCtx(ctx context.Context, data kongfig.ConfigData) ([]byte, error) {
	opts := p.writeOpts()
	opts.inline.ctxPaths = kongfig.InlineTablesKey.GetAll(ctx)
	// Width is ignored when writing a file, but an overflow mark implies an inline
	// one, so a field tagged only ,overflow still inlines here.
	opts.inline.overflowCtx = kongfig.InlineOverflowKey.GetAll(ctx)
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
	// annCols is the width the annotation of the entry being written needs on the
	// line it rides, held back from the width the lines below that one may use, so
	// they stay clear of the column the annotations align on.
	annCols int
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
	// overflowCtx are the marks carried in the render context by
	// [kongfig.InlineOverflowKey], i.e. those derived from ,overflow struct tags.
	overflowCtx map[string]bool
	// patterns are the marks configured on the parser via [WithInlineTables].
	patterns []string
	// overflowPatterns are the marks configured via [WithInlineOverflow]. Like the
	// struct tag, an overflow mark implies an inline one.
	overflowPatterns []string
	defaultMax       int
	checkWidth       bool
	// wrap allows a table too wide for its line to be reflowed across lines
	// instead of falling back to a section header. See [WithInlineWrap].
	wrap bool
}

func (ip inlinePolicy) empty() bool {
	return len(ip.patterns) == 0 && len(ip.ctxPaths) == 0 &&
		len(ip.overflowPatterns) == 0 && len(ip.overflowCtx) == 0
}

// overflows reports whether path keeps its compact one-line form even when that
// line does not fit the terminal. Patterns are matched the same way
// [inlinePolicy.maxKeysFor] matches them.
func (ip inlinePolicy) overflows(path string) bool {
	if path == "" {
		return false
	}
	segs := strings.Split(path, ".")
	for _, pat := range ip.overflowPatterns {
		if matchPathPattern(strings.Split(pat, "."), segs) {
			return true
		}
	}
	for pat, on := range ip.overflowCtx {
		if on && matchPathPattern(strings.Split(pat, "."), segs) {
			return true
		}
	}
	return false
}

// maxKeysFor reports whether path is marked for inlining and, if so, how many
// direct keys the table may hold. Patterns are matched segment by segment; a "*"
// segment matches any single segment. When several context marks match, the
// largest explicit limit wins, so the result does not depend on map order.
//
// An overflow mark counts as an inline mark without a limit of its own: asking
// for the compact form past the edge of the window is asking for the compact
// form.
func (ip inlinePolicy) maxKeysFor(path string) (int, bool) {
	if path == "" {
		return 0, false
	}
	segs := strings.Split(path, ".")

	marked := ip.overflows(path)
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
		// TOML has no null. Showing "k = nil" would be output the user cannot
		// paste back into a config file, so nil keys are left out of rendered
		// documents exactly as they are left out of written ones.
		omitNil: true,
	}
	opts.inline.ctxPaths = kongfig.InlineTablesKey.GetAll(ctx)
	opts.inline.overflowCtx = kongfig.InlineOverflowKey.GetAll(ctx)
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
	keys := dropEmptyKeys(ctx, prefix, data, render.OrderedKeys(ctx, prefix, data))

	// Scalars first, then tables, then table-arrays (TOML convention: scalars must precede section headers)
	scalars, tables, tableArrs := classifyTOMLKeys(data, keys)
	// Inlined tables and inlined arrays of tables are key/value lines, so they
	// must join the scalars ahead of any header — anything after a header would
	// belong to that section instead.
	inlines, tables := splitInlineTables(ctx, s, data, tables, prefix, depth, opts)
	inlineArrs, tableArrs := splitTableArrays(data, tableArrs, prefix, depth, opts)

	// Every table level owns a header. Top-level tables are preceded by a blank
	// line; nested ones sit directly under their parent.
	if tableHeader != "" {
		if depth <= 1 {
			fmt.Fprintln(w)
		}
		// A table's own help text belongs above its header. Reading it here also
		// spends it, so the prefix match in [render.HelpText] cannot hand it to
		// whichever key happens to be rendered first inside the table, where it
		// would read as documentation of that key.
		if help := opts.help(ctx, tableHeader); help != "" {
			fmt.Fprintf(w, "%s%s\n", opts.ind(depth-1), s.Comment("# "+help))
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

	// A redacted value carries its placeholder rather than its value, so it is
	// written even though the value it hides is nil.
	if leafVal == nil && opts.omitNil && (!isRV || !rv.Redacted) {
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
		renderTOMLArrayBlock(ctx, w, s, styledKey, leafVal, rv, isRV, path, pad, opts)
		return nil
	}

	// The annotation is measured before the fold, not appended after it: it rides
	// the line that closes the string, so that line has to be packed to leave it room.
	ann := ""
	if isRV {
		ann = tomlAnnSuffix(ctx, rv, path, s, opts)
	}

	if folded, ok := foldTOMLString(leafVal, isRV && rv.Redacted, pad+"  ", keyW+3, annTailWidth(ann), opts); ok {
		writeFoldedLeaf(w, pad+styledKey+" = "+render.Value(s, v, folded), ann, pad, opts)
		return nil
	}

	fmt.Fprintln(w, pad+styledKey+" = "+render.Value(s, v, inline)+ann)
	return nil
}

// writeFoldedLeaf writes an entry whose value folded across lines, with its
// annotation pinned to the closing line so the aligner cannot lift it: every other
// line of the value is inside the string, and a comment written above the closing
// line lands there too. The fold leaves the room, so the pin only has to hold; when
// a value with nowhere to break leaves the closing line no room anyway, the
// annotation goes above the whole entry, outside the string, where a comment is a
// comment.
func writeFoldedLeaf(w io.Writer, entry, ann, pad string, opts tomlRenderOpts) {
	if opts.cols > 0 && render.VisualWidth(lastLine(entry))+annTailWidth(ann) > opts.cols {
		if ann != "" {
			fmt.Fprintln(w, pad+bareAnn(ann))
		}
		fmt.Fprintln(w, entry)
		return
	}
	fmt.Fprintln(w, entry+pinAnn(ann))
}

// annTailWidth is the room a trailing annotation needs on the line it rides: its own
// width, plus the column that separates it from the content. The alignment marker is
// a sentinel the aligner strips, so it costs nothing.
func annTailWidth(ann string) int {
	if ann == "" {
		return 0
	}
	return 1 + render.VisualWidth(strings.ReplaceAll(ann, render.AnnMarker, ""))
}

// pinAnn keeps the annotation on the line it was written on. The aligner lifts an
// annotation it cannot fit to a comment line above the value; above the line that
// closes a folded string, that comment is inside the string.
func pinAnn(ann string) string {
	return strings.ReplaceAll(ann, render.AnnMarker, render.AnnMarkerFixed)
}

// bareAnn is the annotation as a comment of its own: the aligner's markers gone,
// and with them the padding that put the comment beside content.
func bareAnn(ann string) string {
	ann = strings.ReplaceAll(ann, render.AnnMarker, "")
	ann = strings.ReplaceAll(ann, render.AnnMarkerFixed, "")
	return strings.TrimLeft(ann, " ")
}

// holdBack returns opts with the room the annotation ann needs reserved, so a value
// folded across the lines below it stays clear of the column ann aligns on. A reserve
// that would take more than half the line back is not a trade worth making: the value
// is what the reader came for.
func holdBack(opts tomlRenderOpts, ann string) tomlRenderOpts {
	if ann == "" || opts.cols <= 0 || 2*annTailWidth(ann) > opts.cols {
		return opts
	}
	opts.annCols = annTailWidth(ann)
	return opts
}

// lastLine returns the last line of text, the one a trailing annotation rides.
func lastLine(text string) string {
	if i := strings.LastIndex(text, "\n"); i >= 0 {
		return text[i+1:]
	}
	return text
}

// foldTOMLString breaks a string too wide for the terminal across lines as a TOML
// multi-line basic string. Every line but the last ends with a backslash, which
// trims the newline and the indentation of the line below it, so the folded form
// holds exactly the value the single line held. It beats letting the terminal wrap
// the line, which breaks mid-token and at column zero.
//
// It reports false when there is nothing to gain: no terminal to fit (a written
// config file must not depend on the one that produced it), a value that is not a
// string, a redacted placeholder standing in for one, or a value with no space to
// break at — a fold mid-token would be a fold the reader cannot see.
//
// startCol is the column the value starts at and contIndent the indentation the
// lines below it carry, so the same fold serves a key/value line and an element of
// an expanded array. tail is the room whatever follows the closing delimiter needs
// on that last line — an element's comma, a leaf's annotation.
//
// The width every line is packed into is opts.annCols narrower, for a fold that sits
// below a line whose annotation it should stay clear of rather than beside.
func foldTOMLString(leafVal any, redacted bool, contIndent string, startCol, tail int, opts tomlRenderOpts) (string, bool) {
	str, isStr := leafVal.(string)
	if !isStr || redacted || opts.cols <= 0 {
		return "", false
	}
	avail := opts.cols - opts.annCols
	if avail-len(contIndent)-3-tail < 1 {
		// Nothing left to fold into: staying clear of a comment column is worth less
		// than a value the reader can follow.
		avail = opts.cols
	}
	quoted := tomlString(str)
	if startCol+render.VisualWidth(quoted)+tail <= avail {
		return "", false
	}

	// The escaped body carries no bare `"""` and no raw newline, so it is as safe
	// between triple quotes as it was between single ones.
	body := strings.TrimSuffix(strings.TrimPrefix(quoted, `"`), `"`)
	lines := packFoldChunks(foldChunks(body),
		// The first line spends three columns on the opening delimiter and one on
		// the backslash that ends it.
		avail-startCol-4,
		// A line below it spends the same one column on its backslash, with two to
		// spare so a fold does not sit flush against the terminal's edge.
		avail-len(contIndent)-3,
		// The last line closes the string and carries whatever follows it.
		avail-len(contIndent)-3-tail)
	if len(lines) < 2 {
		return "", false
	}
	return `"""` + strings.Join(lines, "\\\n"+contIndent) + `"""`, true
}

// foldChunks splits an escaped string body after each run of spaces, so that the
// chunks joined back together are the body again. The spaces stay on the line they
// end: a line-ending backslash trims only what comes after it.
func foldChunks(body string) []string {
	var chunks []string
	start := 0
	for i := 0; i < len(body); i++ {
		if body[i] != ' ' {
			continue
		}
		for i+1 < len(body) && body[i+1] == ' ' {
			i++
		}
		chunks = append(chunks, body[start:i+1])
		start = i + 1
	}
	if start < len(body) {
		chunks = append(chunks, body[start:])
	}
	return chunks
}

// packFoldChunks fills lines with chunks, greedily, within the width the first
// line has and the width every line below it has. A chunk wider than a line of its
// own still gets one: the alternative is a break inside a token.
//
// lastWidth is the narrower width the closing line has, since the delimiter and any
// annotation ride it. When greedy packing overshoots it, the closing chunk moves to
// a line of its own — an annotation that does not fit is written above its line,
// which for a folded string means inside the string, where the backslash above it
// swallows the comment into the value.
func packFoldChunks(chunks []string, firstWidth, contWidth, lastWidth int) []string {
	var lines []string
	cur, width := "", firstWidth
	for _, chunk := range chunks {
		if cur != "" && render.VisualWidth(cur+chunk) > width {
			lines = append(lines, cur)
			cur, width = chunk, contWidth
			continue
		}
		cur += chunk
	}
	lines = append(lines, cur)

	if last := len(lines) - 1; len(lines) > 1 && render.VisualWidth(lines[last]) > lastWidth {
		if head, tail := splitLastChunk(lines[last]); head != "" {
			lines[last] = head
			lines = append(lines, tail)
		}
	}
	return lines
}

// splitLastChunk cuts a packed line before its final chunk, so the chunk can move
// to a line of its own. It reports an empty head when the line is one chunk already:
// a break inside a token is worse than a line that runs long.
func splitLastChunk(line string) (head, tail string) {
	cut := strings.LastIndex(strings.TrimRight(line, " "), " ")
	if cut < 0 {
		return "", line
	}
	// The spaces stay on the line they end: a line-ending backslash trims only what
	// comes after it, so moving them would drop them from the value.
	for cut+1 < len(line) && line[cut+1] == ' ' {
		cut++
	}
	return line[:cut+1], line[cut+1:]
}

// renderTOMLArrayBlock writes an array one element per line, for arrays too wide
// for the terminal or when collections are forced into block form. The key stays
// a key/value line, so it keeps its place ahead of any section header.
func renderTOMLArrayBlock(ctx context.Context, w io.Writer, s kongfig.Styler, styledKey string, leafVal any, rv kongfig.RenderedValue, isRV bool, path, pad string, opts tomlRenderOpts) {
	// The annotation belongs to the key, so it rides on the opening bracket. A
	// line of its own above the key would read as a comment on the whole block.
	ann := ""
	if isRV {
		ann = tomlAnnSuffix(ctx, rv, path, s, opts)
	}
	fmt.Fprintf(w, "%s%s = [%s\n", pad, styledKey, ann)
	// The annotation rides the bracket, but the elements below it are lines of the
	// same entry: held clear of the column it aligns on, the block reads as one
	// thing with a comment beside it.
	for _, line := range arrayElemLines(s, leafVal, pad+"  ", holdBack(opts, ann)) {
		fmt.Fprintln(w, line)
	}
	fmt.Fprintf(w, "%s]\n", pad)
}

// arrayBlockText is renderTOMLArrayBlock as a value: the brackets and the elements
// between them, for an array expanded inside a reflowed inline table.
func arrayBlockText(s kongfig.Styler, leafVal any, pad string, opts tomlRenderOpts) string {
	lines := arrayElemLines(s, leafVal, pad+"  ", opts)
	if len(lines) == 0 {
		return "[]"
	}
	return "[\n" + strings.Join(lines, "\n") + "\n" + pad + "]"
}

// arrayElemLines writes an array's elements a line per element, each indented by
// elemPad and followed by its comma. An element too wide for its line folds, the
// way a string value of that width would.
func arrayElemLines(s kongfig.Styler, leafVal any, elemPad string, opts tomlRenderOpts) []string {
	var lines []string
	for _, elem := range toTOMLSlice(leafVal) {
		// The comma after the element rides its last line, so the fold holds a
		// column back there.
		if folded, ok := foldTOMLString(elem, false, elemPad+"  ", len(elemPad), 1, opts); ok {
			lines = append(lines, elemPad+render.Value(s, elem, folded)+",")
			continue
		}
		lines = append(lines, elemPad+tomlValueStyled(s, elem)+",")
	}
	return lines
}

// inlineEntry is a table that passed the inline policy, together with the
// already-rendered value so the width check and the emission agree.
type inlineEntry struct {
	key   string
	path  string
	value any
	// sub is the table as written: the value's table minus the keys left out of
	// it. Annotations are collected from it, so a dropped key does not annotate
	// a line it no longer appears on. Nil for a redacted entry.
	sub kongfig.ConfigData
	// rendered is the styled "{k = v, ...}" form, or the redacted placeholder.
	rendered string
	// wrapped is the same table reflowed one pair per line, set only when the
	// one line does not fit the terminal. It is the value text, newlines and
	// all, to be written after the entry's "key = ".
	wrapped string
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
	if !ok {
		return inlineEntry{}, false
	}
	// Filter before counting: a key that is not written does not fill the table.
	sub = dropOmitEmpty(ctx, path, sub)
	if len(sub) > maxKeys {
		return inlineEntry{}, false
	}

	e := inlineEntry{key: k, path: path, value: v, sub: sub}
	if rv, isRV := v.(kongfig.RenderedValue); isRV && rv.Redacted {
		e.rendered = s.Redacted(rv.RedactedDisplay)
		e.sub = nil
	} else {
		if opts.omitNil {
			sub = stripNils(sub)
			e.sub = sub
		}
		e.rendered = tomlInlineTableCtx(ctx, s, path, sub)
	}

	// Width only gates the terminal; a written file must not depend on the size
	// of the terminal that produced it. An overflow mark waives the gate: the
	// caller has said the compact shape is worth a line that runs past the edge.
	if opts.inline.checkWidth && opts.cols > 0 && !opts.inline.overflows(path) {
		pad := opts.ind(depth)
		headW := len(pad) + render.VisualWidth(s.Key(tomlKey(k))) + 3
		if headW+render.VisualWidth(e.rendered) > opts.cols {
			// Wrapping off leaves nothing but the section header: a 1.0 reader
			// needs the whole table on one line, and it does not fit one.
			if !opts.inline.wrap || e.sub == nil {
				return inlineEntry{}, false
			}
			e.wrapped = reflowInlineTable(ctx, s, path, sub, pad, opts)
		}
	}
	return e, true
}

// reflowInlineTable lays a table too wide for its line out one pair per line: the
// brace opens the key's line, the pairs sit a level in, and the closing brace
// lines up under the key. The result is the value text, newlines and all, for a
// caller that has already written the "key = " ahead of it.
//
// The shape is kept rather than traded for a section header. The inline mark says
// this table is an entry and not a section, and that reading holds however few
// pairs fit a line.
//
// TOML permitted a newline inside an inline table only from 1.1 — 1.0 required
// one line — so this is written by the render path alone, which is describing a
// value to a reader. [Parser.Marshal] ignores width and never reaches here, and
// [WithInlineWrap] turns it off for output a 1.0 parser has to read back.
func reflowInlineTable(ctx context.Context, s kongfig.Styler, path string, sub kongfig.ConfigData, pad string, opts tomlRenderOpts) string {
	keys := render.OrderedKeys(ctx, path, sub)
	if len(keys) == 0 {
		return "{}"
	}
	inner := pad + opts.pairIndent()
	var b strings.Builder
	b.WriteString("{\n")
	for i, k := range keys {
		styledKey := s.Key(tomlKey(k))
		// The value starts after the indentation, the key and " = ", and its last
		// line carries the comma that separates it from the pair below.
		startCol := len(inner) + render.VisualWidth(styledKey) + 3
		b.WriteString(inner + styledKey + " = ")
		b.WriteString(inlineValueText(ctx, s, buildTOMLPath(path, k), sub[k], inner, startCol, opts))
		if i < len(keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(pad + "}")
	return b.String()
}

// inlineValueText renders a value inside a reflowed inline table. A value that
// fits its line stays on it; one that does not is expanded in place, the way it
// would be on a key/value line of its own — an array a line per element, a table
// reflowed in turn, a long string folded.
func inlineValueText(ctx context.Context, s kongfig.Styler, path string, v any, indent string, startCol int, opts tomlRenderOpts) string {
	nested, isTable := asConfigData(v)
	one := ""
	if isTable {
		one = tomlInlineTableCtx(ctx, s, path, nested)
	} else {
		one = tomlValueStyled(s, v)
	}
	// A trailing comma may still follow, so a value that only just fits does not.
	if opts.cols <= 0 || startCol+render.VisualWidth(one)+1 <= opts.cols {
		return one
	}

	leafVal := v
	if rv, isRV := v.(kongfig.RenderedValue); isRV {
		leafVal = rv.Value
	}
	switch {
	case isTable:
		return reflowInlineTable(ctx, s, path, nested, indent, opts)
	case isTOMLArray(leafVal):
		return arrayBlockText(s, leafVal, indent, opts)
	}
	// A trailing comma may still follow the value, so its last line holds a column back.
	if folded, ok := foldTOMLString(leafVal, isRedacted(v), indent+"  ", startCol, 1, opts); ok {
		return render.Value(s, v, folded)
	}
	return one
}

// pairIndent is the indentation a reflowed inline table puts its pairs at, one
// level in from the key that owns them. Indentation may be switched off, and a
// pair flush against its brace would read as a key of the table above it.
func (o tomlRenderOpts) pairIndent() string {
	if o.indent == "" {
		return "  "
	}
	return o.indent
}

// isRedacted reports whether v stands in for a value it hides.
func isRedacted(v any) bool {
	rv, ok := v.(kongfig.RenderedValue)
	return ok && rv.Redacted
}

func renderTOMLInline(ctx context.Context, w io.Writer, s kongfig.Styler, e inlineEntry, depth int, opts tomlRenderOpts) {
	pad := opts.ind(depth)
	if help := opts.help(ctx, e.path); help != "" {
		fmt.Fprintf(w, "%s%s\n", pad, s.Comment("# "+help))
	}
	head := pad + s.Key(tomlKey(e.key)) + " = "
	lines := inlineEntryLines(head, e)

	// Several groups joined onto one line is what wraps in a terminal; the
	// annotation then reads better as its own comment lines above the entry.
	groups := inlineAnnotationGroups(ctx, s, e, opts)
	joined := strings.Join(groups, ", ")
	if joined != "" && len(groups) > 1 && opts.cols > 0 &&
		render.VisualWidth(lines[0])+4+render.VisualWidth(joined) > opts.cols {
		for _, g := range groups {
			fmt.Fprintf(w, "%s%s\n", pad, s.Comment("# ")+g)
		}
		for _, l := range lines {
			fmt.Fprintln(w, l)
		}
		return
	}
	// The annotation belongs to the key, so it rides the entry's first line — the
	// opening brace of a reflowed table, as it rides the opening bracket of an
	// expanded array. Put on the closing brace instead, the aligner could move it
	// above that line, landing it between the pairs it annotates.
	lines[0] += annSuffix(joined, s, opts)
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}

// inlineEntryLines lays the entry out after its "key = ", as one line or as the
// lines of the reflowed table, which carry their own indentation.
func inlineEntryLines(head string, e inlineEntry) []string {
	if e.wrapped == "" {
		return []string{head + e.rendered}
	}
	return strings.Split(head+e.wrapped, "\n")
}

// inlineAnnotationGroups annotates an inlined table. The table's own source wins
// when it has one; otherwise the leaves that were collapsed into the line supply
// it, so inlining never costs the reader the provenance a block table would show.
func inlineAnnotationGroups(ctx context.Context, s kongfig.Styler, e inlineEntry, opts tomlRenderOpts) []string {
	if opts.omitComments {
		return nil
	}
	if rv, ok := e.value.(kongfig.RenderedValue); ok {
		if ann := render.Annotation(ctx, rv, e.path, s); ann != "" {
			return []string{ann}
		}
	}
	sub := e.sub
	if sub == nil {
		var ok bool
		if sub, ok = asConfigData(e.value); !ok {
			return nil
		}
	}
	return annotationGroups(collectLeafAnnotations(ctx, s, e.path, "", sub, nil))
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
func splitTableArrays(data kongfig.ConfigData, tableArrs []string, prefix string, depth int, opts tomlRenderOpts) (inlineArrs, blocks []string) {
	for _, k := range tableArrs {
		slice, rv, isRV, ok := tableArraySlice(data[k])
		switch {
		case !ok:
			continue // not a slice after all; nothing to emit
		case isRV && rv.Redacted:
			// The placeholder is a single value, so it is always a key/value line.
			inlineArrs = append(inlineArrs, k)
		case tableArrayInlines(slice, buildTOMLPath(prefix, k), depth, opts):
			inlineArrs = append(inlineArrs, k)
		default:
			blocks = append(blocks, k)
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
	groups := tableArrayAnnotation(ctx, s, path, slice, rv, isRV, opts)
	ann := strings.Join(groups, ", ")
	if opts.cols > 0 && keyW+3+render.VisualWidth(valueStr) > opts.cols {
		for _, g := range groups {
			fmt.Fprintf(w, "%s%s\n", pad, s.Comment("# ")+g)
		}
		fmt.Fprintf(w, "%s%s = [\n", pad, styledKey)
		for i, e := range elems {
			for _, line := range tableArrayElemLines(ctx, s, path, slice[i], e, pad+"  ", opts) {
				fmt.Fprintf(w, "%s  %s\n", pad, line)
			}
		}
		fmt.Fprintf(w, "%s]\n", pad)
		return
	}

	fmt.Fprintln(w, pad+styledKey+" = "+valueStr+annSuffix(ann, s, opts))
}

// tableArrayElemLines returns the lines one element of a one-per-line array
// occupies, comma included: the single-line form when it fits the terminal, and
// the pairs broken across lines when it does not.
//
// Breaking beats letting the terminal wrap it, because the terminal wraps
// mid-token and at column zero, where the continuation reads as a new entry
// rather than as more of this one.
func tableArrayElemLines(ctx context.Context, s kongfig.Styler, path string, elem any, rendered, indent string, opts tomlRenderOpts) []string {
	cd, isTable := elem.(kongfig.ConfigData)
	// One column of the width belongs to the comma the element ends with.
	width := len(indent) + render.VisualWidth(rendered) + 1
	if opts.cols <= 0 || width <= opts.cols || !isTable {
		return []string{rendered + ","}
	}
	if opts.omitNil {
		cd = stripNils(cd)
	}
	cd = dropOmitEmpty(ctx, path, cd)
	// One column of the budget belongs to the comma the last line ends with.
	lines := wrapInlineTable(tomlInlinePairs(ctx, s, path, cd), indent, opts.cols-1)
	lines[len(lines)-1] += ","
	return lines
}

// tableArrayAnnotation is inlineAnnotationGroups for an array of tables: the
// array's own source when it has one, otherwise the sources of the leaves the
// key/value line collapsed. It returns bare groups, since the overflow form
// writes them on their own lines rather than as a suffix.
func tableArrayAnnotation(ctx context.Context, s kongfig.Styler, path string, slice []any, rv kongfig.RenderedValue, isRV bool, opts tomlRenderOpts) []string {
	if opts.omitComments {
		return nil
	}
	if isRV {
		if ann := render.Annotation(ctx, rv, path, s); ann != "" {
			return []string{ann}
		}
	}
	var parts []leafAnnotation
	for _, elem := range slice {
		if sub, isTable := asConfigData(elem); isTable {
			parts = collectLeafAnnotations(ctx, s, path, "", sub, parts)
		}
	}
	return annotationGroups(parts)
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
			cd = dropOmitEmpty(ctx, path, cd)
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
	if !ok || len(slice) == 0 {
		return nil
	}
	// The help text describes the list, not each entry, so it is emitted once
	// above the first block — and spent there, for the reason in [renderMap].
	// It has no trailing newline: the blank line each block starts with ends the
	// comment line, which also keeps the comment attached to the block below it.
	if help := opts.help(ctx, path); help != "" {
		fmt.Fprintf(w, "\n%s%s", opts.ind(depth), s.Comment("# "+help))
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
	return annSuffix(render.Annotation(ctx, rv, path, s), s, opts)
}

// annSuffix decorates an already-rendered annotation as a trailing comment.
func annSuffix(ann string, s kongfig.Styler, opts tomlRenderOpts) string {
	if ann == "" {
		return ""
	}
	if opts.align {
		return render.AnnMarker + "  " + s.Comment("# ") + ann
	}
	return "  " + s.Comment("# ") + ann
}

// leafAnnotation is one collapsed leaf: its key relative to the inlined table,
// and the annotation that leaf would have carried on a line of its own.
type leafAnnotation struct{ key, ann string }

// collectLeafAnnotations walks an inlined subtree in render order, gathering the
// annotations of the leaves it collapsed. Identical key/annotation pairs — which
// arrays of tables produce for every element — are recorded once.
func collectLeafAnnotations(ctx context.Context, s kongfig.Styler, path, rel string, sub kongfig.ConfigData, out []leafAnnotation) []leafAnnotation {
	for _, k := range render.OrderedKeys(ctx, path, sub) {
		childPath := buildTOMLPath(path, k)
		childRel := k
		if rel != "" {
			childRel = rel + "." + k
		}
		v := sub[k]
		if nested, isTable := asConfigData(v); isTable {
			out = collectLeafAnnotations(ctx, s, childPath, childRel, nested, out)
			continue
		}
		if slice, isSlice := v.([]any); isSlice {
			for _, elem := range slice {
				if nested, isTable := asConfigData(elem); isTable {
					out = collectLeafAnnotations(ctx, s, childPath, childRel, nested, out)
				}
			}
			continue
		}
		rv, isRV := v.(kongfig.RenderedValue)
		if !isRV {
			continue
		}
		ann := render.Annotation(ctx, rv, childPath, s)
		if ann == "" || containsLeafAnnotation(out, childRel, ann) {
			continue
		}
		out = append(out, leafAnnotation{key: childRel, ann: ann})
	}
	return out
}

func containsLeafAnnotation(parts []leafAnnotation, key, ann string) bool {
	for _, p := range parts {
		if p.key == key && p.ann == ann {
			return true
		}
	}
	return false
}

// annotationGroups renders the collapsed leaves as annotation groups: a single
// bare label when they all agree — naming one source for a mixed table would be
// wrong — otherwise one "keys: label" group per distinct label, in the order the
// labels first appear. Grouping keeps a repeated source from being spelled out
// once per key, which is what made these lines long enough to wrap.
func annotationGroups(parts []leafAnnotation) []string {
	if len(parts) == 0 {
		return nil
	}
	same := true
	for _, p := range parts[1:] {
		if p.ann != parts[0].ann {
			same = false
			break
		}
	}
	if same {
		return []string{parts[0].ann}
	}
	order := make([]string, 0, len(parts))
	keys := make(map[string][]string, len(parts))
	for _, p := range parts {
		if _, seen := keys[p.ann]; !seen {
			order = append(order, p.ann)
		}
		keys[p.ann] = append(keys[p.ann], p.key)
	}
	groups := make([]string, len(order))
	for i, ann := range order {
		groups[i] = strings.Join(keys[ann], ", ") + ": " + ann
	}
	return groups
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

// tableArrayInlines reports whether a table-array may be written as a key/value
// line rather than [[...]] sections. Every element must be a small object by the
// same key limit that governs inline tables — the elements are what becomes an
// inline table, so the limit applies to each of them, and [WithInlineTables]
// marks move that limit per path. Every element must also have an inline form at
// all: TOML cannot nest a table inside an inline table.
//
// The width of the whole array does not decide it: an array that overflows is
// written one element per line instead. Neither does the width of one element,
// on its own — an element wider than the terminal wraps, and a wrap is usually
// cheaper than the block form, which spends a header plus a line per key. What
// decides it is which of the two costs more lines, so a terminal narrow enough
// that wrapping buys nothing still gets its sections — unless the path is marked
// by [WithInlineOverflow], which waives the comparison.
func tableArrayInlines(slice []any, path string, depth int, opts tomlRenderOpts) bool {
	if opts.forceBlock {
		return false
	}
	// Unlike a table, an array of tables is inlinable without a mark: the block
	// form is the verbose one, so the default runs the other way. A mark only
	// moves the key limit for this path.
	maxKeys := opts.inline.defaultMax
	if n, marked := opts.inline.maxKeysFor(path); marked {
		maxKeys = n
	}
	// An overflow mark takes the cost comparison out of it: the caller has said
	// the per-element shape is worth keeping however narrow the terminal is.
	overflow := opts.inline.overflows(path)
	for _, elem := range slice {
		cd, ok := elem.(kongfig.ConfigData)
		if !ok || len(cd) > maxKeys {
			return false
		}
		for _, v := range cd {
			if _, ok := v.(kongfig.ConfigData); ok {
				return false
			}
		}
		// "  {...}," on its own line, indented with the key that owns it. Width
		// only gates the terminal: a written file must not depend on the size of
		// the terminal that produced it.
		if opts.inline.checkWidth && opts.cols > 0 && !overflow {
			line := len(opts.ind(depth)) + 2 + render.VisualWidth(tomlValue(cd)) + 1
			// With wrapping off there is no reflowed form to weigh the block form
			// against: an element that does not fit its line has to become a
			// [[block]], because a reflowed inline table is TOML 1.1 and the whole
			// point of the option is output a 1.0 parser can read. See
			// [WithInlineWrap].
			if !opts.inline.wrap && line > opts.cols {
				return false
			}
			// Ties go to the block form, which is the more explicit of two shapes
			// that cost the same.
			if lines := (line + opts.cols - 1) / opts.cols; lines >= len(cd)+1 {
				return false
			}
		}
	}
	return true
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
		// A redacted placeholder stands in for the value, so it survives even
		// though what it hides is nil.
		if leaf == nil && (!isRV || !rv.Redacted) {
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

// dropEmptyKeys removes the keys marked omitempty that hold nothing, so a
// table's shape follows what is actually configured.
func dropEmptyKeys(ctx context.Context, prefix string, data kongfig.ConfigData, keys []string) []string {
	if len(kongfig.OmitEmptyKey.GetAll(ctx)) == 0 {
		return keys
	}
	out := keys[:0:0]
	for _, k := range keys {
		if render.OmitEmpty(ctx, buildTOMLPath(prefix, k), data[k]) {
			continue
		}
		out = append(out, k)
	}
	return out
}

// dropOmitEmpty rewrites a subtree without its marked-empty keys, for the inline
// forms that format a table before it reaches renderMap. Nested tables are
// rewritten too, so a mark on a key several levels inside an inlined table still
// applies — collapsed onto one line, the reader cannot delete it by hand.
func dropOmitEmpty(ctx context.Context, path string, sub kongfig.ConfigData) kongfig.ConfigData {
	if len(kongfig.OmitEmptyKey.GetAll(ctx)) == 0 {
		return sub
	}
	out := make(kongfig.ConfigData, len(sub))
	for k, v := range sub {
		childPath := buildTOMLPath(path, k)
		if render.OmitEmpty(ctx, childPath, v) {
			continue
		}
		switch val := v.(type) {
		case kongfig.ConfigData:
			out[k] = dropOmitEmpty(ctx, childPath, val)
		case kongfig.RenderedValue:
			if nested, isTable := val.Value.(kongfig.ConfigData); isTable {
				val.Value = dropOmitEmpty(ctx, childPath, nested)
			}
			out[k] = val
		default:
			out[k] = v
		}
	}
	return out
}

// tomlInlineTableCtx formats a table as an inline table, honoring the key order
// configured for path.
func tomlInlineTableCtx(ctx context.Context, s kongfig.Styler, path string, sub kongfig.ConfigData) string {
	return "{" + strings.Join(tomlInlinePairs(ctx, s, path, sub), ", ") + "}"
}

// tomlInlinePairs returns an inline table's key/value pairs in the key order
// configured for path, unjoined, for a caller that has to decide where the line
// breaks go.
func tomlInlinePairs(ctx context.Context, s kongfig.Styler, path string, sub kongfig.ConfigData) []string {
	keys := render.OrderedKeys(ctx, path, sub)
	pairs := make([]string, len(keys))
	for i, k := range keys {
		pairs[i] = s.Key(tomlKey(k)) + " = " + tomlValueStyled(s, sub[k])
	}
	return pairs
}

// wrapInlineTable breaks an inline table's pairs across lines that fit cols,
// returning the lines without their indentation. The first line opens with "{"
// and the last closes with "}"; every break lands after a pair's comma, and
// continuation lines are offset by one column so they align under the first pair
// rather than under the brace.
//
// TOML permitted this only from 1.1 — 1.0 required an inline table to be one
// line — so the wrap is written by the render path alone, which is describing a
// value to a reader. [Parser.Marshal] ignores width and never reaches here, and
// what it writes stays readable to a 1.0 parser.
func wrapInlineTable(pairs []string, indent string, cols int) []string {
	if len(pairs) == 0 {
		return []string{"{}"}
	}
	var lines []string
	cur, curW := "{", len(indent)+1
	for i, pair := range pairs {
		// The comma travels with the pair it follows, so a break never leaves a
		// line starting with one.
		piece := pair + ","
		if i == len(pairs)-1 {
			piece = pair + "}"
		}
		pieceW := render.VisualWidth(piece)
		sep := ""
		if i > 0 {
			sep = " "
		}
		if i > 0 && curW+len(sep)+pieceW > cols {
			lines = append(lines, cur)
			// One column past the indent, under the line above's first pair.
			cur, curW = " "+piece, len(indent)+1+pieceW
			continue
		}
		cur += sep + piece
		curW += len(sep) + pieceW
	}
	return append(lines, cur)
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
