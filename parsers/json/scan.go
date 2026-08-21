package json

import (
	"bytes"
	"encoding/json"
)

// This file reads a JSON document as text rather than as data: every value keeps
// the byte range it was written in. That is what lets an edit replace one value
// and leave the rest of the file exactly as its author wrote it — the key order,
// the indentation, the way a number is spelled, and, in a JSONC document, the
// comments.

// span is a half-open byte range of the document.
type span struct {
	start int
	end   int
}

func (s span) empty() bool { return s.end <= s.start }

// nodeKind says what shape a value is written in, which decides what an edit of
// it can do: an object takes a new member, an array takes a new element, and a
// scalar is replaced whole.
type nodeKind uint8

const (
	nodeScalar nodeKind = iota
	nodeObject
	nodeArray
)

// docNode is one written value: the text it occupies and, for a container, what
// the document put inside it and where something new can go.
type docNode struct {
	children  map[string]*docNode
	indent    string
	order     []string
	elems     []*docNode
	value     span
	del       span
	hollow    span
	kind      nodeKind
	multiline bool
}

// items reports how many members or elements the container holds.
func (n *docNode) items() int { return len(n.order) + len(n.elems) }

// scanner walks the bytes of a document, in JSONC mode stepping over comments as
// if they were space.
type scanner struct {
	step     string
	src      []byte
	pos      int
	comments bool
}

// scanDocument reads src into a tree that holds the byte range of every value.
// The document has to hold an object: a document with no keys in it has nothing
// an edit could be about.
func scanDocument(src []byte, p Parser) (*docNode, error) {
	s := &scanner{src: src, step: p.indent(), comments: p.Comments}
	root, err := s.value("", !p.Compact)
	if err != nil {
		return nil, err
	}
	s.skipSpace()
	if s.pos < len(s.src) {
		return nil, cannotEdit("", "the document holds more than one value")
	}
	if root.kind != nodeObject {
		return nil, cannotEdit("", "the document does not hold an object")
	}
	return root, nil
}

// value reads one value. parentIndent and parentMultiline describe the container
// it sits in, which is the layout it takes when it has none of its own.
func (s *scanner) value(parentIndent string, parentMultiline bool) (*docNode, error) {
	s.skipSpace()
	if s.pos >= len(s.src) {
		return nil, cannotEdit("", "the document ends where a value should be")
	}
	switch s.src[s.pos] {
	case '{':
		return s.object(parentIndent, parentMultiline)
	case '[':
		return s.array(parentIndent, parentMultiline)
	default:
		start := s.pos
		if err := s.scalar(); err != nil {
			return nil, err
		}
		return &docNode{
			kind:      nodeScalar,
			value:     span{start: start, end: s.pos},
			multiline: parentMultiline,
			indent:    parentIndent,
		}, nil
	}
}

func (s *scanner) object(parentIndent string, parentMultiline bool) (*docNode, error) {
	node := &docNode{kind: nodeObject, children: map[string]*docNode{}}
	open := s.pos
	s.pos++ // '{'
	s.skipSpace()
	s.layout(node, open, parentIndent, parentMultiline)
	for s.pos < len(s.src) && s.src[s.pos] != '}' {
		keyStart := s.pos
		key, err := s.stringLiteral()
		if err != nil {
			return nil, err
		}
		s.skipSpace()
		if s.pos >= len(s.src) || s.src[s.pos] != ':' {
			return nil, cannotEdit("", "a key with no value after it")
		}
		s.pos++
		child, err := s.value(lineIndent(s.src, keyStart), node.multiline)
		if err != nil {
			return nil, err
		}
		child.del = span{start: keyStart, end: child.value.end}
		if _, twice := node.children[key]; !twice {
			node.order = append(node.order, key)
		}
		node.children[key] = child
		if err := s.separator('}'); err != nil {
			return nil, err
		}
	}
	if s.pos >= len(s.src) {
		return nil, cannotEdit("", "an object with no closing brace")
	}
	s.pos++ // '}'
	node.value = span{start: open, end: s.pos}
	return node, nil
}

func (s *scanner) array(parentIndent string, parentMultiline bool) (*docNode, error) {
	node := &docNode{kind: nodeArray}
	open := s.pos
	s.pos++ // '['
	s.skipSpace()
	s.layout(node, open, parentIndent, parentMultiline)
	for s.pos < len(s.src) && s.src[s.pos] != ']' {
		elem, err := s.value(node.indent, node.multiline)
		if err != nil {
			return nil, err
		}
		elem.del = elem.value
		node.elems = append(node.elems, elem)
		if err := s.separator(']'); err != nil {
			return nil, err
		}
	}
	if s.pos >= len(s.src) {
		return nil, cannotEdit("", "an array with no closing bracket")
	}
	s.pos++ // ']'
	node.value = span{start: open, end: s.pos}
	return node, nil
}

// layout works out how a container is laid out, from the text between its opening
// delimiter and its first member: on lines of its own, or all on one line. A
// container the document left empty has no layout of its own, so it takes the one
// of the container it sits in and its members would sit one level deeper.
//
// It runs before the members are read, because each of them takes the layout of
// the container as the layout it sits in.
func (s *scanner) layout(node *docNode, open int, parentIndent string, parentMultiline bool) {
	if s.pos < len(s.src) && (s.src[s.pos] == '}' || s.src[s.pos] == ']') {
		node.multiline = parentMultiline
		node.indent = parentIndent + s.step
		node.hollow = span{start: open + 1, end: s.pos}
		return
	}
	node.multiline = bytes.IndexByte(s.src[open+1:s.pos], '\n') >= 0
	if node.multiline {
		node.indent = lineIndent(s.src, s.pos)
		return
	}
	node.indent = parentIndent
}

// separator steps over the comma between two members, or stops at the closing
// delimiter.
func (s *scanner) separator(closer byte) error {
	s.skipSpace()
	if s.pos >= len(s.src) {
		return cannotEdit("", "the document ends inside a container")
	}
	if s.src[s.pos] == ',' {
		s.pos++
		s.skipSpace()
		return nil
	}
	if s.src[s.pos] == closer {
		return nil
	}
	return cannotEdit("", "two values with no comma between them")
}

// scalar steps over a value the document writes as one token: a string, a number,
// or true, false or null.
func (s *scanner) scalar() error {
	if s.src[s.pos] == '"' {
		_, err := s.stringLiteral()
		return err
	}
	start := s.pos
	for s.pos < len(s.src) && !isValueEnd(s.src[s.pos]) {
		s.pos++
	}
	if s.pos == start {
		return cannotEdit("", "text where a value should be")
	}
	return nil
}

// stringLiteral steps over a quoted string and returns what it spells, so a key
// written with escapes matches the key the parse gave the data.
func (s *scanner) stringLiteral() (string, error) {
	if s.pos >= len(s.src) || s.src[s.pos] != '"' {
		return "", cannotEdit("", "a key that is not a string")
	}
	start := s.pos
	s.pos++
	for s.pos < len(s.src) {
		switch s.src[s.pos] {
		case '\\':
			s.pos += 2
			continue
		case '"':
			s.pos++
			var out string
			if err := json.Unmarshal(s.src[start:s.pos], &out); err != nil {
				return "", cannotEdit("", "a string the document does not spell as one")
			}
			return out, nil
		}
		s.pos++
	}
	return "", cannotEdit("", "a string with no closing quote")
}

// skipSpace steps over whitespace, and over comments when the parser reads them:
// a comment is text no data change is about, so the editor treats it as space it
// walks past and never as text it writes over.
func (s *scanner) skipSpace() {
	for s.pos < len(s.src) {
		switch {
		case isSpace(s.src[s.pos]):
			s.pos++
		case !s.comments || s.pos+1 >= len(s.src) || s.src[s.pos] != '/':
			return
		case s.src[s.pos+1] == '/':
			s.pos = skipLineComment(s.src, s.pos)
		case s.src[s.pos+1] == '*':
			s.pos = skipBlockComment(s.src, s.pos)
		default:
			return
		}
	}
}

// isValueEnd reports whether c ends the text of an untyped scalar.
func isValueEnd(c byte) bool {
	switch c {
	case ',', '}', ']', '/':
		return true
	default:
		return isSpace(c)
	}
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func isInlineSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' }

func lineStartOf(src []byte, at int) int {
	for i := at - 1; i >= 0; i-- {
		if src[i] == '\n' {
			return i + 1
		}
	}
	return 0
}

// lineEndOf is the offset of the newline that ends the line at, sits on, or the
// end of the document when the last line has none.
func lineEndOf(src []byte, at int) int {
	for i := at; i < len(src); i++ {
		if src[i] == '\n' {
			return i
		}
	}
	return len(src)
}

// lineIndent is the whitespace the line at at starts with, which is the
// indentation anything written on a line of its own there has to carry.
func lineIndent(src []byte, at int) string {
	start := lineStartOf(src, at)
	end := start
	for end < len(src) && isInlineSpace(src[end]) {
		end++
	}
	return string(src[start:end])
}
