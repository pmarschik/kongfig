package validation

import kongfig "github.com/pmarschik/kongfig"

// Position returns where this path was defined in its source document, or nil
// when the position is unavailable — no provenance, a source with no document
// (env, flags, defaults), or a parser that reports no positions.
//
// Use it to point at the offending line when formatting a violation:
//
//	msg := ps.Path + ": " + v.Message
//	if pos := ps.Position(); pos != nil {
//	    msg += " (" + pos.String() + ")"
//	}
func (ps PathSource) Position() *kongfig.SourcePosition {
	if ps.Source == nil || ps.Path == "" {
		return nil
	}
	return ps.Source.Layer.PositionOf(ps.Path)
}
