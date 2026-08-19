package kongfig

import "strconv"

// SourcePosition locates a config path inside its source document.
// Line and Col are 1-based; a zero value means "unknown" and is omitted by
// [SourcePosition.String].
type SourcePosition struct {
	File string // absolute or display path of the document
	Line int    // 1-based line number, 0 when unknown
	Col  int    // 1-based column number, 0 when unknown
}

// String renders the position in the conventional "file:line:col" form,
// dropping trailing components that are unknown ("file:line", "file", "").
func (p SourcePosition) String() string {
	if p.File == "" {
		return ""
	}
	if p.Line <= 0 {
		return p.File
	}
	s := p.File + ":" + strconv.Itoa(p.Line)
	if p.Col > 0 {
		s += ":" + strconv.Itoa(p.Col)
	}
	return s
}

// PositionSupport is an optional [ProviderData] extension.
// Implement it on provider data to report where a config path was defined in
// the underlying document. Return nil for paths the provider cannot locate.
//
// Only sources backed by a text document can support this; env, flag and
// defaults layers simply do not implement it.
type PositionSupport interface {
	PositionOf(path string) *SourcePosition
}

// PositionOf returns the position of path in this layer's source document,
// or nil when the layer has no document, no position support, or no recorded
// position for that path.
func (m LayerMeta) PositionOf(path string) *SourcePosition {
	ps, ok := m.Data.(PositionSupport)
	if !ok {
		return nil
	}
	return ps.PositionOf(path)
}
