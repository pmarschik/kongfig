package json

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	kongfig "github.com/pmarschik/kongfig"
	render "github.com/pmarschik/kongfig/render"
)

// Parser implements [kongfig.Parser] for JSON and JSONC.
//
// Comments bool enables JSONC mode: strips // and /* */ comments before parsing,
// and emits // annotations when rendering.
//
// Indent sets the indentation string used for rendering (default: "  " when empty).
// Set Compact to true to render without any indentation or newlines between keys.
type Parser struct {
	Indent   string
	Comments bool
	Compact  bool
}

// Default is a strict JSON parser with no comment support.
var Default = &Parser{}

// WithComments is a JSONC parser: strips // and /* */ comments before parsing.
var WithComments = &Parser{Comments: true}

// Compact is a strict JSON parser that renders without indentation.
var Compact = &Parser{Compact: true}

var (
	_ kongfig.Parser         = Parser{}
	_ kongfig.ParserNamer    = Parser{}
	_ kongfig.OutputProvider = Parser{}
)

func (p Parser) indent() string {
	if p.Compact {
		return ""
	}
	if p.Indent == "" {
		return "  "
	}
	return p.Indent
}

// Unmarshal decodes JSON (or JSONC) bytes into a map.
func (p Parser) Unmarshal(b []byte) (kongfig.ConfigData, error) {
	if p.Comments {
		b = stripComments(b)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return make(kongfig.ConfigData), nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return kongfig.ToConfigData(out), nil
}

// Marshal encodes a map to JSON bytes.
// Uses compact encoding when Compact is true; otherwise uses indented output.
// The returned bytes always end with a trailing newline.
func (p Parser) Marshal(data kongfig.ConfigData) ([]byte, error) {
	var (
		b   []byte
		err error
	)
	if p.Compact {
		b, err = json.Marshal(data)
	} else {
		b, err = json.MarshalIndent(data, "", p.indent())
	}
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Format returns the parser's format name for source label composition.
// Returns "jsonc" for comment-aware parsers, "json" otherwise.
func (p Parser) Format() string {
	if p.Comments {
		return "jsonc"
	}
	return "json"
}

// Extensions returns the file extensions handled by this parser.
func (p Parser) Extensions() []string {
	if p.Comments {
		return []string{".jsonc", ".json"}
	}
	return []string{".json"}
}

// Bind returns a [kongfig.Renderer] that writes syntax-highlighted JSON.
func (p Parser) Bind(s kongfig.Styler) kongfig.Renderer {
	return &renderer{p: p, s: s}
}

// renderer writes JSON with token-level styling.
type renderer struct {
	s kongfig.Styler
	p Parser
}

func (r *renderer) Render(ctx context.Context, w io.Writer, data kongfig.ConfigData) error {
	if !render.AlignSources(ctx) {
		fmt.Fprintln(w, r.s.Syntax("{"))
		if err := renderMap(ctx, w, r.s, r.p, data, "", 1, false); err != nil {
			return err
		}
		fmt.Fprintln(w, r.s.Syntax("}"))
		return nil
	}
	// Two-pass: render with annotation markers, then align.
	var buf bytes.Buffer
	fmt.Fprintln(&buf, r.s.Syntax("{"))
	if err := renderMap(ctx, &buf, r.s, r.p, data, "", 1, true); err != nil {
		return err
	}
	fmt.Fprintln(&buf, r.s.Syntax("}"))
	return render.AlignAnnotations(buf.String(), w)
}

//nolint:gocognit,cyclop // complex recursive renderer, intentional
func renderMap(ctx context.Context, w io.Writer, s kongfig.Styler, p Parser, data kongfig.ConfigData, prefix string, depth int, align bool) error {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pad := strings.Repeat(p.indent(), depth)
	helpTexts := render.HelpTexts(ctx)

	for i, k := range keys {
		v := data[k]
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		comma := ","
		if i == len(keys)-1 {
			comma = ""
		}

		if sub, ok := v.(kongfig.ConfigData); ok {
			var buf bytes.Buffer
			if err := renderMap(ctx, &buf, s, p, sub, path, depth+1, align); err != nil {
				return err
			}
			if buf.Len() > 0 {
				fmt.Fprintf(w, "%s%s: %s\n", pad, s.Key(`"`+k+`"`), s.Syntax("{"))
				if _, err := buf.WriteTo(w); err != nil {
					return err
				}
				fmt.Fprintf(w, "%s%s\n", pad, s.Syntax("}")+comma)
			}
			continue
		}

		// Unwrap RenderedValue
		rv, isRV := v.(kongfig.RenderedValue)
		var leafVal any
		if isRV {
			leafVal = rv.Value
		} else {
			leafVal = v
		}

		if helpTexts != nil {
			if help, ok := helpTexts[path]; ok {
				fmt.Fprintf(w, "%s%s\n", pad, s.Comment("// "+help))
			}
		}

		valBytes, _ := json.Marshal(leafVal) //nolint:errcheck // Marshal of already-valid data cannot fail
		line := fmt.Sprintf("%s%s: %s%s", pad, s.Key(`"`+k+`"`), render.Value(s, v, string(valBytes)), comma)
		if p.Comments && isRV {
			if ann := render.Annotation(ctx, rv, path, s); ann != "" {
				if align {
					line += render.AnnMarker + "  " + s.Comment("// ") + ann
				} else {
					line += "  " + s.Comment("// ") + ann
				}
			}
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

// stripComments removes // line comments and /* */ block comments from JSON bytes,
// leaving string literals intact.
func stripComments(b []byte) []byte { //nolint:gocognit,cyclop // complex state-machine parser, intentional
	out := make([]byte, 0, len(b))
	i := 0
	for i < len(b) {
		if b[i] == '"' {
			out = append(out, b[i])
			i++
			for i < len(b) {
				c := b[i]
				out = append(out, c)
				i++
				if c == '\\' && i < len(b) {
					out = append(out, b[i])
					i++
				} else if c == '"' {
					break
				}
			}
			continue
		}
		if i+1 < len(b) && b[i] == '/' && b[i+1] == '/' {
			for i < len(b) && b[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(b) && b[i] == '/' && b[i+1] == '*' {
			i += 2
			for i+1 < len(b) {
				if b[i] == '*' && b[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}
		out = append(out, b[i])
		i++
	}
	return out
}
