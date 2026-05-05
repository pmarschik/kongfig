package toml

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
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
	return render.AlignAnnotationsCtx(ctx, buf.String(), w)
}

func renderMap(ctx context.Context, w io.Writer, s kongfig.Styler, data kongfig.ConfigData, prefix, tableHeader string, align bool) error { //nolint:gocognit,cyclop // complex recursive renderer, intentional
	keys := render.OrderedKeys(ctx, prefix, data)

	// Scalars first, then tables (TOML convention: scalars must precede [table] sections)
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

	// Print table header only when this level owns scalars; sub-table-only
	// sections are implied by their children's headers in TOML.
	if tableHeader != "" && len(scalars) > 0 {
		fmt.Fprintf(w, "\n%s\n", s.Syntax("[")+s.Key(tableHeader)+s.Syntax("]"))
	}

	tty, _ := render.TTYSizeKey.Read(ctx)
	cols := tty.Cols
	forceBlock := render.BlockCollections(ctx)

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

		if help := render.HelpText(ctx, path); help != "" {
			fmt.Fprintf(w, "%s\n", s.Comment("# "+help))
		}

		inline := tomlValue(leafVal)
		keyW := render.VisualWidth(s.Key(k))

		// For TOML arrays, switch to multiline when forced or inline form would overflow.
		if isTOMLArray(leafVal) && (forceBlock || (cols > 0 && keyW+3+render.VisualWidth(inline) > cols)) {
			if isRV {
				if ann := render.Annotation(ctx, rv, path, s); ann != "" {
					fmt.Fprintf(w, "%s\n", s.Comment("# ")+ann)
				}
			}
			fmt.Fprintf(w, "%s = [\n", s.Key(k))
			for _, elem := range toTOMLSlice(leafVal) {
				fmt.Fprintf(w, "  %s,\n", tomlValueStyled(s, elem))
			}
			fmt.Fprintln(w, "]")
			continue
		}

		line := s.Key(k) + " = " + render.Value(s, v, inline)
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
	case []any:
		return tomlArray(val)
	case []string:
		out := make([]any, len(val))
		for i, s := range val {
			out[i] = s
		}
		return tomlArray(out)
	case map[string]any:
		return tomlInlineTable(val)
	default:
		rv := reflect.ValueOf(val)
		switch rv.Kind() { //nolint:exhaustive // only slice/map/struct need special treatment
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
		}
		return fmt.Sprintf("%q", strings.TrimSpace(fmt.Sprintf("%v", val)))
	}
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
		pairs[i] = k + " = " + tomlValue(m[k])
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

// tomlValueStyled formats a value for TOML output with Styler-applied coloring.
// Used for elements in multiline arrays where keys and values can be individually styled.
func tomlValueStyled(s kongfig.Styler, v any) string { //nolint:cyclop // mirrors tomlValue type dispatch, intentional
	if v == nil {
		return s.Null("nil")
	}
	switch val := v.(type) {
	case string:
		return s.String(fmt.Sprintf("%q", val))
	case bool:
		if val {
			return s.Bool("true")
		}
		return s.Bool("false")
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return s.Number(fmt.Sprintf("%v", val))
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
		rv := reflect.ValueOf(val)
		switch rv.Kind() { //nolint:exhaustive // only slice/map/struct need special treatment
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
		}
		return s.String(fmt.Sprintf("%q", strings.TrimSpace(fmt.Sprintf("%v", val))))
	}
}

// tomlArrayStyled formats a slice as a TOML inline array with styled values.
func tomlArrayStyled(s kongfig.Styler, vals []any) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = tomlValueStyled(s, v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
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
		pairs[i] = s.Key(k) + " = " + tomlValueStyled(s, m[k])
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}
