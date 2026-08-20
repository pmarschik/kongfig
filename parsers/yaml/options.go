package yaml

// Option configures a [Parser] built by [New].
type Option func(*Parser)

// New returns a Parser configured by opts. The returned Parser is usable both as
// a [kongfig.Parser] (Marshal/Unmarshal) and, via Bind, as a renderer; layout
// options therefore apply equally to written and displayed YAML.
func New(opts ...Option) *Parser {
	p := &Parser{}
	for _, o := range opts {
		o(p)
	}
	return p
}

// WithInlineMaps marks mapping paths that may be emitted as YAML flow mappings
// ({k: v}) instead of a block mapping of their own. Patterns are dot-separated
// paths matched segment by segment against the full path of the mapping; a "*"
// segment matches exactly one path segment, so "buckets.*" marks every direct
// child of the buckets mapping.
//
// A marked mapping is only collapsed when it has at most [WithInlineMaxKeys]
// direct keys; over that it stays a block mapping. When rendering to a terminal
// a marked mapping too wide for its line stays a block mapping too — mark the
// path with [WithInlineOverflow] to keep the one-line form regardless of width.
//
// This is the YAML spelling of the same mark parsers/toml reads for inline
// tables, so a kongfig:",inline" struct tag drives both.
//
// Repeated calls append; passing no patterns is a no-op.
func WithInlineMaps(pats ...string) Option {
	return func(p *Parser) { p.inlinePatterns = append(p.inlinePatterns, pats...) }
}

// WithInlineOverflow marks paths whose compact form is kept even when it does
// not fit the terminal, instead of falling back to a block mapping. Patterns are
// matched exactly as [WithInlineMaps] matches them, and an overflow mark implies
// an inline one, so marking a path here is enough on its own.
//
// Use it where the shape carries the meaning: a list of two-key entries reads as
// a list even when the lines run past the edge of the window, while a block
// mapping per entry buries it. Writing a file never consults the width, so this
// only affects rendered output.
//
// Repeated calls append; passing no patterns is a no-op.
func WithInlineOverflow(pats ...string) Option {
	return func(p *Parser) { p.overflowPatterns = append(p.overflowPatterns, pats...) }
}

// WithInlineMaxKeys sets how many direct keys a mapping marked by
// [WithInlineMaps] (or a ,inline struct tag) may hold and still be written as a
// flow mapping. The default is [DefaultInlineMaxKeys]. Values below zero are
// clamped to zero, which disables collapsing for anything but empty mappings.
func WithInlineMaxKeys(n int) Option {
	return func(p *Parser) {
		if n < 0 {
			n = 0
		}
		p.inlineMaxKeys = &n
	}
}
