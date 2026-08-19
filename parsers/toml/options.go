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
// keys, and — when rendering to a terminal — when the resulting line fits the
// terminal width. Otherwise it falls back to a regular section.
//
// Repeated calls append; passing no patterns is a no-op.
func WithInlineTables(pats ...string) Option {
	return func(p *Parser) { p.inlinePatterns = append(p.inlinePatterns, pats...) }
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
