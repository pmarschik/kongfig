package yaml

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	kongfig "github.com/pmarschik/kongfig"
	render "github.com/pmarschik/kongfig/render"
	goyaml "gopkg.in/yaml.v3"
)

// Parser implements [kongfig.Parser] for YAML.
type Parser struct{}

// Default is a ready-to-use Parser instance.
var Default = &Parser{}

var (
	_ kongfig.Parser         = Parser{}
	_ kongfig.ParserNamer    = Parser{}
	_ kongfig.OutputProvider = Parser{}
)

// Unmarshal decodes YAML bytes into a map.
func (Parser) Unmarshal(b []byte) (kongfig.ConfigData, error) {
	var out map[string]any
	if err := goyaml.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return kongfig.ToConfigData(out), nil
}

// Marshal encodes a map to indented YAML bytes.
// The returned bytes always end with a trailing newline (added by the YAML encoder).
func (Parser) Marshal(data kongfig.ConfigData) ([]byte, error) {
	var buf bytes.Buffer
	enc := goyaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Format returns the parser's format name for source label composition.
func (Parser) Format() string { return "yaml" }

// Extensions returns the file extensions handled by this parser.
func (Parser) Extensions() []string { return []string{".yaml", ".yml"} }

// Bind returns a [kongfig.Renderer] that writes syntax-highlighted YAML.
func (p Parser) Bind(s kongfig.Styler) kongfig.Renderer {
	return &renderer{p: p, s: s}
}

// renderer writes YAML with token-level styling.
type renderer struct {
	p Parser
	s kongfig.Styler
}

func (r *renderer) Render(ctx context.Context, w io.Writer, data kongfig.ConfigData) error {
	if !render.AlignSources(ctx) {
		return renderMap(ctx, w, r.s, data, "", 0, false)
	}
	// Two-pass: render with annotation markers, then align.
	var buf bytes.Buffer
	if err := renderMap(ctx, &buf, r.s, data, "", 0, true); err != nil {
		return err
	}
	return render.AlignAnnotations(buf.String(), w)
}

//nolint:gocognit,cyclop // complex recursive renderer, intentional
func renderMap(ctx context.Context, w io.Writer, s kongfig.Styler, data kongfig.ConfigData, prefix string, indent int, align bool) error {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pad := strings.Repeat("  ", indent)
	helpTexts := render.HelpTexts(ctx)

	for _, k := range keys {
		v := data[k]
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}

		if sub, ok := v.(kongfig.ConfigData); ok {
			var buf bytes.Buffer
			if err := renderMap(ctx, &buf, s, sub, path, indent+1, align); err != nil {
				return err
			}
			if buf.Len() > 0 {
				fmt.Fprintf(w, "%s%s:\n", pad, s.Key(k))
				if _, err := buf.WriteTo(w); err != nil {
					return err
				}
			}
			continue
		}

		// Unwrap RenderedValue
		rv, isRV := v.(kongfig.RenderedValue)

		if helpTexts != nil {
			if help, ok := helpTexts[path]; ok {
				fmt.Fprintf(w, "%s%s\n", pad, s.Comment("# "+help))
			}
		}

		var formatted string
		if isRV && !rv.Redacted {
			formatted = fmt.Sprintf("%v", rv.Value)
		} else if !isRV {
			formatted = fmt.Sprintf("%v", v)
		}

		line := fmt.Sprintf("%s%s: %s", pad, s.Key(k), render.Value(s, v, formatted))
		if isRV {
			if ann := render.Annotation(ctx, rv, path, s); ann != "" {
				if align {
					line += render.AnnMarker + "  " + s.Comment("# ") + ann
				} else {
					line += "  " + s.Comment("# ") + ann
				}
			}
		}
		fmt.Fprintln(w, line)
	}
	return nil
}
