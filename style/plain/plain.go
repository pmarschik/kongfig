// Package plain provides a no-op [kongfig.Styler] that returns all tokens unchanged.
// It is always available as part of the core module and has no external dependencies.
package plain

import kongfig "github.com/pmarschik/kongfig"

// Plain is a [kongfig.Styler] that applies no styling.
type Plain struct{}

// New returns a Plain Styler.
func New() *Plain { return &Plain{} }

func (Plain) Key(s string) string     { return s }
func (Plain) String(s string) string  { return s }
func (Plain) Number(s string) string  { return s }
func (Plain) Bool(s string) string    { return s }
func (Plain) Null(s string) string    { return s }
func (Plain) Syntax(s string) string  { return s }
func (Plain) Comment(s string) string { return s }

func (Plain) Annotation(_, s string) string { return s }
func (Plain) SourceKind(s string) string    { return s }
func (Plain) SourceData(s string) string    { return s }
func (Plain) SourceKey(s string) string     { return s }
func (Plain) Redacted(s string) string      { return s }
func (Plain) Codec(s string) string         { return s }

// Ensure Plain implements kongfig.Styler at compile time.
var _ kongfig.Styler = Plain{}
