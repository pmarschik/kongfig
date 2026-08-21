package toml

import (
	"errors"
	"fmt"
	"slices"
	"strconv"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/internal/editpatch"
	"github.com/pmarschik/kongfig/internal/editsplice"
)

// EditDocument rewrites src so it holds want, touching only the text the data
// change touches: comments, key order, indentation and the layout choices the
// author made stay as they are. It implements [kongfig.DocumentEditor]; call it
// through [kongfig.EditDocument] to have the result checked against want.
//
// want is the whole document, not a patch — a key it does not mention is a key
// the rewrite removes.
//
// A change TOML cannot express as an edit of *this* document is refused rather
// than guessed at, and the document is left alone. What it refuses:
//
//   - a new key whose value needs a section the document does not have: a table,
//     or a list of tables too big for the one-line form [Parser.Marshal] writes
//     small ones in
//   - a new element for a list of tables written as [[header]] blocks
//   - a new key in a table the document only names in a dotted key
//
// The caller can fall back to [Parser.Marshal] and accept a reformatted file.
func (p Parser) EditDocument(src []byte, want kongfig.ConfigData) ([]byte, error) {
	e, err := p.editor(src, want)
	if err != nil {
		return nil, err
	}
	return e.apply()
}

// PatchDocument reports the edits [Parser.EditDocument] would make instead of
// making them. It implements [kongfig.DocumentPatcher]; call it through
// [kongfig.PatchDocument] to have the edits checked against want.
//
// Applying the patch to src gives what EditDocument returns, so a caller can show
// the change — a diff, the lines it touches — before it writes anything. It
// refuses what EditDocument refuses, for the same reasons.
func (p Parser) PatchDocument(src []byte, want kongfig.ConfigData) (kongfig.DocumentPatch, error) {
	e, err := p.editor(src, want)
	if err != nil {
		return kongfig.DocumentPatch{}, err
	}
	return e.patch()
}

// editor walks the document and collects the edits want implies, which is the
// work both EditDocument and PatchDocument do before they part ways.
func (p Parser) editor(src []byte, want kongfig.ConfigData) (*docEditor, error) {
	got, err := p.Unmarshal(src)
	if err != nil {
		return nil, fmt.Errorf("toml: read the document to edit: %w", err)
	}
	root, err := scanDocument(src)
	if err != nil {
		return nil, err
	}
	e := &docEditor{src: src, opts: p.writeOpts()}
	if err := e.table(root, got, want, ""); err != nil {
		return nil, err
	}
	return e, nil
}

// ErrCannotEdit reports a change TOML cannot express as an edit of the document
// it was handed, so the document was left alone.
var ErrCannotEdit = errors.New("toml: cannot express that as an edit of this document")

func cannotEdit(path, why string) error {
	if path == "" {
		path = "the document root"
	}
	return fmt.Errorf("%w: %s: %s", ErrCannotEdit, path, why)
}

// docEditor collects the edits a data change implies and splices them in at the
// end, so a refusal anywhere leaves the document untouched.
type docEditor struct {
	src   []byte
	edits []editsplice.Edit
	opts  tomlRenderOpts
}

func (e *docEditor) replace(at span, text string) {
	e.edits = append(e.edits, editsplice.Edit{Start: at.start, End: at.end, Text: text})
}

func (e *docEditor) insert(at int, text string) {
	e.edits = append(e.edits, editsplice.Edit{Start: at, End: at, Text: text})
}

func (e *docEditor) remove(at span) {
	e.edits = append(e.edits, editsplice.Edit{Start: at.start, End: at.end})
}

// apply splices the collected edits into the document in one pass.
func (e *docEditor) apply() ([]byte, error) {
	out, err := editsplice.Apply(e.src, e.edits)
	if err != nil {
		// Two edits over the same bytes would each rewrite what the other
		// read; refusing is the only safe answer.
		return nil, fmt.Errorf("%w: %w", ErrCannotEdit, err)
	}
	return out, nil
}

// patch hands the collected edits over instead of the document they make, in the
// order a caller reading the document alongside them needs.
func (e *docEditor) patch() (kongfig.DocumentPatch, error) {
	patch, err := editpatch.From(e.edits)
	if err != nil {
		return kongfig.DocumentPatch{}, fmt.Errorf("%w: %w", ErrCannotEdit, err)
	}
	return patch, nil
}

// value edits the text of one value so it holds want. It is the entry point for
// every node: unchanged values are left alone, which is what keeps the spelling
// the author chose.
func (e *docEditor) value(node *docNode, got, want any, path string) error {
	if kongfig.EqualValues(got, want) {
		return nil
	}
	if wm, ok := wantTable(want); ok {
		return e.table(node, got, wm, path)
	}
	if ws, ok := wantList(want); ok {
		return e.list(node, got, ws, path)
	}
	return e.rewrite(node, want, path)
}

// rewrite replaces a whole value with the text for want, which only works for a
// value the document spells out in place — a [header] block has no such text.
func (e *docEditor) rewrite(node *docNode, want any, path string) error {
	if node == nil || node.value.empty() {
		return cannotEdit(path, "there is no value to rewrite")
	}
	e.replace(node.value, tomlValue(want))
	return nil
}

// table edits a table key by key: values that changed, keys want added, keys want
// dropped. Keys it does not mention that the document has are removed, since want
// is the whole document.
func (e *docEditor) table(node *docNode, got any, want map[string]any, path string) error {
	if node == nil || node.kind != nodeTable {
		return e.rewrite(node, want, path)
	}
	gotMap, _ := wantTable(got)
	for _, key := range sortedKeys(want) {
		child, ok := node.children[key]
		if !ok {
			if err := e.insertKey(node, key, want[key], path); err != nil {
				return err
			}
			continue
		}
		if err := e.value(child, gotMap[key], want[key], joinPath(path, key)); err != nil {
			return err
		}
	}
	for _, key := range node.order {
		if _, wanted := want[key]; wanted {
			continue
		}
		if err := e.removeChild(node, key, path); err != nil {
			return err
		}
	}
	return nil
}

// insertKey writes a key the document does not have yet, in the layout of the
// table it joins: on a line of its own after the keys already there, or before
// the closing brace of an inline table.
func (e *docEditor) insertKey(node *docNode, key string, value any, path string) error {
	at := joinPath(path, key)
	if !node.canInsert {
		return cannotEdit(at, "there is no place in the document to write it")
	}
	if node.inline {
		text := tomlKey(key) + " = " + tomlValue(value)
		if len(node.order) > 0 {
			text = ", " + text
		}
		e.insert(node.insertAt, text)
		return nil
	}
	if needsSection(value) && !e.writesOnOneLine(value, at) {
		return cannotEdit(at, "its value needs a section the document does not have")
	}
	e.insert(node.insertAt, node.indent+tomlKey(key)+" = "+tomlValue(value)+"\n")
	return nil
}

// writesOnOneLine reports whether a value [needsSection] rejected has a
// key/value form after all. A list of tables does: the block form is the verbose
// one, so [Parser.Marshal] writes small elements as inline tables on one line,
// and refusing an edit the same parser would happily write is a refusal for
// nothing. A table does not — a section is what Marshal gives it, and collapsing
// it onto a line would be a layout the author never chose.
func (e *docEditor) writesOnOneLine(value any, path string) bool {
	list, isList := wantList(value)
	if !isList || len(list) == 0 {
		return false
	}
	elems := make([]any, 0, len(list))
	for _, elem := range list {
		table, isTable := wantTable(elem)
		if !isTable {
			return false
		}
		elems = append(elems, kongfig.ToConfigData(table))
	}
	// Depth is only read for the width comparison, which the write path leaves
	// out: what is written must not depend on the terminal that wrote it.
	return tableArrayInlines(elems, path, 0, e.opts)
}

// removeChild takes out the text that writes one key: its line, its section, or
// its pair inside an inline table.
func (e *docEditor) removeChild(node *docNode, key, path string) error {
	child := node.children[key]
	if child == nil || child.del.empty() {
		return cannotEdit(joinPath(path, key), "there is no text to remove")
	}
	at := child.del
	if node.inline {
		at = e.withSeparator(at)
	}
	e.remove(at)
	return nil
}

// wholeLines widens a span to the lines it sits on, for text that has a line to
// itself.
func (e *docEditor) wholeLines(at span) span {
	start := lineStartOf(e.src, at.start)
	end := at.end
	for end < len(e.src) && e.src[end] != '\n' {
		end++
	}
	if end < len(e.src) {
		end++
	}
	return span{start: start, end: end}
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

// needsSection reports whether a value can only be written as a section — a
// table, or a list with a table in it. Written as a new key line it would change
// the shape of the document rather than a value in it.
func needsSection(v any) bool {
	if _, isTable := wantTable(v); isTable {
		return true
	}
	list, isList := wantList(v)
	if !isList {
		return false
	}
	for _, elem := range list {
		if _, isTable := wantTable(elem); isTable {
			return true
		}
	}
	return false
}

// wantTable views a value as a table when it is one.
func wantTable(v any) (map[string]any, bool) {
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
	if _, isString := v.(string); isString {
		return nil, false
	}
	switch s := v.(type) {
	case []any:
		return s, true
	case nil:
		return nil, false
	default:
		if list := toTOMLSlice(v); list != nil {
			return list, true
		}
		return nil, false
	}
}

// sortedKeys walks a table in a fixed order, so what a refused edit reports does
// not depend on Go's map order.
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
