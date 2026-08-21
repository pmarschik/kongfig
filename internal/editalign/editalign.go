// Package editalign matches the elements a document writes against the elements
// a caller wants, so an in-place editor can rewrite the element that changed and
// take out or put in a line for the rest.
//
// Pairing element i with element i instead is what makes an editor move
// comments: take the first element out of a three-element list and every
// remaining element is a different value at the same position, so every line
// gets rewritten and the comment beside each one ends up beside its neighbour's
// value. Matching the elements that did not change first leaves their text — and
// their comments — alone.
package editalign

import "slices"

// Kind is what an editor does with one element.
type Kind uint8

const (
	// Rewrite replaces the text of the document's element Doc with the wanted
	// element Want, in place, keeping whatever else is on that line.
	Rewrite Kind = iota
	// Delete takes the document's element Doc out.
	Delete
	// Insert puts the wanted element Want in before the document's element Doc.
	// Doc is the element count when it goes in after the last one.
	Insert
)

// Op is one step of the script that turns a document's elements into the wanted
// ones. Elements that did not change get no step at all.
type Op struct {
	Kind Kind
	Doc  int
	Want int
}

// maxCells bounds the alignment table. A list long enough to overrun it is
// paired by position: the cost of the table grows with the product of the two
// lengths, and a config file with thousands of list elements is not a file whose
// comments an editor can protect anyway.
const maxCells = 1 << 16

// Elements returns the steps that turn doc into want, in ascending Doc order.
// equal decides which elements are the same element — for config data that is a
// deep comparison, so an unchanged table in a list of tables matches itself.
func Elements(doc, want []any, equal func(a, b any) bool) []Op {
	if (len(doc)+1)*(len(want)+1) > maxCells {
		return Positional(len(doc), len(want))
	}
	return pair(walk(doc, want, equal), len(doc))
}

// Positional pairs element i with element i, which is all an aligner that cannot
// afford to look for identities can do. It is also the honest answer when the
// document and the parsed data disagree about how many elements there are.
func Positional(doc, want int) []Op {
	ops := make([]Op, 0, max(doc, want))
	for i := range min(doc, want) {
		ops = append(ops, Op{Kind: Rewrite, Doc: i, Want: i})
	}
	for i := want; i < doc; i++ {
		ops = append(ops, Op{Kind: Delete, Doc: i})
	}
	for j := doc; j < want; j++ {
		ops = append(ops, Op{Kind: Insert, Doc: doc, Want: j})
	}
	return ops
}

// stepKind is what the raw script says about one element. It is not a [Kind]:
// the script has no rewrite in it — pairing a loss with a gain is what produces
// one — and it has a step for an element both sides keep, which an editor does
// nothing about but the script needs to know where a block of changes ends.
type stepKind uint8

const (
	stepKeep stepKind = iota
	stepDelete
	stepInsert
)

// step is one entry of the raw script: an element the document keeps, one it
// loses, or one it gains.
type step struct {
	kind stepKind
	doc  int
	want int
}

// walk reads the longest common subsequence of doc and want into a script of
// keeps, deletes and inserts, in document order.
func walk(doc, want []any, equal func(a, b any) bool) []step {
	// common[i][j] is the length of the longest common subsequence of doc[i:]
	// and want[j:], filled from the back so the walk below can read it forward.
	common := make([][]int, len(doc)+1)
	for i := range common {
		common[i] = make([]int, len(want)+1)
	}
	for i, d := range slices.Backward(doc) {
		for j := len(want) - 1; j >= 0; j-- {
			if equal(d, want[j]) {
				common[i][j] = common[i+1][j+1] + 1
				continue
			}
			common[i][j] = max(common[i+1][j], common[i][j+1])
		}
	}
	var script []step
	i, j := 0, 0
	for i < len(doc) && j < len(want) {
		switch {
		case equal(doc[i], want[j]):
			script = append(script, step{kind: stepKeep, doc: i, want: j})
			i++
			j++
		case common[i+1][j] >= common[i][j+1]:
			script = append(script, step{kind: stepDelete, doc: i})
			i++
		default:
			script = append(script, step{kind: stepInsert, want: j})
			j++
		}
	}
	for ; i < len(doc); i++ {
		script = append(script, step{kind: stepDelete, doc: i})
	}
	for ; j < len(want); j++ {
		script = append(script, step{kind: stepInsert, want: j})
	}
	return script
}

// pair turns the raw script into ops, reading each stretch of changes between
// two kept elements as a block: what the block loses and what it gains are
// paired off into rewrites, so a changed value is rewritten where it is written
// rather than deleted and written again somewhere else. What is left over is a
// plain delete or a plain insert.
func pair(script []step, docLen int) []Op {
	var ops []Op
	var dels, inss []int
	flush := func(anchor int) {
		for k := range min(len(dels), len(inss)) {
			ops = append(ops, Op{Kind: Rewrite, Doc: dels[k], Want: inss[k]})
		}
		for _, at := range dels[min(len(dels), len(inss)):] {
			ops = append(ops, Op{Kind: Delete, Doc: at})
		}
		for _, at := range inss[min(len(dels), len(inss)):] {
			ops = append(ops, Op{Kind: Insert, Doc: anchor, Want: at})
		}
		dels, inss = nil, nil
	}
	for _, s := range script {
		switch s.kind {
		case stepKeep:
			flush(s.doc)
		case stepDelete:
			dels = append(dels, s.doc)
		case stepInsert:
			inss = append(inss, s.want)
		}
	}
	flush(docLen)
	return ops
}
