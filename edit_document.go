package kongfig

import (
	"errors"
	"fmt"
	"strconv"
)

// DocumentEditor is an optional interface a [Parser] can implement to rewrite a
// document in place: the caller edits the parsed data, and the parser changes
// only the text that data change touches, leaving comments, key order and hand
// layout alone.
//
// want is the whole document, not a patch — a key it does not mention is a key
// the rewrite removes. An implementation errors for a change it cannot express
// as an edit of this document (a new key that would need a section the document
// does not have, say) rather than guessing; the caller can fall back to
// [Parser.Marshal] and accept a reformatted file.
//
// Implementations are called through [EditDocument], which verifies the result
// before it reaches the caller. Call the method directly only to skip that check.
type DocumentEditor interface {
	EditDocument(src []byte, want ConfigData) ([]byte, error)
}

var (
	// ErrNoDocumentEditor reports that a [Parser] has no in-place editor, so
	// there is no way to rewrite a document of its format without reformatting
	// it. A caller with no layout to protect falls back to [Parser.Marshal].
	ErrNoDocumentEditor = errors.New("kongfig: parser cannot edit a document in place")

	// ErrEditNotVerified reports that a rewritten document did not hold the data
	// it was asked to hold. The bytes are dropped: the caller never gets a file
	// to write from a rewrite that went wrong.
	ErrEditNotVerified = errors.New("kongfig: edited document does not hold the requested data")
)

// EditDocument rewrites src so it holds want, changing only the text that data
// change touches, and returns the new document. It parses the result and compares
// it against want before returning, so a botched rewrite is an error rather than
// a file the caller has already written.
//
// want is the whole document: a key it does not mention is removed. Errors:
//
//   - [ErrNoDocumentEditor] when p is not a [DocumentEditor] — not a problem with
//     the data, and the caller may fall back to [Parser.Marshal].
//   - the editor's own error for a change the format cannot express as an edit.
//   - [ErrEditNotVerified] when the rewritten document parses to something else.
//
// Nothing here touches the filesystem; the caller decides whether to write the
// bytes.
func EditDocument(p Parser, src []byte, want ConfigData) ([]byte, error) {
	editor, ok := p.(DocumentEditor)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrNoDocumentEditor, p)
	}
	out, err := editor.EditDocument(src, want)
	if err != nil {
		return nil, fmt.Errorf("kongfig: edit document: %w", err)
	}
	if err := verifyDocument(p, out, want); err != nil {
		return nil, err
	}
	return out, nil
}

// verifyDocument reads an edited document back and checks that it holds want.
// Both [EditDocument] and [PatchDocument] go through it: a patch is the edit
// before it is written, so it earns the same check.
func verifyDocument(p Parser, out []byte, want ConfigData) error {
	got, err := p.Unmarshal(out)
	if err != nil {
		return fmt.Errorf("%w: the result does not parse: %w", ErrEditNotVerified, err)
	}
	if EqualConfigData(got, want) {
		return nil
	}
	if path, differs := firstDifference(got, want, ""); differs {
		return fmt.Errorf("%w: at %s", ErrEditNotVerified, path)
	}
	return ErrEditNotVerified
}

// firstDifference names a path where got and want disagree, so a failed check
// says where to look instead of only that something is off. Which path it names
// when several differ is not fixed — a map is walked in the order Go hands its
// keys over.
func firstDifference(got, want any, prefix string) (string, bool) {
	if EqualValues(got, want) {
		return "", false
	}
	gm, gotIsMap := asStringMap(unwrapRenderedValue(got))
	wm, wantIsMap := asStringMap(unwrapRenderedValue(want))
	if gotIsMap && wantIsMap {
		return firstMapDifference(gm, wm, prefix)
	}
	gs, gotIsSlice := asAnySlice(unwrapRenderedValue(got))
	ws, wantIsSlice := asAnySlice(unwrapRenderedValue(want))
	if gotIsSlice && wantIsSlice && len(gs) == len(ws) {
		for i := range gs {
			if path, differs := firstDifference(gs[i], ws[i], joinIndex(prefix, i)); differs {
				return path, true
			}
		}
	}
	return pathOrRoot(prefix), true
}

func firstMapDifference(got, want map[string]any, prefix string) (string, bool) {
	for key, wv := range want {
		gv, present := got[key]
		if !present {
			return joinPath(prefix, key) + " (missing)", true
		}
		if path, differs := firstDifference(gv, wv, joinPath(prefix, key)); differs {
			return path, true
		}
	}
	for key := range got {
		if _, wanted := want[key]; !wanted {
			return joinPath(prefix, key) + " (unwanted)", true
		}
	}
	return pathOrRoot(prefix), true
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

func pathOrRoot(prefix string) string {
	if prefix == "" {
		return "the document root"
	}
	return prefix
}
