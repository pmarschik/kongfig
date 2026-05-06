package json

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
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
	_ kongfig.KeyOrderParser = Parser{}
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
		if _, err := fmt.Fprintln(w, r.s.Syntax("{")); err != nil {
			return err
		}
		if err := renderMap(ctx, w, r.s, r.p, data, "", 1, false); err != nil {
			return err
		}
		_, err := fmt.Fprintln(w, r.s.Syntax("}"))
		return err
	}
	// Two-pass: render with annotation markers, then align.
	var buf bytes.Buffer
	if _, err := fmt.Fprintln(&buf, r.s.Syntax("{")); err != nil {
		return err
	}
	if err := renderMap(ctx, &buf, r.s, r.p, data, "", 1, true); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(&buf, r.s.Syntax("}")); err != nil {
		return err
	}
	return render.AlignAnnotationsCtx(ctx, buf.String(), w)
}

func renderMap(ctx context.Context, w io.Writer, s kongfig.Styler, p Parser, data kongfig.ConfigData, prefix string, depth int, align bool) error {
	keys := render.OrderedKeys(ctx, prefix, data)
	pad := strings.Repeat(p.indent(), depth)

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
			if err := renderJSONSubMap(ctx, w, s, p, k, sub, path, pad, comma, depth, align); err != nil {
				return err
			}
			continue
		}

		if err := renderJSONLeaf(ctx, w, s, p, k, v, path, pad, comma, align); err != nil {
			return err
		}
	}
	return nil
}

func renderJSONSubMap(ctx context.Context, w io.Writer, s kongfig.Styler, p Parser, k string, sub kongfig.ConfigData, path, pad, comma string, depth int, align bool) error {
	var buf bytes.Buffer
	if err := renderMap(ctx, &buf, s, p, sub, path, depth+1, align); err != nil {
		return err
	}
	if buf.Len() == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "%s%s: %s\n", pad, s.Key(`"`+k+`"`), s.Syntax("{")); err != nil {
		return err
	}
	if _, err := buf.WriteTo(w); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s%s\n", pad, s.Syntax("}")+comma)
	return err
}

func renderJSONLeaf(ctx context.Context, w io.Writer, s kongfig.Styler, p Parser, k string, v any, path, pad, comma string, align bool) error {
	rv, isRV := v.(kongfig.RenderedValue)
	var leafVal any
	if isRV {
		leafVal = rv.Value
	} else {
		leafVal = v
	}

	if help := render.HelpText(ctx, path); help != "" {
		if _, err := fmt.Fprintf(w, "%s%s\n", pad, s.Comment("// "+help)); err != nil {
			return err
		}
	}

	valBytes, err := json.Marshal(leafVal)
	if err != nil {
		return fmt.Errorf("json render: marshal key %q: %w", k, err)
	}
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
	_, err = fmt.Fprintln(w, line)
	return err
}

// UnmarshalWithKeyOrder decodes JSON bytes and also returns the key insertion order
// per parent path from the document. Implements [kongfig.KeyOrderParser].
// Comments are stripped first when the parser is in JSONC mode.
func (p Parser) UnmarshalWithKeyOrder(b []byte) (kongfig.ConfigData, map[string][]string, error) {
	if p.Comments {
		b = stripComments(b)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return make(kongfig.ConfigData), nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	t, err := dec.Token()
	if err != nil {
		return nil, nil, fmt.Errorf("json: %w", err)
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return nil, nil, fmt.Errorf("json: expected top-level object, got %T", t)
	}
	data, order, err := jsonDecodeObject(dec, "")
	if err != nil {
		return nil, nil, err
	}
	if len(order) == 0 {
		order = nil
	}
	return data, order, nil
}

// jsonDecodeObject reads the body of a JSON object (after the opening '{' has been
// consumed) and returns the data map plus key insertion order for all nested objects.
func jsonDecodeObject(dec *json.Decoder, prefix string) (kongfig.ConfigData, map[string][]string, error) {
	data := make(kongfig.ConfigData)
	order := make(map[string][]string)
	var keys []string
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := kt.(string)
		if !ok {
			return nil, nil, fmt.Errorf("json: expected string key, got %T", kt)
		}
		keys = append(keys, key)
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		val, childOrder, err := jsonDecodeValue(dec, path)
		if err != nil {
			return nil, nil, err
		}
		data[key] = val
		maps.Copy(order, childOrder)
	}
	if _, err := dec.Token(); err != nil { // consume '}'
		return nil, nil, err
	}
	if len(keys) > 0 {
		order[prefix] = keys
	}
	return data, order, nil
}

// jsonDecodeValue reads one JSON value from the decoder and returns its Go representation.
// Nested objects are returned as [kongfig.ConfigData] with their key order captured.
// Arrays are returned as []any; order within arrays is not tracked (elements have no string keys).
func jsonDecodeValue(dec *json.Decoder, path string) (val any, order map[string][]string, err error) {
	t, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	switch d := t.(type) {
	case json.Delim:
		if d == '{' {
			return jsonDecodeObject(dec, path)
		}
		// array: collect elements, no key-order tracking needed
		var elems []any
		for dec.More() {
			v, _, err := jsonDecodeValue(dec, path)
			if err != nil {
				return nil, nil, err
			}
			elems = append(elems, v)
		}
		if _, err := dec.Token(); err != nil { // consume ']'
			return nil, nil, err
		}
		return elems, nil, nil
	default:
		// scalar: string, float64, bool, nil — matches encoding/json defaults
		return d, nil, nil
	}
}

// stripComments removes // line comments and /* */ block comments from JSON bytes,
// leaving string literals intact.
func stripComments(b []byte) []byte {
	out := make([]byte, 0, len(b))
	i := 0
	for i < len(b) {
		switch {
		case b[i] == '"':
			i, out = consumeJSONString(b, i, out)
		case i+1 < len(b) && b[i] == '/' && b[i+1] == '/':
			i = skipLineComment(b, i)
		case i+1 < len(b) && b[i] == '/' && b[i+1] == '*':
			i = skipBlockComment(b, i)
		default:
			out = append(out, b[i])
			i++
		}
	}
	return out
}

func consumeJSONString(b []byte, i int, out []byte) (end int, result []byte) {
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
	return i, out
}

func skipLineComment(b []byte, i int) int {
	for i < len(b) && b[i] != '\n' {
		i++
	}
	return i
}

func skipBlockComment(b []byte, i int) int {
	i += 2
	for i+1 < len(b) {
		if b[i] == '*' && b[i+1] == '/' {
			return i + 2
		}
		i++
	}
	return i
}
