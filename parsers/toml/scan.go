package toml

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// This file reads a TOML document for its shape rather than its data: which
// bytes spell which value, which line a key is written on, and where a new key
// or element can be added. The decoder already produced the data — what it does
// not report is where any of it is written, which is what an in-place edit needs.
//
// It is deliberately shallow. Anything it does not recognize ends the value it is
// reading rather than the scan, and a path it fails to map is an edit the editor
// refuses; a refused edit costs the caller a fallback, one mapped to the wrong
// bytes costs them their file.

// span is a half-open byte range of the document.
type span struct {
	start int
	end   int
}

func (s span) empty() bool { return s.end <= s.start }

// nodeKind says what shape a value is written in, which decides what an edit of
// it can do — a table written as a [header] block takes a new key on a new line,
// one written as { pairs } takes it before the closing brace.
type nodeKind uint8

const (
	nodeScalar nodeKind = iota
	nodeArray
	nodeTable
	nodeTableArray
)

// docNode is one value of the document, with the positions an edit of it needs.
type docNode struct {
	children  map[string]*docNode
	indent    string
	order     []string
	elems     []*docNode
	value     span
	del       span
	insertAt  int
	kind      nodeKind
	multiline bool
	comma     bool
	inline    bool
	canInsert bool
}

func newTable() *docNode {
	return &docNode{kind: nodeTable, children: map[string]*docNode{}}
}

// set attaches a child, remembering the order the document wrote its keys in.
func (n *docNode) set(key string, child *docNode) {
	if n.children == nil {
		n.children = map[string]*docNode{}
	}
	if _, seen := n.children[key]; !seen {
		n.order = append(n.order, key)
	}
	n.children[key] = child
}

// descend walks the segments of a dotted key, creating the tables they imply. A
// table that only exists because a dotted key named it takes no new keys of its
// own: there is no line of the document that is "inside" it.
func (n *docNode) descend(segs []string) (*docNode, error) {
	cur := n
	for _, seg := range segs {
		child, ok := cur.children[seg]
		if !ok {
			child = newTable()
			cur.set(seg, child)
		}
		if child.kind == nodeTableArray {
			if len(child.elems) == 0 {
				return nil, fmt.Errorf("toml: %q has no element to write into", seg)
			}
			child = child.elems[len(child.elems)-1]
		}
		if child.kind != nodeTable {
			return nil, fmt.Errorf("toml: %q is not a table", seg)
		}
		cur = child
	}
	return cur, nil
}

var errUnexpectedEnd = errors.New("toml: document ends inside a value")

// scanDocument reads src and returns its shape as a tree of [docNode].
func scanDocument(src []byte) (*docNode, error) {
	s := &scanner{src: src, root: newTable()}
	s.root.canInsert = true
	s.cur = s.root
	if err := s.run(); err != nil {
		return nil, err
	}
	return s.root, nil
}

type scanner struct {
	root  *docNode
	cur   *docNode // the table the key lines being read belong to
	block *docNode // the [header] block being read, whose span ends at the next header
	src   []byte
	pos   int
}

func (s *scanner) run() error {
	for s.pos < len(s.src) {
		lineStart := s.pos
		s.skipInlineSpace()
		switch {
		case s.pos >= len(s.src):
		case s.src[s.pos] == '\n' || s.src[s.pos] == '\r' || s.src[s.pos] == '#':
			s.pos = s.lineEnd(s.pos)
		case s.src[s.pos] == '[':
			if err := s.scanHeader(lineStart); err != nil {
				return err
			}
		default:
			if err := s.scanKeyLine(lineStart); err != nil {
				return err
			}
		}
	}
	s.closeBlock(len(s.src))
	return nil
}

// closeBlock ends the block being read at end, which is where the next header
// starts — the bytes a deletion of that whole table would take.
func (s *scanner) closeBlock(end int) {
	if s.block != nil {
		s.block.del.end = end
		s.block = nil
	}
}

// scanHeader reads a [table] or [[table array]] header and makes it the table
// the following key lines belong to.
func (s *scanner) scanHeader(lineStart int) error {
	s.closeBlock(lineStart)
	if len(s.root.order) == 0 && s.root.insertAt == 0 {
		// A new root key goes ahead of the first header: after one it would
		// belong to that table instead of to the document.
		s.root.insertAt = lineStart
	}
	isArray := strings.HasPrefix(string(s.src[s.pos:]), "[[")
	brackets := 1
	if isArray {
		brackets = 2
	}
	s.pos += brackets
	nameStart := s.pos
	for s.pos < len(s.src) && s.src[s.pos] != ']' && s.src[s.pos] != '\n' {
		if isQuote(s.src[s.pos]) {
			s.skipString()
			continue
		}
		s.pos++
	}
	if s.pos >= len(s.src) || s.src[s.pos] != ']' {
		return fmt.Errorf("toml: unterminated table header at byte %d", lineStart)
	}
	name := string(s.src[nameStart:s.pos])
	s.pos += brackets

	segs, err := splitKeyPath(name)
	if err != nil {
		return err
	}
	table, err := s.headerTable(segs, isArray)
	if err != nil {
		return err
	}
	end := s.lineEnd(s.pos)
	table.del = span{start: lineStart}
	table.insertAt = end
	table.canInsert = true
	s.cur, s.block, s.pos = table, table, end
	return nil
}

// headerTable resolves a header's path to the table its keys belong to, adding
// an element for a [[table array]] header.
func (s *scanner) headerTable(segs []string, isArray bool) (*docNode, error) {
	parent, err := s.root.descend(segs[:len(segs)-1])
	if err != nil {
		return nil, err
	}
	last := segs[len(segs)-1]
	if !isArray {
		table, ok := parent.children[last]
		if !ok || table.kind != nodeTable {
			table = newTable()
			parent.set(last, table)
		}
		return table, nil
	}
	arr, ok := parent.children[last]
	if !ok || arr.kind != nodeTableArray {
		arr = &docNode{kind: nodeTableArray}
		parent.set(last, arr)
	}
	elem := newTable()
	arr.elems = append(arr.elems, elem)
	return elem, nil
}

// scanKeyLine reads one "key = value" line and records where its value and its
// line are.
func (s *scanner) scanKeyLine(lineStart int) error {
	indent := string(s.src[lineStart:s.pos])
	segs, err := s.scanKeyPath()
	if err != nil {
		return err
	}
	s.skipInlineSpace()
	node, err := s.scanValue()
	if err != nil {
		return err
	}
	end := s.lineEnd(node.value.end)
	node.del = span{start: lineStart, end: end}
	parent, err := s.cur.descend(segs[:len(segs)-1])
	if err != nil {
		return err
	}
	parent.set(segs[len(segs)-1], node)
	// A new key of this table joins it after the last line it already has, and
	// lines up with them.
	s.cur.insertAt = end
	s.cur.indent = indent
	s.pos = end
	return nil
}

// scanKeyPath reads the key of a key/value line, up to and including the "=".
func (s *scanner) scanKeyPath() ([]string, error) {
	start := s.pos
	for s.pos < len(s.src) && s.src[s.pos] != '=' && s.src[s.pos] != '\n' {
		if isQuote(s.src[s.pos]) {
			s.skipString()
			continue
		}
		s.pos++
	}
	if s.pos >= len(s.src) || s.src[s.pos] != '=' {
		return nil, fmt.Errorf("toml: no value on the line at byte %d", start)
	}
	key := string(s.src[start:s.pos])
	s.pos++ // past the "="
	return splitKeyPath(key)
}

// scanValue reads the value at the cursor, whichever shape it is written in.
func (s *scanner) scanValue() (*docNode, error) {
	if s.pos >= len(s.src) {
		return nil, errUnexpectedEnd
	}
	switch s.src[s.pos] {
	case '[':
		return s.scanArray()
	case '{':
		return s.scanInlineTable()
	default:
		return s.scanScalar()
	}
}

// scanScalar reads a value up to whatever ends it — the end of the line, a
// comment, or the comma or bracket of the container it sits in.
func (s *scanner) scanScalar() (*docNode, error) {
	start := s.pos
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		if c == '\n' || c == '#' || c == ',' || c == ']' || c == '}' {
			break
		}
		if isQuote(c) {
			s.skipString()
			continue
		}
		s.pos++
	}
	end := s.pos
	for end > start && isInlineSpace(s.src[end-1]) {
		end--
	}
	if end == start {
		return nil, fmt.Errorf("toml: empty value at byte %d", start)
	}
	return &docNode{kind: nodeScalar, value: span{start: start, end: end}}, nil
}

// scanArray reads an array and each of its elements, and works out where a new
// element joins it: on its own line for an array written across lines, after the
// last element for one written on a single line.
func (s *scanner) scanArray() (*docNode, error) {
	node := &docNode{kind: nodeArray}
	start := s.pos
	s.pos++ // past the "["
	node.insertAt = s.pos
	trailingComma := false
	for {
		crossedLine := s.skipSpaceAndComments()
		node.multiline = node.multiline || crossedLine
		if s.pos >= len(s.src) {
			return nil, errUnexpectedEnd
		}
		if s.src[s.pos] == ']' {
			s.pos++
			break
		}
		elem, err := s.scanValue()
		if err != nil {
			return nil, err
		}
		elem.del = elem.value
		node.elems = append(node.elems, elem)
		node.indent = s.lineIndent(elem.value.start)
		node.insertAt = elem.value.end
		trailingComma = false

		crossedLine = s.skipSpaceAndComments()
		node.multiline = node.multiline || crossedLine
		if s.pos < len(s.src) && s.src[s.pos] == ',' {
			s.pos++
			node.insertAt = s.pos
			trailingComma = true
		}
	}
	node.comma = trailingComma
	node.value = span{start: start, end: s.pos}
	return node, nil
}

// scanInlineTable reads a { pairs } table, where a new key goes before the
// closing brace.
func (s *scanner) scanInlineTable() (*docNode, error) {
	node := newTable()
	node.inline = true
	node.canInsert = true
	start := s.pos
	s.pos++ // past the "{"
	for {
		crossedLine := s.skipSpaceAndComments()
		node.multiline = node.multiline || crossedLine
		if s.pos >= len(s.src) {
			return nil, errUnexpectedEnd
		}
		if s.src[s.pos] == '}' {
			node.insertAt = s.pos
			s.pos++
			break
		}
		pairStart := s.pos
		segs, err := s.scanKeyPath()
		if err != nil {
			return nil, err
		}
		s.skipInlineSpace()
		child, err := s.scanValue()
		if err != nil {
			return nil, err
		}
		child.del = span{start: pairStart, end: child.value.end}
		parent, err := node.descend(segs[:len(segs)-1])
		if err != nil {
			return nil, err
		}
		parent.set(segs[len(segs)-1], child)

		s.skipSpaceAndComments()
		if s.pos < len(s.src) && s.src[s.pos] == ',' {
			s.pos++
		}
	}
	node.value = span{start: start, end: s.pos}
	return node, nil
}

// --- cursor helpers ---

func (s *scanner) skipInlineSpace() {
	for s.pos < len(s.src) && isInlineSpace(s.src[s.pos]) {
		s.pos++
	}
}

// skipSpaceAndComments moves past whitespace, newlines and comments inside a
// container, reporting whether it crossed a line — which is how the scanner
// knows the container is written across lines.
func (s *scanner) skipSpaceAndComments() (crossedLine bool) {
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; {
		case c == '\n':
			crossedLine = true
			s.pos++
		case isInlineSpace(c) || c == '\r':
			s.pos++
		case c == '#':
			s.pos = s.lineEnd(s.pos)
			crossedLine = true
		default:
			return crossedLine
		}
	}
	return crossedLine
}

// skipString moves past a string literal, so the syntax characters inside one
// are read as the text they are.
func (s *scanner) skipString() {
	quote := s.src[s.pos]
	triple := []byte{quote, quote, quote}
	if strings.HasPrefix(string(s.src[s.pos:]), string(triple)) {
		s.pos += 3
		for s.pos < len(s.src) {
			if s.src[s.pos] == '\\' && quote == '"' {
				s.pos += 2
				continue
			}
			if strings.HasPrefix(string(s.src[s.pos:]), string(triple)) {
				s.pos += 3
				return
			}
			s.pos++
		}
		return
	}
	s.pos++
	for s.pos < len(s.src) {
		switch {
		case s.src[s.pos] == '\\' && quote == '"':
			s.pos += 2
		case s.src[s.pos] == quote:
			s.pos++
			return
		case s.src[s.pos] == '\n':
			return // an unterminated string ends with its line
		default:
			s.pos++
		}
	}
}

// lineEnd returns the offset just past the newline that ends the line from is
// on, or the end of the document.
func (s *scanner) lineEnd(from int) int {
	for i := from; i < len(s.src); i++ {
		if s.src[i] == '\n' {
			return i + 1
		}
	}
	return len(s.src)
}

// lineIndent returns the whitespace the line holding at starts with, so an
// inserted element lines up with the ones already there.
func (s *scanner) lineIndent(at int) string {
	start := lineStartOf(s.src, at)
	end := start
	for end < len(s.src) && isInlineSpace(s.src[end]) {
		end++
	}
	return string(s.src[start:end])
}

func lineStartOf(src []byte, at int) int {
	for i := at - 1; i >= 0; i-- {
		if src[i] == '\n' {
			return i + 1
		}
	}
	return 0
}

func isInlineSpace(c byte) bool { return c == ' ' || c == '\t' }

func isQuote(c byte) bool { return c == '"' || c == '\'' }

// splitKeyPath splits a key into its dotted segments, unquoting the ones the
// document wrote in quotes.
func splitKeyPath(key string) ([]string, error) {
	var segs []string
	var cur strings.Builder
	inQuote := byte(0)
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
				continue
			}
			if c == '\\' && inQuote == '"' && i+1 < len(key) {
				i++
				cur.WriteByte(key[i])
				continue
			}
			cur.WriteByte(c)
		case isQuote(c):
			inQuote = c
		case c == '.':
			segs = append(segs, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	segs = append(segs, strings.TrimSpace(cur.String()))
	if slices.Contains(segs, "") {
		return nil, fmt.Errorf("toml: empty key segment in %q", key)
	}
	return segs, nil
}
