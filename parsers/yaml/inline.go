package yaml

import (
	"context"
	"strings"

	kongfig "github.com/pmarschik/kongfig"
	render "github.com/pmarschik/kongfig/render"
	goyaml "gopkg.in/yaml.v3"
)

// DefaultInlineMaxKeys is the number of direct keys a marked mapping may hold
// and still be emitted as a flow mapping when [WithInlineMaxKeys] is not used.
const DefaultInlineMaxKeys = 3

// inlinePolicy decides which mappings may collapse into YAML flow mappings.
//
// It is the YAML twin of the policy parsers/toml applies to inline tables: the
// two formats spell the compact shape differently but read the same marks, so a
// kongfig:",inline" tag means the same thing in both.
type inlinePolicy struct {
	// ctxPaths are the marks carried in the render context by
	// [kongfig.InlineTablesKey], i.e. those derived from ,inline struct tags.
	// A value of 0 defers to defaultMax.
	ctxPaths map[string]int
	// overflowCtx are the marks carried in the render context by
	// [kongfig.InlineOverflowKey], i.e. those derived from ,overflow struct tags.
	overflowCtx map[string]bool
	// patterns are the marks configured on the parser via [WithInlineMaps].
	patterns []string
	// overflowPatterns are the marks configured via [WithInlineOverflow]. Like the
	// struct tag, an overflow mark implies an inline one.
	overflowPatterns []string
	defaultMax       int
	checkWidth       bool
}

// inlinePolicyFor builds the policy shared by Marshal and Render. checkWidth is
// set only on the render path: written files must not depend on the width of the
// terminal that happened to produce them.
func (p Parser) inlinePolicyFor(checkWidth bool) inlinePolicy {
	limit := DefaultInlineMaxKeys
	if p.inlineMaxKeys != nil {
		limit = *p.inlineMaxKeys
	}
	return inlinePolicy{
		patterns:         p.inlinePatterns,
		overflowPatterns: p.overflowPatterns,
		defaultMax:       limit,
		checkWidth:       checkWidth,
	}
}

// withCtxMarks returns the policy with the marks a config struct published to
// ctx folded in, so a ,inline tag needs no parser option to take effect.
func (ip inlinePolicy) withCtxMarks(ctx context.Context) inlinePolicy {
	ip.ctxPaths = kongfig.InlineTablesKey.GetAll(ctx)
	ip.overflowCtx = kongfig.InlineOverflowKey.GetAll(ctx)
	return ip
}

func (ip inlinePolicy) empty() bool {
	return len(ip.patterns) == 0 && len(ip.ctxPaths) == 0 &&
		len(ip.overflowPatterns) == 0 && len(ip.overflowCtx) == 0
}

// overflows reports whether path keeps its compact one-line form even when that
// line does not fit the terminal. Patterns are matched the same way
// [inlinePolicy.maxKeysFor] matches them.
func (ip inlinePolicy) overflows(path string) bool {
	if path == "" {
		return false
	}
	segs := strings.Split(path, ".")
	for _, pat := range ip.overflowPatterns {
		if matchPathPattern(strings.Split(pat, "."), segs) {
			return true
		}
	}
	for pat, on := range ip.overflowCtx {
		if on && matchPathPattern(strings.Split(pat, "."), segs) {
			return true
		}
	}
	return false
}

// maxKeysFor reports whether path is marked for collapsing and, if so, how many
// direct keys the mapping may hold. Patterns are matched segment by segment; a
// "*" segment matches any single segment. When several context marks match, the
// largest explicit limit wins, so the result does not depend on map order.
//
// An overflow mark counts as an inline mark without a limit of its own: asking
// for the compact form past the edge of the window is asking for the compact
// form.
func (ip inlinePolicy) maxKeysFor(path string) (int, bool) {
	if path == "" {
		return 0, false
	}
	segs := strings.Split(path, ".")

	marked := ip.overflows(path)
	for _, pat := range ip.patterns {
		if matchPathPattern(strings.Split(pat, "."), segs) {
			marked = true
			break
		}
	}
	explicit := 0
	for pat, n := range ip.ctxPaths {
		if !matchPathPattern(strings.Split(pat, "."), segs) {
			continue
		}
		marked = true
		if n > explicit {
			explicit = n
		}
	}

	if !marked {
		return 0, false
	}
	if explicit > 0 {
		return explicit, true
	}
	return ip.defaultMax, true
}

func matchPathPattern(pat, segs []string) bool {
	if len(pat) != len(segs) {
		return false
	}
	for i, p := range pat {
		if p != "*" && p != segs[i] {
			return false
		}
	}
	return true
}

// inlineMappingLine returns the one-line form of a marked mapping — the whole
// "key: {a: 1, b: 2}" line, indentation included — and whether the mapping may
// take it. A mapping that is not marked, holds more keys than the mark allows,
// or would run past the terminal keeps its block form.
func inlineMappingLine(ctx context.Context, s kongfig.Styler, k string, sub kongfig.ConfigData, path, pad string, opts yamlRenderOpts) (string, bool) {
	if opts.forceBlock || opts.inline.empty() {
		return "", false
	}
	maxKeys, marked := opts.inline.maxKeysFor(path)
	if !marked {
		return "", false
	}
	// Filter before counting: a key that is not written does not fill the mapping.
	// An empty one is left to the block path, which writes nothing for it at all.
	keys := inlineKeys(ctx, path, sub)
	if len(keys) == 0 || len(keys) > maxKeys {
		return "", false
	}

	line := pad + s.Key(k) + ": " + yamlFlowMapping(ctx, s, path, sub, keys)

	// Width only gates the terminal; a written file must not depend on the size
	// of the terminal that produced it. An overflow mark waives the gate: the
	// caller has said the compact shape is worth a line that runs past the edge.
	if opts.inline.checkWidth && opts.cols > 0 && !opts.inline.overflows(path) &&
		render.VisualWidth(line) > opts.cols {
		return "", false
	}
	return line, true
}

// inlineKeys returns the keys of sub that are actually written, in render order.
func inlineKeys(ctx context.Context, path string, sub kongfig.ConfigData) []string {
	ordered := render.OrderedKeys(ctx, path, sub)
	keys := make([]string, 0, len(ordered))
	for _, k := range ordered {
		child := k
		if path != "" {
			child = path + "." + k
		}
		if render.OmitEmpty(ctx, child, sub[k]) {
			continue
		}
		keys = append(keys, k)
	}
	return keys
}

// yamlFlowMapping renders sub as a styled YAML flow mapping.
func yamlFlowMapping(ctx context.Context, s kongfig.Styler, path string, sub kongfig.ConfigData, keys []string) string {
	pairs := make([]string, len(keys))
	for i, k := range keys {
		child := k
		if path != "" {
			child = path + "." + k
		}
		pairs[i] = s.Key(k) + s.Syntax(":") + " " + yamlFlowNode(ctx, s, child, sub[k])
	}
	return s.Syntax("{") + strings.Join(pairs, s.Syntax(",")+" ") + s.Syntax("}")
}

// yamlFlowNode renders one value inside a flow mapping. Nested mappings are
// collapsed too — a block mapping cannot live inside a flow one — and every
// other value is spelled the way go-yaml would spell it, so what the line says
// parses back to what it was built from.
func yamlFlowNode(ctx context.Context, s kongfig.Styler, path string, v any) string {
	raw := v
	if rv, ok := v.(kongfig.RenderedValue); ok {
		if rv.Redacted {
			return render.Value(s, v, "")
		}
		raw = rv.Value
	}
	switch sub := raw.(type) {
	case kongfig.ConfigData:
		return yamlFlowMapping(ctx, s, path, sub, inlineKeys(ctx, path, sub))
	case map[string]any:
		data := kongfig.ConfigData(sub)
		return yamlFlowMapping(ctx, s, path, data, inlineKeys(ctx, path, data))
	}
	return render.Value(s, v, yamlFlowValue(raw))
}

// leafAnnotation is one collapsed leaf's provenance, keyed by its path relative
// to the mapping that collapsed.
type leafAnnotation struct {
	key string
	ann string
}

// inlineAnnotation returns the provenance comment for a collapsed mapping. The
// lines its values would have been annotated on are gone, so their annotations
// move onto the line that replaced them: one comment when every value shares a
// source, otherwise one group per source naming the keys it covers.
func inlineAnnotation(ctx context.Context, s kongfig.Styler, path string, sub kongfig.ConfigData) string {
	return strings.Join(annotationGroups(collectLeafAnnotations(ctx, s, path, "", sub, nil)), ", ")
}

func collectLeafAnnotations(ctx context.Context, s kongfig.Styler, path, rel string, sub kongfig.ConfigData, out []leafAnnotation) []leafAnnotation {
	for _, k := range inlineKeys(ctx, path, sub) {
		childPath := k
		if path != "" {
			childPath = path + "." + k
		}
		childRel := k
		if rel != "" {
			childRel = rel + "." + k
		}
		v := sub[k]
		if nested, isMap := asConfigData(v); isMap {
			out = collectLeafAnnotations(ctx, s, childPath, childRel, nested, out)
			continue
		}
		if slice, isSlice := v.([]any); isSlice {
			for _, elem := range slice {
				if nested, isMap := asConfigData(elem); isMap {
					out = collectLeafAnnotations(ctx, s, childPath, childRel, nested, out)
				}
			}
			continue
		}
		rv, isRV := v.(kongfig.RenderedValue)
		if !isRV {
			continue
		}
		ann := render.Annotation(ctx, rv, childPath, s)
		if ann == "" || containsLeafAnnotation(out, childRel, ann) {
			continue
		}
		out = append(out, leafAnnotation{key: childRel, ann: ann})
	}
	return out
}

func containsLeafAnnotation(parts []leafAnnotation, key, ann string) bool {
	for _, p := range parts {
		if p.key == key && p.ann == ann {
			return true
		}
	}
	return false
}

// annotationGroups turns the collected leaf annotations into the comment groups
// written after the line: one group when they all agree, otherwise "a, b: src"
// per distinct source, in the order the keys were walked.
func annotationGroups(parts []leafAnnotation) []string {
	if len(parts) == 0 {
		return nil
	}
	same := true
	for _, p := range parts[1:] {
		if p.ann != parts[0].ann {
			same = false
			break
		}
	}
	if same {
		return []string{parts[0].ann}
	}
	order := make([]string, 0, len(parts))
	keys := make(map[string][]string, len(parts))
	for _, p := range parts {
		if _, seen := keys[p.ann]; !seen {
			order = append(order, p.ann)
		}
		keys[p.ann] = append(keys[p.ann], p.key)
	}
	groups := make([]string, len(order))
	for i, ann := range order {
		groups[i] = strings.Join(keys[ann], ", ") + ": " + ann
	}
	return groups
}

// asConfigData unwraps v to the mapping it holds, if any.
func asConfigData(v any) (kongfig.ConfigData, bool) {
	switch val := v.(type) {
	case kongfig.ConfigData:
		return val, true
	case map[string]any:
		return kongfig.ConfigData(val), true
	case kongfig.RenderedValue:
		return asConfigData(val.Value)
	}
	return nil, false
}

// applyInlineFlow switches the mapping nodes at marked paths to flow style
// before the document is written. Width is not consulted: a file must read the
// same however wide the terminal that wrote it was.
func applyInlineFlow(node *goyaml.Node, path string, ip inlinePolicy) {
	switch node.Kind {
	case goyaml.DocumentNode:
		for _, c := range node.Content {
			applyInlineFlow(c, path, ip)
		}
	case goyaml.MappingNode:
		if maxKeys, marked := ip.maxKeysFor(path); marked && len(node.Content)/2 <= maxKeys {
			setYAMLFlowStyle(node)
			return
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			child := node.Content[i].Value
			if path != "" {
				child = path + "." + child
			}
			applyInlineFlow(node.Content[i+1], child, ip)
		}
	case goyaml.SequenceNode, goyaml.ScalarNode, goyaml.AliasNode:
		// Only mappings have a path of their own to be marked by.
	}
}
