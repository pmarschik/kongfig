package yaml

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	kongfig "github.com/pmarschik/kongfig"
	goyaml "gopkg.in/yaml.v3"
)

// This file edits a document instead of rewriting it: the parsed node tree says
// which value is written where, and only the bytes a data change touches are
// replaced. Comments, key order, indentation, quoting and blank lines survive
// because nothing else is rewritten.
//
// Anything it cannot bound safely — a value spread over several lines, a block
// of text, a mapping that merges another one — is refused rather than guessed
// at. A refused edit costs the caller a fallback; one written over the wrong
// bytes costs them their file.

// ErrCannotEdit reports a change YAML cannot express as an edit of the document
// it was handed, so the document was left alone.
var ErrCannotEdit = errors.New("yaml: cannot express that as an edit of this document")

// EditDocument rewrites src so it holds want, touching only the text the data
// change touches. It implements [kongfig.DocumentEditor]; call it through
// [kongfig.EditDocument] to have the result checked against want.
//
// want is the whole document, not a patch — a key it does not mention is a key
// the rewrite removes. A value the editor cannot rewrite in place is refused and
// the document is left alone; the caller can fall back to [Parser.Marshal] and
// accept a reformatted file.
func (p Parser) EditDocument(src []byte, want kongfig.ConfigData) ([]byte, error) {
	got, err := p.Unmarshal(src)
	if err != nil {
		return nil, fmt.Errorf("yaml: read the document to edit: %w", err)
	}
	var doc goyaml.Node
	if err := goyaml.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("yaml: read the document to edit: %w", err)
	}
	if doc.Kind != goyaml.DocumentNode || len(doc.Content) == 0 {
		return nil, cannotEdit("", "the document has no content")
	}
	e := &docEditor{src: src, lines: lineStarts(src)}
	if err := e.mapping(doc.Content[0], got, want, ""); err != nil {
		return nil, err
	}
	return e.apply()
}

func cannotEdit(path, why string) error {
	if path == "" {
		path = "the document root"
	}
	return fmt.Errorf("%w: %s: %s", ErrCannotEdit, path, why)
}

// span is a half-open byte range of the document.
type span struct {
	start int
	end   int
}

// docEdit replaces the bytes of at with text; an empty span inserts, empty text
// deletes.
type docEdit struct {
	text string
	at   span
}

// docEditor collects the edits a data change implies and splices them in at the
// end, so a refusal anywhere leaves the document untouched.
type docEditor struct {
	src   []byte
	lines []int // byte offset of each line start
	edits []docEdit
}

func (e *docEditor) replace(at span, text string) {
	e.edits = append(e.edits, docEdit{at: at, text: text})
}

func (e *docEditor) insert(at int, text string) {
	e.edits = append(e.edits, docEdit{at: span{start: at, end: at}, text: text})
}

func (e *docEditor) remove(at span) {
	e.edits = append(e.edits, docEdit{at: at})
}

// apply splices the edits in from the back, so each one's offsets still refer to
// the document it was measured against.
func (e *docEditor) apply() ([]byte, error) {
	if len(e.edits) == 0 {
		return slices.Clone(e.src), nil
	}
	edits := slices.Clone(e.edits)
	slices.SortStableFunc(edits, func(a, b docEdit) int { return b.at.start - a.at.start })
	out := slices.Clone(e.src)
	prev := len(e.src)
	for _, ed := range edits {
		if ed.at.end > prev {
			// Two edits over the same bytes would each rewrite what the other
			// read; refusing is the only safe answer.
			return nil, fmt.Errorf("%w: two changes over the same text", ErrCannotEdit)
		}
		out = slices.Concat(out[:ed.at.start], []byte(ed.text), out[ed.at.end:])
		prev = ed.at.start
	}
	return out, nil
}

// value edits one value so it holds want. Values that did not change are left
// alone, which is what keeps the spelling the author chose.
func (e *docEditor) value(node *goyaml.Node, got, want any, path string, indent int) error {
	if kongfig.EqualValues(got, want) {
		return nil
	}
	if node.Kind == goyaml.AliasNode || node.Anchor != "" {
		return cannotEdit(path, "the value is written with an anchor or an alias")
	}
	if wm, ok := wantMapping(want); ok && node.Kind == goyaml.MappingNode {
		return e.mapping(node, got, wm, path)
	}
	if ws, ok := wantList(want); ok && node.Kind == goyaml.SequenceNode {
		return e.sequence(node, got, ws, path, indent)
	}
	return e.rewrite(node, want, path)
}

// rewrite replaces the whole text of a value, for a value the document spells out
// in one place.
func (e *docEditor) rewrite(node *goyaml.Node, want any, path string) error {
	at, err := e.tokenSpan(node, path)
	if err != nil {
		return err
	}
	text, err := encode(want, node, path)
	if err != nil {
		return err
	}
	e.replace(at, text)
	return nil
}

// mapping edits a mapping key by key: values that changed, keys want adds, keys
// want leaves out. want is the whole document, so a key it does not mention goes
// away.
func (e *docEditor) mapping(node *goyaml.Node, got any, want map[string]any, path string) error {
	if node.Kind != goyaml.MappingNode {
		return e.rewrite(node, want, path)
	}
	if node.Style&goyaml.FlowStyle != 0 {
		return e.flowMapping(node, got, want, path)
	}
	at := map[string]int{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "<<" {
			return cannotEdit(path, "the mapping merges another one")
		}
		at[node.Content[i].Value] = i
	}
	own := node.Column - 1
	gotMap, _ := wantMapping(got)
	for _, key := range sortedKeys(want) {
		i, written := at[key]
		if !written {
			if err := e.insertKey(node, key, want[key], path, own); err != nil {
				return err
			}
			continue
		}
		if err := e.value(node.Content[i+1], gotMap[key], want[key], joinPath(path, key), own); err != nil {
			return err
		}
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if _, wanted := want[key]; wanted {
			continue
		}
		e.remove(span{
			start: e.lineStartAt(e.offset(node.Content[i])),
			end:   e.blockEnd(e.offset(node.Content[i+1]), own),
		})
	}
	return nil
}

// insertKey writes a key the mapping does not have yet, on a line of its own
// after the keys already there and indented with them.
func (e *docEditor) insertKey(node *goyaml.Node, key string, value any, path string, indent int) error {
	at := joinPath(path, key)
	if len(node.Content) < 2 {
		return cannotEdit(at, "there is no place in the document to write it")
	}
	last := node.Content[len(node.Content)-1]
	text, err := keyLines(key, value, indent, at)
	if err != nil {
		return err
	}
	e.insert(e.blockEnd(e.offset(last), indent), text)
	return nil
}

// keyLines writes one key and its value: on one line for a scalar, as an indented
// block under the key for a mapping or a list.
func keyLines(key string, value any, indent int, path string) (string, error) {
	body, err := goyaml.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrCannotEdit, path, err)
	}
	pad := strings.Repeat(" ", indent)
	written := strings.TrimRight(string(body), "\n")
	if !strings.Contains(written, "\n") && !isCollection(value) {
		return pad + key + ": " + written + "\n", nil
	}
	var out strings.Builder
	out.WriteString(pad + key + ":\n")
	for line := range strings.SplitSeq(written, "\n") {
		out.WriteString(pad + "  " + line + "\n")
	}
	return out.String(), nil
}

// flowMapping edits a { key: value } mapping, where a new key goes before the
// closing brace.
func (e *docEditor) flowMapping(node *goyaml.Node, got any, want map[string]any, path string) error {
	end, err := e.flowEnd(node, path)
	if err != nil {
		return err
	}
	at := map[string]int{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		at[node.Content[i].Value] = i
	}
	gotMap, _ := wantMapping(got)
	for _, key := range sortedKeys(want) {
		i, written := at[key]
		if !written {
			text, err := encode(want[key], nil, joinPath(path, key))
			if err != nil {
				return err
			}
			pair := key + ": " + text
			if len(node.Content) > 0 {
				pair = ", " + pair
			}
			e.insert(end-1, pair)
			continue
		}
		if err := e.value(node.Content[i+1], gotMap[key], want[key], joinPath(path, key), 0); err != nil {
			return err
		}
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if _, wanted := want[key]; wanted {
			continue
		}
		valueEnd, err := e.tokenSpan(node.Content[i+1], joinPath(path, key))
		if err != nil {
			return err
		}
		e.remove(e.withSeparator(span{start: e.offset(node.Content[i]), end: valueEnd.end}))
	}
	return nil
}

// encode writes a value the way the document writes its values: a string keeps
// the quoting of the value it replaces or joins, everything else is spelled the
// way YAML spells it.
func encode(v any, like *goyaml.Node, path string) (string, error) {
	var node goyaml.Node
	if err := node.Encode(v); err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrCannotEdit, path, err)
	}
	if node.Kind != goyaml.ScalarNode {
		return yamlFlowValue(v), nil
	}
	if _, isString := v.(string); isString && like != nil && like.Kind == goyaml.ScalarNode {
		if quoted := like.Style & (goyaml.SingleQuotedStyle | goyaml.DoubleQuotedStyle); quoted != 0 {
			node.Style = quoted
		}
	}
	out, err := goyaml.Marshal(&node)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrCannotEdit, path, err)
	}
	written := strings.TrimRight(string(out), "\n")
	if strings.Contains(written, "\n") {
		return "", cannotEdit(path, "the value does not fit on one line")
	}
	return written, nil
}

// tokenSpan is the text that spells a value, for a value the editor can bound
// safely. What it refuses to bound it refuses to rewrite.
func (e *docEditor) tokenSpan(node *goyaml.Node, path string) (span, error) {
	start := e.offset(node)
	if node.Kind != goyaml.ScalarNode {
		if node.Style&goyaml.FlowStyle == 0 {
			return span{}, cannotEdit(path, "the value is written as a block, not as one value")
		}
		end, err := e.flowEnd(node, path)
		return span{start: start, end: end}, err
	}
	switch {
	case node.Style&(goyaml.LiteralStyle|goyaml.FoldedStyle) != 0:
		return span{}, cannotEdit(path, "the value is written as a block of text")
	case node.Style&(goyaml.SingleQuotedStyle|goyaml.DoubleQuotedStyle) != 0:
		return e.quotedSpan(start, path)
	case node.Tag == "!!null" && node.Value == "":
		return span{}, cannotEdit(path, "the document leaves the value empty")
	}
	// A plain scalar ends with its line, unless it sits in a flow collection,
	// where a comma or a closing bracket ends it. Which one it is shows in the
	// text: only the reading that spells the value the parser reported is right.
	for _, end := range []int{e.lineContentEnd(start), e.flowItemEnd(start)} {
		if string(e.src[start:end]) == node.Value {
			return span{start: start, end: end}, nil
		}
	}
	return span{}, cannotEdit(path, "the value is spread over more than one line")
}

// flowItemEnd is where a plain scalar inside a flow collection ends: at the
// separator or the bracket that closes the collection.
func (e *docEditor) flowItemEnd(start int) int {
	end := start
	for end < len(e.src) && !strings.ContainsRune(",]}\n", rune(e.src[end])) {
		if isInlineSpace(e.src[end]) && end+1 < len(e.src) && e.src[end+1] == '#' {
			break
		}
		end++
	}
	for end > start && isInlineSpace(e.src[end-1]) {
		end--
	}
	return end
}

// quotedSpan bounds a quoted scalar, which the editor only rewrites while it
// stays on its line.
func (e *docEditor) quotedSpan(start int, path string) (span, error) {
	quote := e.src[start]
	for i := start + 1; i < len(e.src); i++ {
		switch e.src[i] {
		case '\n':
			return span{}, cannotEdit(path, "the value is spread over more than one line")
		case '\\':
			if quote == '"' {
				i++
			}
		case quote:
			if quote == '\'' && i+1 < len(e.src) && e.src[i+1] == '\'' {
				i++
				continue
			}
			return span{start: start, end: i + 1}, nil
		}
	}
	return span{}, cannotEdit(path, "the value has no end on this line")
}

// flowEnd finds the closing bracket of a flow collection, reading past the
// brackets and separators inside its strings.
func (e *docEditor) flowEnd(node *goyaml.Node, path string) (int, error) {
	start := e.offset(node)
	if start >= len(e.src) || (e.src[start] != '[' && e.src[start] != '{') {
		return 0, cannotEdit(path, "the list or mapping does not start where the parser says")
	}
	depth := 0
	for i := start; i < len(e.src); i++ {
		switch c := e.src[i]; c {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				return i + 1, nil
			}
		case '"', '\'':
			at, err := e.quotedSpan(i, path)
			if err != nil {
				return 0, err
			}
			i = at.end - 1
		}
	}
	return 0, cannotEdit(path, "the list or mapping has no end")
}

// withSeparator widens a span to the comma that separates it from the next item,
// or, for the last item, to the one that separates it from the previous.
func (e *docEditor) withSeparator(at span) span {
	end := at.end
	for end < len(e.src) && isInlineSpace(e.src[end]) {
		end++
	}
	if end < len(e.src) && e.src[end] == ',' {
		end++
		for end < len(e.src) && isInlineSpace(e.src[end]) {
			end++
		}
		return span{start: at.start, end: end}
	}
	start := at.start
	for start > 0 && isInlineSpace(e.src[start-1]) {
		start--
	}
	if start > 0 && e.src[start-1] == ',' {
		start--
	}
	return span{start: start, end: at.end}
}

// --- positions ---

// offset is where a node starts, from the line and column the parser reports.
func (e *docEditor) offset(node *goyaml.Node) int {
	line := max(node.Line-1, 0)
	if line >= len(e.lines) {
		return len(e.src)
	}
	return min(e.lines[line]+node.Column-1, len(e.src))
}

func (e *docEditor) lineStartAt(off int) int {
	for i := off - 1; i >= 0; i-- {
		if e.src[i] == '\n' {
			return i + 1
		}
	}
	return 0
}

// lineEnd is the offset just past the newline that ends the line off is on.
func (e *docEditor) lineEnd(off int) int {
	for i := off; i < len(e.src); i++ {
		if e.src[i] == '\n' {
			return i + 1
		}
	}
	return len(e.src)
}

// lineContentEnd is where the text of a value ends on its line: before a trailing
// comment and before the whitespace that leads to it.
func (e *docEditor) lineContentEnd(start int) int {
	end := start
	for end < len(e.src) && e.src[end] != '\n' {
		if isInlineSpace(e.src[end]) && end+1 < len(e.src) && e.src[end+1] == '#' {
			break
		}
		end++
	}
	for end > start && isInlineSpace(e.src[end-1]) {
		end--
	}
	return end
}

// blockEnd is where a block value ends: after its own line and every line under
// it that is indented deeper than the key or dash it belongs to. Blank lines
// count as part of the block only when deeper-indented text follows them.
func (e *docEditor) blockEnd(start, indent int) int {
	end := e.lineEnd(start)
	for end < len(e.src) {
		probe := end
		for probe < len(e.src) && e.blankLine(probe) {
			probe = e.lineEnd(probe)
		}
		if probe >= len(e.src) || e.indentAt(probe) <= indent {
			return end
		}
		end = e.lineEnd(probe)
	}
	return end
}

func (e *docEditor) blankLine(lineStart int) bool {
	for i := lineStart; i < len(e.src) && e.src[i] != '\n'; i++ {
		if !isInlineSpace(e.src[i]) {
			return false
		}
	}
	return true
}

func (e *docEditor) indentAt(lineStart int) int {
	i := lineStart
	for i < len(e.src) && isInlineSpace(e.src[i]) {
		i++
	}
	return i - lineStart
}

// dashIndent reports where the "-" of a list element sits, for an element that
// starts its own line — which is what an insertion or a deletion needs.
func (e *docEditor) dashIndent(elem *goyaml.Node) (int, bool) {
	start := e.lineStartAt(e.offset(elem))
	indent := e.indentAt(start)
	if start+indent >= len(e.src) || e.src[start+indent] != '-' {
		return 0, false
	}
	return indent, true
}

func lineStarts(src []byte) []int {
	lines := []int{0}
	for i, c := range src {
		if c == '\n' {
			lines = append(lines, i+1)
		}
	}
	return lines
}

func isInlineSpace(c byte) bool { return c == ' ' || c == '\t' }

// --- values ---

func isCollection(v any) bool {
	if _, isMap := wantMapping(v); isMap {
		return true
	}
	_, isList := wantList(v)
	return isList
}

// wantMapping views a value as a mapping when it is one.
func wantMapping(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case kongfig.ConfigData:
		return m, true
	case map[string]any:
		return m, true
	default:
		return nil, false
	}
}

// wantList views a value as a list when it is one. A string is not a list.
func wantList(v any) ([]any, bool) {
	switch s := v.(type) {
	case []any:
		return s, true
	case string, nil:
		return nil, false
	default:
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			out := make([]any, rv.Len())
			for i := range out {
				out[i] = rv.Index(i).Interface()
			}
			return out, true
		}
		return nil, false
	}
}

// sortedKeys walks a mapping in a fixed order, so what a refused edit reports
// does not depend on Go's map order.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func joinIndex(prefix string, i int) string {
	return joinPath(prefix, strconv.Itoa(i))
}
