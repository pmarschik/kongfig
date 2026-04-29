package yaml

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
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
	return render.AlignAnnotationsCtx(ctx, buf.String(), w)
}

//nolint:gocognit,cyclop,nestif // complex recursive renderer, intentional
func renderMap(ctx context.Context, w io.Writer, s kongfig.Styler, data kongfig.ConfigData, prefix string, indent int, align bool) error {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pad := strings.Repeat("  ", indent)
	tty, _ := render.TTYSizeKey.Read(ctx)
	cols := tty.Cols
	forceBlock := render.BlockCollections(ctx)

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

		if help := render.HelpText(ctx, path); help != "" {
			fmt.Fprintf(w, "%s%s\n", pad, s.Comment("# "+help))
		}

		// Determine the raw leaf value (for collection type dispatch).
		var rawVal any
		if isRV {
			rawVal = rv.Value
		} else {
			rawVal = v
		}

		// Collections (slices, maps) use YAML-native syntax and switch to block
		// style when the inline form would exceed the terminal width.
		var formatted string
		var blockLines []string
		if !isRV || !rv.Redacted {
			switch {
			case isYAMLCollection(rawVal):
				inline := yamlFlowValue(rawVal)
				keyW := render.VisualWidth(s.Key(k))
				if forceBlock || (cols > 0 && len(pad)+keyW+2+render.VisualWidth(inline) > cols) {
					if b, err := goyaml.Marshal(rawVal); err == nil {
						for bl := range strings.SplitSeq(strings.TrimRight(string(b), "\n"), "\n") {
							blockLines = append(blockLines, bl)
						}
					} else {
						formatted = inline // marshal failed, use inline as fallback
					}
				} else {
					formatted = inline
				}
			case isRV:
				formatted = fmt.Sprintf("%v", rv.Value)
			default:
				formatted = fmt.Sprintf("%v", v)
			}
		}

		if len(blockLines) > 0 {
			// Block form: annotation goes above the key line.
			if isRV {
				if ann := render.Annotation(ctx, rv, path, s); ann != "" {
					fmt.Fprintf(w, "%s%s\n", pad, s.Comment("# ")+ann)
				}
			}
			fmt.Fprintf(w, "%s%s:\n", pad, s.Key(k))
			blockPad := pad + "  "
			for _, bl := range blockLines {
				fmt.Fprintf(w, "%s%s\n", blockPad, bl)
			}
			continue
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

// isYAMLCollection reports whether v is a slice or map that deserves
// YAML-native syntax rather than Go's default %v formatting.
// Uses reflection to handle typed slices (e.g. []SomeStruct) and maps.
func isYAMLCollection(v any) bool {
	if v == nil {
		return false
	}
	k := reflect.TypeOf(v).Kind()
	return k == reflect.Slice || k == reflect.Map
}

// yamlFlowValue renders v as a YAML flow (inline) representation.
// For slices it produces "[v1, v2]"; for maps "{k: v}".
func yamlFlowValue(v any) string {
	b, err := goyaml.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	var root goyaml.Node
	if err = goyaml.Unmarshal(b, &root); err != nil || len(root.Content) == 0 {
		return strings.TrimRight(string(b), "\n")
	}
	setYAMLFlowStyle(root.Content[0])
	b2, merr := goyaml.Marshal(root.Content[0])
	if merr != nil {
		return strings.TrimRight(string(b), "\n")
	}
	return strings.TrimRight(string(b2), "\n")
}

func setYAMLFlowStyle(n *goyaml.Node) {
	if n.Kind == goyaml.SequenceNode || n.Kind == goyaml.MappingNode {
		n.Style = goyaml.FlowStyle
	}
	for _, c := range n.Content {
		setYAMLFlowStyle(c)
	}
}
