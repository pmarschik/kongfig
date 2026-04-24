package toml

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	kongfig "github.com/pmarschik/kongfig"
	render "github.com/pmarschik/kongfig/render"
)

// Parser implements [kongfig.Parser] for TOML.
type Parser struct{}

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

// Marshal encodes a map to TOML bytes.
// The returned bytes always end with a trailing newline (added by the TOML encoder).
func (Parser) Marshal(data kongfig.ConfigData) ([]byte, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

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
	p Parser
	s kongfig.Styler
}

func (r *renderer) Render(ctx context.Context, w io.Writer, data kongfig.ConfigData) error {
	if !render.AlignSources(ctx) {
		return renderMap(ctx, w, r.s, data, "", "", false)
	}
	// Two-pass: render with annotation markers, then align.
	var buf bytes.Buffer
	if err := renderMap(ctx, &buf, r.s, data, "", "", true); err != nil {
		return err
	}
	return render.AlignAnnotations(buf.String(), w)
}

func renderMap(ctx context.Context, w io.Writer, s kongfig.Styler, data kongfig.ConfigData, prefix, tableHeader string, align bool) error { //nolint:gocognit,cyclop // complex recursive renderer, intentional
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Scalars first, then tables (TOML convention)
	var scalars, tables []string
	for _, k := range keys {
		if _, ok := data[k].(kongfig.ConfigData); ok {
			tables = append(tables, k)
		} else {
			// Also treat RenderedValue wrapping a sub-map as a table.
			if rv, ok := data[k].(kongfig.RenderedValue); ok {
				if _, isMap := rv.Value.(kongfig.ConfigData); isMap {
					tables = append(tables, k)
					continue
				}
			}
			scalars = append(scalars, k)
		}
	}

	// Print table header if needed (for nested sections)
	if tableHeader != "" && (len(scalars) > 0 || len(tables) > 0) {
		fmt.Fprintf(w, "\n%s\n", s.Syntax("["+tableHeader+"]"))
	}

	helpTexts := render.HelpTexts(ctx)

	for _, k := range scalars {
		v := data[k]
		path := k
		if prefix != "" {
			path = prefix + "." + k
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
				fmt.Fprintf(w, "%s\n", s.Comment("# "+help))
			}
		}

		line := s.Key(k) + " = " + render.Value(s, v, tomlValue(leafVal))
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

	for _, k := range tables {
		v := data[k]
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		var sub kongfig.ConfigData
		if rv, ok := v.(kongfig.RenderedValue); ok {
			sub, _ = rv.Value.(kongfig.ConfigData) //nolint:errcheck // nil map is valid empty sub-map
		} else {
			sub, _ = v.(kongfig.ConfigData) //nolint:errcheck // nil map is valid empty sub-map
		}
		var buf bytes.Buffer
		if err := renderMap(ctx, &buf, s, sub, path, path, align); err != nil {
			return err
		}
		if buf.Len() > 0 {
			if _, err := buf.WriteTo(w); err != nil {
				return err
			}
		}
	}
	return nil
}

// tomlValue formats a scalar value for TOML output.
func tomlValue(v any) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("%q", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return fmt.Sprintf("%v", val)
	default:
		return fmt.Sprintf("%q", strings.TrimSpace(fmt.Sprintf("%v", val)))
	}
}
