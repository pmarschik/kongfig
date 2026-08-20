package yaml

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
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
	_ kongfig.KeyOrderParser = Parser{}
	_ kongfig.DocumentParser = Parser{}
	_ kongfig.CtxMarshaler   = Parser{}
	_ kongfig.DocumentEditor = Parser{}
)

// Unmarshal decodes YAML bytes into a map.
func (Parser) Unmarshal(b []byte) (kongfig.ConfigData, error) {
	var out map[string]any
	if err := goyaml.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return kongfig.ToConfigData(out), nil
}

// UnmarshalWithKeyOrder decodes YAML bytes and also returns the key insertion order
// per parent path, as observed in the document. Implements [kongfig.KeyOrderParser].
func (p Parser) UnmarshalWithKeyOrder(b []byte) (kongfig.ConfigData, map[string][]string, error) {
	data, meta, err := p.UnmarshalDocument(b)
	return data, meta.KeyOrder, err
}

// UnmarshalDocument decodes YAML bytes and also returns the key insertion order
// per parent path and the position of every value in the document.
// Implements [kongfig.DocumentParser].
//
// Positions carry no file name: the parser only sees bytes, so
// [kongfig.SourcePosition.File] is left for the provider to fill in.
func (Parser) UnmarshalDocument(b []byte) (kongfig.ConfigData, kongfig.DocumentMeta, error) {
	var node goyaml.Node
	if err := goyaml.Unmarshal(b, &node); err != nil {
		return nil, kongfig.DocumentMeta{}, err
	}
	// yaml.v3 wraps the document in a document node; the actual content is node.Content[0].
	if node.Kind != goyaml.DocumentNode || len(node.Content) == 0 {
		return kongfig.ConfigData{}, kongfig.DocumentMeta{}, nil
	}
	root := node.Content[0]
	if root.Kind != goyaml.MappingNode {
		// Non-map root (scalar, sequence): no keys to walk.
		var out map[string]any
		if err := node.Decode(&out); err != nil {
			return nil, kongfig.DocumentMeta{}, err
		}
		return kongfig.ToConfigData(out), kongfig.DocumentMeta{}, nil
	}
	var out map[string]any
	if err := root.Decode(&out); err != nil {
		return nil, kongfig.DocumentMeta{}, err
	}
	meta := kongfig.DocumentMeta{
		KeyOrder:  make(map[string][]string),
		Positions: make(map[string]kongfig.SourcePosition),
	}
	collectYAMLDocumentMeta(root, "", meta)
	return kongfig.ToConfigData(out), meta, nil
}

// collectYAMLDocumentMeta walks a YAML mapping node and records, per parent path,
// the key insertion order and, per config path, the position of the value node.
// The value position is used rather than the key's because that is where a
// rejected value actually sits; for a nested mapping it is that mapping's first key.
func collectYAMLDocumentMeta(node *goyaml.Node, prefix string, meta kongfig.DocumentMeta) {
	if node.Kind != goyaml.MappingNode {
		return
	}
	// MappingNode Content is [key1, val1, key2, val2, ...]
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		key := keyNode.Value
		meta.KeyOrder[prefix] = append(meta.KeyOrder[prefix], key)

		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		meta.Positions[path] = kongfig.SourcePosition{Line: valNode.Line, Col: valNode.Column}

		if valNode.Kind == goyaml.MappingNode {
			collectYAMLDocumentMeta(valNode, path, meta)
		}
	}
}

// Marshal encodes a map to indented YAML bytes.
// The returned bytes always end with a trailing newline (added by the YAML encoder).
//
// Keys are ordered by the YAML encoder, which sorts them. Use [Parser.MarshalCtx]
// with a key order in the context to write a document in its own order instead.
func (p Parser) Marshal(data kongfig.ConfigData) ([]byte, error) {
	return p.MarshalCtx(context.Background(), data)
}

// MarshalCtx is [Parser.Marshal] with a context — the [kongfig.CtxMarshaler]
// implementation — so a key order carried under [kongfig.RenderKeyOrderKey] is
// written out as-is, which is what lets a config file be rewritten in the order it
// was parsed rather than alphabetized. Keys the order does not name follow it
// alphabetically. A sortby= mark or a [kongfig.KeySortFunc] in the context orders
// keys too, and is honored the same way. Without anything in the context that
// orders keys the output is byte-for-byte [Parser.Marshal]'s. See
// [kongfig.WithRenderKeyOrderCtx] and [kongfig.WithRenderKeySortCtx] for building
// such a context.
func (Parser) MarshalCtx(ctx context.Context, data kongfig.ConfigData) ([]byte, error) {
	var buf bytes.Buffer
	enc := goyaml.NewEncoder(&buf)
	enc.SetIndent(2)

	// With no order to honor, hand the map to the encoder as before: its own key
	// sorting is not plain alphabetical, so an ordered walk would quietly change
	// the output of every existing caller.
	var value any = data
	if render.HasKeyOrder(ctx) {
		node, err := orderedNode(ctx, "", data)
		if err != nil {
			return nil, err
		}
		value = node
	}
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// orderedNode builds the mapping node for data with its keys in the order ctx
// gives for path. Leaves are encoded by go-yaml itself, so scalar styling and
// quoting stay exactly what Marshal would have produced.
func orderedNode(ctx context.Context, path string, data kongfig.ConfigData) (*goyaml.Node, error) {
	node := &goyaml.Node{Kind: goyaml.MappingNode, Tag: "!!map"}
	for _, k := range render.OrderedKeys(ctx, path, data) {
		keyNode := &goyaml.Node{}
		if err := keyNode.Encode(k); err != nil {
			return nil, err
		}
		child := k
		if path != "" {
			child = path + "." + k
		}
		valNode, err := orderedValueNode(ctx, child, data[k])
		if err != nil {
			return nil, err
		}
		node.Content = append(node.Content, keyNode, valNode)
	}
	return node, nil
}

// orderedValueNode encodes one value, recursing into nested maps so their keys
// are ordered too. Sequences are left to the encoder: an order is keyed by path,
// and array elements have no path of their own.
func orderedValueNode(ctx context.Context, path string, v any) (*goyaml.Node, error) {
	switch sub := v.(type) {
	case kongfig.ConfigData:
		return orderedNode(ctx, path, sub)
	case map[string]any:
		return orderedNode(ctx, path, sub)
	}
	node := &goyaml.Node{}
	if err := node.Encode(v); err != nil {
		return nil, err
	}
	return node, nil
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

// yamlRenderOpts groups the layout options that are computed once per Render call
// and shared across all recursive rendering functions.
type yamlRenderOpts struct {
	cols       int
	forceBlock bool
	align      bool
}

func (r *renderer) Render(ctx context.Context, w io.Writer, data kongfig.ConfigData) error {
	tty, _ := render.TTYSizeKey.Read(ctx)
	opts := yamlRenderOpts{
		cols:       tty.Cols,
		forceBlock: render.BlockCollections(ctx),
	}
	if !render.AlignSources(ctx) {
		return renderMap(ctx, w, r.s, data, "", 0, opts)
	}
	// Two-pass: render with annotation markers, then align.
	opts.align = true
	var buf bytes.Buffer
	if err := renderMap(ctx, &buf, r.s, data, "", 0, opts); err != nil {
		return err
	}
	return render.AlignAnnotationsCtx(ctx, buf.String(), w)
}

func renderMap(ctx context.Context, w io.Writer, s kongfig.Styler, data kongfig.ConfigData, prefix string, indent int, opts yamlRenderOpts) error {
	keys := render.OrderedKeys(ctx, prefix, data)
	pad := strings.Repeat("  ", indent)

	for _, k := range keys {
		v := data[k]
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}

		// A mapping is not dropped on its own emptiness here: the recursion below
		// writes nothing for one whose keys all went, and the header follows.
		if render.OmitEmpty(ctx, path, v) {
			continue
		}

		if sub, ok := v.(kongfig.ConfigData); ok {
			var buf bytes.Buffer
			if err := renderMap(ctx, &buf, s, sub, path, indent+1, opts); err != nil {
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

		if err := renderYAMLLeaf(ctx, w, s, k, v, path, pad, opts); err != nil {
			return err
		}
	}
	return nil
}

func renderYAMLLeaf(ctx context.Context, w io.Writer, s kongfig.Styler, k string, v any, path, pad string, opts yamlRenderOpts) error {
	rv, isRV := v.(kongfig.RenderedValue)
	var rawVal any
	if isRV {
		rawVal = rv.Value
	} else {
		rawVal = v
	}

	if help := render.HelpText(ctx, path); help != "" {
		fmt.Fprintf(w, "%s%s\n", pad, s.Comment("# "+help))
	}

	formatted, useBlock := resolveYAMLLeafFormat(rv, isRV, rawVal, k, pad, s, opts)

	if useBlock {
		if isRV {
			if ann := render.Annotation(ctx, rv, path, s); ann != "" {
				fmt.Fprintf(w, "%s%s\n", pad, s.Comment("# ")+ann)
			}
		}
		fmt.Fprintf(w, "%s%s:\n", pad, s.Key(k))
		renderYAMLCollection(w, s, rawVal, pad+"  ")
		return nil
	}

	line := fmt.Sprintf("%s%s: %s", pad, s.Key(k), render.Value(s, v, formatted))
	if isRV {
		line += yamlAnnSuffix(ctx, rv, path, s, opts.align)
	}
	fmt.Fprintln(w, line)
	return nil
}

func resolveYAMLLeafFormat(rv kongfig.RenderedValue, isRV bool, rawVal any, k, pad string, s kongfig.Styler, opts yamlRenderOpts) (formatted string, useBlock bool) {
	if isRV && rv.Redacted {
		return "", false
	}
	switch {
	case rawVal == nil:
		// YAML has a null literal; Go's "<nil>" is not it. Spelling it the way
		// Marshal does keeps rendered output pasteable back into a config file.
		return "null", false
	case isYAMLCollection(rawVal):
		inline := yamlFlowValue(rawVal)
		keyW := render.VisualWidth(s.Key(k))
		if opts.forceBlock || (opts.cols > 0 && len(pad)+keyW+2+render.VisualWidth(inline) > opts.cols) {
			return "", true
		}
		return inline, false
	case isRV:
		return fmt.Sprintf("%v", rv.Value), false
	default:
		return fmt.Sprintf("%v", rawVal), false
	}
}

func yamlAnnSuffix(ctx context.Context, rv kongfig.RenderedValue, path string, s kongfig.Styler, align bool) string {
	ann := render.Annotation(ctx, rv, path, s)
	if ann == "" {
		return ""
	}
	if align {
		return render.AnnMarker + "  " + s.Comment("# ") + ann
	}
	return "  " + s.Comment("# ") + ann
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

// renderYAMLCollection renders a YAML collection in block form with Styler-applied
// key and value coloring. It marshals v to a YAML AST and walks it with styling.
// pad is the left-margin prefix for top-level items in the collection.
func renderYAMLCollection(w io.Writer, s kongfig.Styler, v any, pad string) {
	b, err := goyaml.Marshal(v)
	if err != nil {
		return // unlikely; key line already printed by caller
	}
	var root goyaml.Node
	if err := goyaml.Unmarshal(b, &root); err != nil || len(root.Content) == 0 {
		// Fallback: raw goyaml lines without key styling.
		for line := range strings.SplitSeq(strings.TrimRight(string(b), "\n"), "\n") {
			fmt.Fprintf(w, "%s%s\n", pad, line)
		}
		return
	}
	renderYAMLNode(w, s, root.Content[0], pad)
}

// renderYAMLNode renders a YAML AST node in block form with styled keys/values.
func renderYAMLNode(w io.Writer, s kongfig.Styler, node *goyaml.Node, pad string) {
	switch node.Kind {
	case goyaml.DocumentNode, goyaml.ScalarNode, goyaml.AliasNode:
		// these node types don't appear as direct children of collection nodes
	case goyaml.SequenceNode:
		for _, elem := range node.Content {
			switch elem.Kind {
			case goyaml.MappingNode:
				if len(elem.Content) == 0 {
					fmt.Fprintf(w, "%s- {}\n", pad)
				} else {
					renderYAMLSeqMap(w, s, elem, pad)
				}
			case goyaml.SequenceNode:
				fmt.Fprintf(w, "%s-\n", pad)
				renderYAMLNode(w, s, elem, pad+"  ")
			default:
				fmt.Fprintf(w, "%s- %s\n", pad, styledYAMLScalar(s, elem))
			}
		}
	case goyaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			kn := node.Content[i]
			vn := node.Content[i+1]
			if vn.Kind == goyaml.MappingNode || vn.Kind == goyaml.SequenceNode {
				fmt.Fprintf(w, "%s%s:\n", pad, s.Key(kn.Value))
				renderYAMLNode(w, s, vn, pad+"  ")
			} else {
				fmt.Fprintf(w, "%s%s: %s\n", pad, s.Key(kn.Value), styledYAMLScalar(s, vn))
			}
		}
	}
}

// renderYAMLSeqMap renders a YAML mapping node as a sequence element.
// The first key gets the "- " list marker; subsequent keys get "  " continuation indent.
// Nested collections inside values are indented by 4 extra spaces (marker + indent).
func renderYAMLSeqMap(w io.Writer, s kongfig.Styler, mnode *goyaml.Node, pad string) {
	for i := 0; i+1 < len(mnode.Content); i += 2 {
		kn := mnode.Content[i]
		vn := mnode.Content[i+1]
		prefix := "  "
		if i == 0 {
			prefix = "- "
		}
		if vn.Kind == goyaml.MappingNode || vn.Kind == goyaml.SequenceNode {
			fmt.Fprintf(w, "%s%s%s:\n", pad, prefix, s.Key(kn.Value))
			renderYAMLNode(w, s, vn, pad+"    ")
		} else {
			fmt.Fprintf(w, "%s%s%s: %s\n", pad, prefix, s.Key(kn.Value), styledYAMLScalar(s, vn))
		}
	}
}

// styledYAMLScalar applies the appropriate Styler method based on the YAML node's tag.
func styledYAMLScalar(s kongfig.Styler, node *goyaml.Node) string {
	switch node.Tag {
	case "!!int", "!!float":
		return s.Number(node.Value)
	case "!!bool":
		return s.Bool(node.Value)
	case "!!null":
		return s.Null("null")
	default:
		return s.String(node.Value)
	}
}
