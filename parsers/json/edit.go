package json

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/internal/editpatch"
	"github.com/pmarschik/kongfig/internal/editsplice"
)

// This file edits a document instead of rewriting it: the scan says which value
// is written where, and only the bytes a data change touches are replaced. Key
// order, indentation, the spelling of a number and — in a JSONC document — the
// comments all survive, because nothing else is rewritten.

// ErrCannotEdit reports a change JSON cannot express as an edit of the document
// it was handed, so the document was left alone.
var ErrCannotEdit = errors.New("json: cannot express that as an edit of this document")

// EditDocument rewrites src so it holds want, touching only the text the data
// change touches. It implements [kongfig.DocumentEditor]; call it through
// [kongfig.EditDocument] to have the result checked against want.
//
// want is the whole document, not a patch — a key it does not mention is a key
// the rewrite removes. A new member or element joins the layout of the container
// it goes into: on a line of its own in a document written across lines, on the
// line in a document written on one.
//
// Nearly every change JSON can hold is a change it can express in place, so a
// refusal is rare: a document that holds no object has no keys to edit, and a
// value JSON has no text for — a NaN, an infinity — cannot be written. The caller
// can fall back to [Parser.Marshal] and accept a reformatted file.
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
		return nil, fmt.Errorf("json: read the document to edit: %w", err)
	}
	root, err := scanDocument(src, p)
	if err != nil {
		return nil, err
	}
	e := &docEditor{src: src, step: p.indent(), compact: p.Compact}
	if err := e.object(root, got, want, ""); err != nil {
		return nil, err
	}
	return e, nil
}

func cannotEdit(path, why string) error {
	if path == "" {
		path = "the document root"
	}
	return fmt.Errorf("%w: %s: %s", ErrCannotEdit, path, why)
}

// docEditor collects the edits a data change implies and splices them in at the
// end, so a refusal anywhere leaves the document untouched.
type docEditor struct {
	src     []byte
	step    string
	edits   []editsplice.Edit
	compact bool
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

// value edits one value so it holds want. A value that did not change is left
// alone, which is what keeps the spelling its author chose.
func (e *docEditor) value(node *docNode, got, want any, path string) error {
	if kongfig.EqualValues(got, want) {
		return nil
	}
	if wm, ok := wantObject(want); ok {
		return e.object(node, got, wm, path)
	}
	if ws, ok := wantList(want); ok {
		return e.list(node, got, ws, path)
	}
	return e.rewrite(node, want, path)
}

// rewrite replaces a whole value with the text for want.
func (e *docEditor) rewrite(node *docNode, want any, path string) error {
	if node == nil || node.value.empty() {
		return cannotEdit(path, "there is no value to rewrite")
	}
	text, err := e.text(want, lineIndent(e.src, node.value.start), node.multiline, path)
	if err != nil {
		return err
	}
	e.replace(node.value, text)
	return nil
}

// object edits an object key by key: the values that changed, the keys want adds,
// the keys want drops. A key the document has that want does not mention is
// removed, since want is the whole document.
//
// The keys it adds go in before the keys it drops go out. Both can land on the
// same comma — the one after the last key that stays — and the splice takes the
// edits at one offset in the order they were collected, so the comma that joins
// the new key has to be written before the comma that held the old one is taken
// away.
func (e *docEditor) object(node *docNode, got any, want map[string]any, path string) error {
	if node == nil || node.kind != nodeObject {
		return e.rewrite(node, want, path)
	}
	gotMap, _ := wantObject(got)
	var add []string
	for _, key := range sortedKeys(want) {
		child, ok := node.children[key]
		if !ok {
			add = append(add, key)
			continue
		}
		if err := e.value(child, gotMap[key], want[key], joinPath(path, key)); err != nil {
			return err
		}
	}
	drop, kept := membersToDrop(node, want)
	texts := make([]string, 0, len(add))
	for _, key := range add {
		text, err := e.text(want[key], node.indent, node.multiline, joinPath(path, key))
		if err != nil {
			return err
		}
		texts = append(texts, e.member(key, text))
	}
	e.appendItems(node, kept, texts)
	return e.removeMembers(node, drop, path)
}

// membersToDrop reads the keys the document has against the keys want has: the
// ones to take out, and the last one that stays, which is where a new key joins
// the object.
func membersToDrop(node *docNode, want map[string]any) (drop []string, kept *docNode) {
	for _, key := range node.order {
		if _, keep := want[key]; keep {
			kept = node.children[key]
			continue
		}
		drop = append(drop, key)
	}
	return drop, kept
}

// removeMembers takes out the text of the keys want does not mention.
func (e *docEditor) removeMembers(node *docNode, drop []string, path string) error {
	if len(drop) == 0 {
		return nil
	}
	last := node.order[len(node.order)-1]
	for _, key := range drop {
		child := node.children[key]
		if err := e.removeItem(node, child.del, key == last, joinPath(path, key)); err != nil {
			return err
		}
	}
	if !slices.Contains(drop, last) {
		return nil
	}
	_, kept := membersToDrop(node, keysOf(node.order, drop))
	e.dropTrailingComma(kept)
	return nil
}

// keysOf turns the keys that stay into a set membersToDrop can read, so the last
// key that stays is found the same way in both places.
func keysOf(order, drop []string) map[string]any {
	stay := make(map[string]any, len(order))
	for _, key := range order {
		if slices.Contains(drop, key) {
			continue
		}
		stay[key] = nil
	}
	return stay
}

// removeItem takes out the text of one member or element. A member that has a
// line to itself goes away with its line, and with the comment written above it,
// which is about that member. One that shares its line with others goes away with
// the comma that separates it from the next.
//
// The last member of a container has no comma after it, so taking it out leaves
// the one before it dangling; [docEditor.dropTrailingComma] is what takes that
// one away.
func (e *docEditor) removeItem(node *docNode, del span, last bool, path string) error {
	if del.empty() {
		return cannotEdit(path, "there is no text to remove")
	}
	if node.multiline && e.ownsItsLine(del) {
		at := e.wholeLines(del)
		at.start = e.itemStart(del)
		e.remove(at)
		return nil
	}
	if last {
		// The comma before it is dropped separately, and that is what takes the
		// space in front of it away; eating the space here as well would put two
		// edits over the same text.
		e.remove(del)
		return nil
	}
	e.remove(span{start: e.sharedLineStart(del.start), end: e.pastSeparator(del.end)})
	return nil
}

// sharedLineStart is where the text of a member that shares its line begins once
// the space that separates it from what comes before it on that line is counted
// as its own. A member that has only indentation before it leaves that
// indentation to whatever follows it on the line.
func (e *docEditor) sharedLineStart(at int) int {
	start := at
	for start > 0 && isInlineSpace(e.src[start-1]) {
		start--
	}
	if start == lineStartOf(e.src, at) {
		return at
	}
	return start
}

// dropTrailingComma takes out the comma after the last member that stays, which
// the member it used to separate has just taken away with it.
func (e *docEditor) dropTrailingComma(kept *docNode) {
	if kept == nil {
		return
	}
	at, ok := e.commaAfter(kept.value.end)
	if !ok {
		return
	}
	at.end = e.pastTrailingSpace(at.end)
	e.remove(at)
}

// appendItems writes new members or elements after the last item that stays, in
// the layout the container already uses. When nothing stays they go in right
// after the opening delimiter and the removals take the rest of the text away;
// when the container was empty to begin with they replace the space inside it.
func (e *docEditor) appendItems(node, kept *docNode, texts []string) {
	if len(texts) == 0 {
		return
	}
	if node.items() == 0 {
		e.replace(node.hollow, e.itemsText(node, texts, true))
		return
	}
	if kept == nil {
		e.insert(node.value.start+1, e.itemsText(node, texts, false))
		return
	}
	at := kept.value.end
	if !node.multiline {
		e.insert(at, e.comma()+strings.Join(texts, e.comma()))
		return
	}
	e.insert(at, ",")
	e.insert(lineEndOf(e.src, at), e.itemsText(node, texts, false))
}

// itemsText lays out new items for a container that has nothing before them.
// closing says the text has to close the container off as well, which is the case
// for a container the document wrote as an empty pair of delimiters.
func (e *docEditor) itemsText(node *docNode, texts []string, closing bool) string {
	if !node.multiline {
		return strings.Join(texts, e.comma())
	}
	text := "\n" + node.indent + strings.Join(texts, ",\n"+node.indent)
	if closing {
		text += "\n" + lineIndent(e.src, node.value.start)
	}
	return text
}

// insertBefore writes new elements in front of an element that stays.
func (e *docEditor) insertBefore(node, before *docNode, texts []string) {
	var text strings.Builder
	if node.multiline && e.ownsItsLine(before.del) {
		for _, t := range texts {
			text.WriteString(node.indent + t + ",\n")
		}
		e.insert(e.itemStart(before.del), text.String())
		return
	}
	for _, t := range texts {
		text.WriteString(t + e.comma())
	}
	e.insert(before.del.start, text.String())
}

// member writes the text of one object member from a key and the text of its
// value.
func (e *docEditor) member(key, value string) string {
	name, err := json.Marshal(key)
	if err != nil {
		// A key is a Go string, which always has JSON text.
		name = []byte(strconv.Quote(key))
	}
	return string(name) + e.colon() + value
}

// text writes a value the way the document would write it: laid out under indent,
// so a new object or array reads like the rest of the file.
func (e *docEditor) text(v any, indent string, multiline bool, path string) (string, error) {
	var (
		b   []byte
		err error
	)
	if multiline && !e.compact {
		b, err = json.MarshalIndent(v, indent, e.step)
	} else {
		b, err = json.Marshal(v)
	}
	if err != nil {
		return "", cannotEdit(path, "JSON has no text for that value: "+err.Error())
	}
	return string(b), nil
}

func (e *docEditor) comma() string {
	if e.compact {
		return ","
	}
	return ", "
}

func (e *docEditor) colon() string {
	if e.compact {
		return ":"
	}
	return ": "
}

// ownsItsLine reports whether a member has its line to itself, which is what lets
// the editor take the whole line away with it. A comment after it is about it, so
// it counts as part of the line; another member does not.
func (e *docEditor) ownsItsLine(at span) bool {
	for i := lineStartOf(e.src, at.start); i < at.start; i++ {
		if !isInlineSpace(e.src[i]) {
			return false
		}
	}
	for i := e.pastSeparator(at.end); i < len(e.src) && e.src[i] != '\n'; i++ {
		if !isInlineSpace(e.src[i]) {
			return e.src[i] == '/'
		}
	}
	return true
}

// wholeLines widens a span to the lines it sits on, for text that has a line to
// itself.
func (e *docEditor) wholeLines(at span) span {
	end := lineEndOf(e.src, at.end)
	if end < len(e.src) {
		end++
	}
	return span{start: lineStartOf(e.src, at.start), end: end}
}

// itemStart is where the text of a member on its own line begins: that line, or
// the first of the comment lines above it. A comment written directly above a
// member is about that member, so it belongs to the member's text — it goes away
// with the member, and a new member goes in above it rather than between the two.
func (e *docEditor) itemStart(at span) int {
	start := lineStartOf(e.src, at.start)
	for start > 0 {
		above := lineStartOf(e.src, start-1)
		if !e.commentLine(above) {
			return start
		}
		start = above
	}
	return start
}

// commentLine reports whether the line at lineStart holds nothing but a comment.
func (e *docEditor) commentLine(lineStart int) bool {
	i := lineStart
	for i < len(e.src) && isInlineSpace(e.src[i]) {
		i++
	}
	return i+1 < len(e.src) && e.src[i] == '/' && (e.src[i+1] == '/' || e.src[i+1] == '*')
}

// commaAfter finds the comma that follows a value.
func (e *docEditor) commaAfter(from int) (span, bool) {
	i := from
	for i < len(e.src) && isInlineSpace(e.src[i]) {
		i++
	}
	if i < len(e.src) && e.src[i] == ',' {
		return span{start: i, end: i + 1}, true
	}
	return span{}, false
}

// pastSeparator is the offset just after the comma that follows a value, and the
// space after it, or the offset itself when no comma follows.
func (e *docEditor) pastSeparator(from int) int {
	at, ok := e.commaAfter(from)
	if !ok {
		return from
	}
	return e.pastTrailingSpace(at.end)
}

// pastTrailingSpace steps over the space after from, which belongs to the text
// that used to follow it and goes away with it. A comment keeps the space in
// front of it, because that space is how it reads.
func (e *docEditor) pastTrailingSpace(from int) int {
	end := from
	for end < len(e.src) && isInlineSpace(e.src[end]) {
		end++
	}
	if end < len(e.src) && e.src[end] == '/' {
		return from
	}
	return end
}

// wantObject views a value as an object when it is one.
func wantObject(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case kongfig.ConfigData:
		return m, true
	case map[string]any:
		return m, true
	default:
		return nil, false
	}
}

// wantList views a value as an array when it is one. A string is not a list.
func wantList(v any) ([]any, bool) {
	if list, ok := v.([]any); ok {
		return list, true
	}
	return nil, false
}

// sortedKeys walks an object in a fixed order, so what a refused edit reports
// does not depend on Go's map order, and so keys added in one edit go in in an
// order that does not change from run to run.
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
