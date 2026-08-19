package toml

// Option configures a [Parser] built by [New].
type Option func(*Parser)

// New returns a Parser configured by opts. The returned Parser is usable both as
// a [kongfig.Parser] (Marshal/Unmarshal) and, via Bind, as a renderer; layout
// options therefore apply equally to written and displayed TOML.
func New(opts ...Option) *Parser {
	p := &Parser{}
	for _, o := range opts {
		o(p)
	}
	return p
}

// WithIndent sets the per-level indentation applied to nested table headers and
// the keys they own. The default is two spaces; pass "" to disable indentation
// and keep every line flush left.
func WithIndent(s string) Option {
	return func(p *Parser) { p.indent = &s }
}

// WithInlineTables marks table paths that may be emitted as TOML inline tables
// ({k = v}) instead of their own [section]. Patterns are dot-separated paths
// matched segment by segment against the full path of the table; a "*" segment
// matches exactly one path segment, so "buckets.*" marks every direct child of
// the buckets table.
//
// A marked table is only inlined when it has at most [WithInlineMaxKeys] direct
// keys; over that it falls back to a regular section. When rendering to a
// terminal, a marked table too wide for its line is reflowed across lines, one
// pair per line — mark the path with [WithInlineOverflow] to keep the one-line
// form regardless of width, or see [WithInlineWrap] to demote instead.
//
// Repeated calls append; passing no patterns is a no-op.
func WithInlineTables(pats ...string) Option {
	return func(p *Parser) { p.inlinePatterns = append(p.inlinePatterns, pats...) }
}

// WithInlineOverflow marks paths whose compact form is kept even when it does
// not fit the terminal, instead of falling back to TOML's roomier shape — a
// [section] for a table, [[section]] blocks for an array of tables. Patterns are
// matched exactly as [WithInlineTables] matches them, and an overflow mark
// implies an inline one, so marking a path here is enough on its own.
//
// Use it where the shape carries the meaning: a list of two-key rules reads as a
// list even when the lines run past the edge of the window, while a section per
// rule buries it. Writing a config file never consults the width, so this only
// affects rendered output.
//
// Repeated calls append; passing no patterns is a no-op.
func WithInlineOverflow(pats ...string) Option {
	return func(p *Parser) { p.overflowPatterns = append(p.overflowPatterns, pats...) }
}

// WithInlineWrap sets whether a rendered inline table too wide for the terminal
// may be reflowed across lines, one pair per line. The default is true.
//
// A newline inside an inline table is TOML 1.1; TOML 1.0 requires the whole table
// on one line. Only rendered output is ever reflowed — [Parser.Marshal] does not
// consult the width and writes 1.0 throughout — so pass false where the rendered
// output is meant to be read back by a strict 1.0 parser. A table that no longer
// fits then falls back to its own [section], and one that fits is inlined as
// before; an array of tables with an element that no longer fits falls back to
// [[block]] form the same way.
func WithInlineWrap(b bool) Option {
	return func(p *Parser) { p.inlineWrap = &b }
}

// WithInlineMaxKeys sets how many direct keys a table marked by
// [WithInlineTables] (or a ,inline struct tag) may hold and still be written
// inline. The default is [DefaultInlineMaxKeys]. Values below zero are clamped
// to zero, which disables inlining for anything but empty tables.
func WithInlineMaxKeys(n int) Option {
	return func(p *Parser) {
		if n < 0 {
			n = 0
		}
		p.inlineMaxKeys = &n
	}
}
