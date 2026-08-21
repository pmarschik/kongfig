package kongfig

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Edit is one change to the data of a document, named by the path it changes
// rather than by the text it rewrites. [Set], [Unset], [Append] and [RemoveAt]
// build one; [Apply] folds them into a document.
//
// The whole-document form that [DocumentEditor] takes is the primitive under
// all of this — it is what makes the parse-back check possible — but it is a
// sharp caller surface: a map that forgets a key is a map that removes it. An
// edit says what changes and leaves the rest of the data alone.
type Edit func(ConfigData) error

var (
	// ErrNoSuchPath reports that a path an edit named holds no value. An edit
	// that removes or reads something says so rather than doing nothing: a path
	// with a typo in it would otherwise write a file that lost a key.
	ErrNoSuchPath = errors.New("kongfig: no value at that path")

	// ErrPathNotMapping reports that a path goes through, or ends at, a value
	// that is not a mapping — a scalar cannot hold a key.
	ErrPathNotMapping = errors.New("kongfig: value at that path is not a mapping")

	// ErrPathNotList reports that a path an [Append] or a [RemoveAt] named holds
	// something other than a list.
	ErrPathNotList = errors.New("kongfig: value at that path is not a list")
)

// Set writes v at path, building the mappings on the way down that the data
// does not have yet. A path that ends in a bracket index replaces that element
// of the list, which must already be there.
//
// Maps and slices in v are normalized the way [ToConfigData] normalizes a
// parse, so a later edit walks into the same shapes a parse produces.
func Set(path string, v any) Edit {
	return func(d ConfigData) error {
		at, err := slotAt(d, path, true)
		if err != nil {
			return err
		}
		return at.write(normalizeAny(v))
	}
}

// Unset removes the value at path. The path must be there: a path that is not
// is [ErrNoSuchPath] rather than a silent no-op. Use [RemoveAt] for an element
// of a list.
func Unset(path string) Edit {
	return func(d ConfigData) error {
		at, err := slotAt(d, path, false)
		if err != nil {
			return err
		}
		if _, present := at.read(); !present {
			return pathError(ErrNoSuchPath, path)
		}
		return at.drop()
	}
}

// Append adds vs to the end of the list at path. The path must hold a list.
func Append(path string, vs ...any) Edit {
	return func(d ConfigData) error {
		at, list, err := listAt(d, path)
		if err != nil {
			return err
		}
		out := slices.Clone(list)
		for _, v := range vs {
			out = append(out, normalizeAny(v))
		}
		return at.write(out)
	}
}

// RemoveAt takes element i out of the list at path. An index the list does not
// reach is [ErrNoSuchPath].
func RemoveAt(path string, i int) Edit {
	return func(d ConfigData) error {
		at, list, err := listAt(d, path)
		if err != nil {
			return err
		}
		if i < 0 || i >= len(list) {
			return fmt.Errorf("%w: %s[%d]", ErrNoSuchPath, path, i)
		}
		return at.write(slices.Delete(slices.Clone(list), i, i+1))
	}
}

// ApplyEdits runs the edits over d in order, so a later edit sees what an
// earlier one did, and stops at the first one that fails. What the failed edit
// left behind stays behind: run the edits over data you can throw away, which
// is what [Apply] and [Kongfig.EditLayer] do.
func ApplyEdits(d ConfigData, edits ...Edit) error {
	for _, edit := range edits {
		if err := edit(d); err != nil {
			return err
		}
	}
	return nil
}

// Apply parses src, folds the edits into the data, and rewrites the document
// through [EditDocument] — so the result is checked against the data the same
// way, and only the text the edits touch changes.
//
// It returns the error of the first edit that fails, with no document, and then
// the errors of [EditDocument]: [ErrNoDocumentEditor] for a parser with no
// in-place editor, the parser's own error for a change the format cannot
// express, and [ErrEditNotVerified] for a rewrite that came back holding
// something else.
//
// Nothing here touches the filesystem. Use [Kongfig.EditLayer] instead when the
// document is already loaded as a layer: it takes the data of that layer rather
// than a fresh parse.
func Apply(p Parser, src []byte, edits ...Edit) ([]byte, error) {
	data, err := p.Unmarshal(src)
	if err != nil {
		return nil, fmt.Errorf("kongfig: apply: %w", err)
	}
	if err := ApplyEdits(data, edits...); err != nil {
		return nil, err
	}
	return EditDocument(p, src, data)
}

// slot is the place a path ends: what is there now, how to write something
// else, and how to take it out.
type slot struct {
	read  func() (any, bool)
	write func(any) error
	drop  func() error
}

// segment is one dot-separated step of a path, with the bracket accessor that
// follows it: "dbs[0]" reads the key dbs and then element 0 of that list.
type segment struct {
	key      string
	accessor string
	indexed  bool
}

// pathSegments splits a dot-path into its segments. A bracket that does not
// close is text, not syntax — the same reading [validation] gives it.
func pathSegments(path string) []segment {
	var out []segment
	for part := range strings.SplitSeq(path, ".") {
		open := strings.IndexByte(part, '[')
		shut := strings.LastIndexByte(part, ']')
		if open < 0 || shut <= open {
			out = append(out, segment{key: part})
			continue
		}
		out = append(out, segment{key: part[:open], accessor: part[open+1 : shut], indexed: true})
	}
	return out
}

// slotAt walks path and returns the place its last segment names. build makes
// the walk create the mappings it does not find, which is what [Set] wants and
// what an edit that only reads must not do.
func slotAt(d ConfigData, path string, build bool) (slot, error) {
	segs := pathSegments(path)
	if len(segs) == 0 || segs[0].key == "" {
		return slot{}, fmt.Errorf("%w: %q", ErrNoSuchPath, path)
	}
	holder := d
	for _, seg := range segs[:len(segs)-1] {
		next, err := walkInto(holder, seg, path, build)
		if err != nil {
			return slot{}, err
		}
		holder = next
	}
	return lastSlot(holder, segs[len(segs)-1], path)
}

// walkInto resolves one segment of the way down to a mapping the walk can
// continue from.
func walkInto(m ConfigData, seg segment, path string, build bool) (ConfigData, error) {
	v, present := m[seg.key]
	if !present {
		if !build || seg.indexed {
			return nil, pathError(ErrNoSuchPath, path)
		}
		sub := ConfigData{}
		m[seg.key] = sub
		return sub, nil
	}
	if seg.indexed {
		elem, err := indexInto(v, seg, path)
		if err != nil {
			return nil, err
		}
		v = elem
	}
	sub, ok := v.(ConfigData)
	if !ok {
		return nil, pathError(ErrPathNotMapping, path)
	}
	return sub, nil
}

// indexInto reads the bracket accessor of seg out of v: an integer indexes a
// list, anything else is a key of a mapping.
func indexInto(v any, seg segment, path string) (any, error) {
	i, isIndex := listIndex(seg.accessor)
	if !isIndex {
		sub, ok := v.(ConfigData)
		if !ok {
			return nil, pathError(ErrPathNotMapping, path)
		}
		elem, present := sub[seg.accessor]
		if !present {
			return nil, pathError(ErrNoSuchPath, path)
		}
		return elem, nil
	}
	list, ok := asAnySlice(v)
	if !ok {
		return nil, pathError(ErrPathNotList, path)
	}
	if i < 0 || i >= len(list) {
		return nil, pathError(ErrNoSuchPath, path)
	}
	return list[i], nil
}

// lastSlot builds the slot for the final segment of a path.
func lastSlot(m ConfigData, seg segment, path string) (slot, error) {
	if !seg.indexed {
		return mapSlot(m, seg.key), nil
	}
	v, present := m[seg.key]
	if !present {
		return slot{}, pathError(ErrNoSuchPath, path)
	}
	i, isIndex := listIndex(seg.accessor)
	if !isIndex {
		sub, ok := v.(ConfigData)
		if !ok {
			return slot{}, pathError(ErrPathNotMapping, path)
		}
		return mapSlot(sub, seg.accessor), nil
	}
	list, ok := asAnySlice(v)
	if !ok {
		return slot{}, pathError(ErrPathNotList, path)
	}
	if i < 0 || i >= len(list) {
		return slot{}, pathError(ErrNoSuchPath, path)
	}
	return elemSlot(mapSlot(m, seg.key), list, i, path), nil
}

// mapSlot is a key of a mapping: it can hold anything, and it can go away.
func mapSlot(m ConfigData, key string) slot {
	return slot{
		read: func() (any, bool) {
			v, present := m[key]
			return v, present
		},
		write: func(v any) error {
			m[key] = v
			return nil
		},
		drop: func() error {
			delete(m, key)
			return nil
		},
	}
}

// elemSlot is one element of a list. A write goes back through the slot the
// list itself sits in, because a list is a value: the copy an edit changes is
// not the copy the document holds.
func elemSlot(owner slot, list []any, i int, path string) slot {
	return slot{
		read: func() (any, bool) { return list[i], true },
		write: func(v any) error {
			out := slices.Clone(list)
			out[i] = v
			return owner.write(out)
		},
		drop: func() error {
			return fmt.Errorf("%w: %s names an element of a list; use RemoveAt", ErrPathNotMapping, path)
		},
	}
}

// listAt resolves path to the list it names, and to the slot that list sits in
// so an edit can write the changed list back.
func listAt(d ConfigData, path string) (slot, []any, error) {
	at, err := slotAt(d, path, false)
	if err != nil {
		return slot{}, nil, err
	}
	v, present := at.read()
	if !present {
		return slot{}, nil, pathError(ErrNoSuchPath, path)
	}
	list, ok := asAnySlice(v)
	if !ok {
		return slot{}, nil, pathError(ErrPathNotList, path)
	}
	return at, list, nil
}

// listIndex reads a bracket accessor as a list index. Anything that is not a
// plain integer is a key of a mapping instead.
func listIndex(accessor string) (int, bool) {
	i, err := strconv.Atoi(accessor)
	if err != nil {
		return 0, false
	}
	return i, true
}

func pathError(err error, path string) error {
	return fmt.Errorf("%w: %s", err, path)
}
