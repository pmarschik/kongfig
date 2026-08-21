package kongfig

import (
	"errors"
	"fmt"

	"github.com/pmarschik/kongfig/internal/editsplice"
)

// DocumentEdit replaces the bytes of a document in [Start, End) with Text. The
// offsets are absolute and into the document as it was handed to the editor, so
// src[Start:End] is the text the edit covers and the caller can show it without
// applying anything.
//
// An empty span (Start == End) inserts at Start, and empty text deletes the span.
type DocumentEdit struct {
	// Text is what goes in place of the span. Empty text deletes.
	Text string

	// Start is the offset the edit begins at, counted in bytes from the start of
	// the document.
	Start int

	// End is the offset the edit stops before. It is never smaller than Start.
	End int
}

// DocumentPatch is the set of edits that turn a document into one holding the
// data that was asked for. It is what an in-place rewrite does, before it is
// written: a caller can show a diff, count the lines that change, hand the edits
// to an editor that speaks ranges, or apply them and write the result.
//
// The edits come back in ascending order and never cover the same bytes twice.
type DocumentPatch struct {
	// Edits are the replacements, in ascending order of Start. Two edits that
	// start at the same offset go in in the order they appear here.
	Edits []DocumentEdit
}

var (
	// ErrNoDocumentPatcher reports that a [Parser] cannot describe an edit as a
	// patch. It says nothing about the data: the parser may still rewrite the
	// document through [EditDocument], and a caller with no diff to show loses
	// nothing.
	ErrNoDocumentPatcher = errors.New("kongfig: parser cannot describe an edit as a patch")

	// ErrPatchNotApplicable reports a patch that does not fit the document it was
	// applied to: an edit outside it, or two edits over the same text. A patch is
	// a value a caller can hold on to, so a document that has moved on since is an
	// error rather than a panic.
	ErrPatchNotApplicable = errors.New("kongfig: patch does not fit the document")
)

// Apply returns src with the edits spliced in. It reads src and does not write to
// it, so the same patch applies to the same bytes as often as the caller likes —
// show a preview, then write.
//
// It errors with [ErrPatchNotApplicable] for a patch that does not fit src rather
// than returning a half-edited document.
func (p DocumentPatch) Apply(src []byte) ([]byte, error) {
	out, err := editsplice.Apply(src, spliceEdits(p.Edits))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPatchNotApplicable, err)
	}
	return out, nil
}

func spliceEdits(edits []DocumentEdit) []editsplice.Edit {
	out := make([]editsplice.Edit, len(edits))
	for i, ed := range edits {
		out[i] = editsplice.Edit{Text: ed.Text, Start: ed.Start, End: ed.End}
	}
	return out
}

// DocumentPatcher is an optional interface a [Parser] with a [DocumentEditor] can
// implement to hand the edits over instead of the document they make. The two
// describe the same rewrite: applying the patch to src gives what
// [DocumentEditor.EditDocument] would have returned.
//
// want is the whole document, the same way [DocumentEditor] takes it, and an
// implementation refuses the same changes for the same reasons. A document that
// already holds want gives an empty patch.
//
// Implementations are called through [PatchDocument], which checks the patch
// before it reaches the caller. Call the method directly only to skip that check.
type DocumentPatcher interface {
	PatchDocument(src []byte, want ConfigData) (DocumentPatch, error)
}

// PatchDocument returns the edits that turn src into a document holding want,
// with the offsets into src as it was handed over. It applies the patch and
// parses the result before returning, so a patch that describes the wrong rewrite
// is an error rather than a diff the caller shows and then writes.
//
// Errors:
//
//   - [ErrNoDocumentPatcher] when p cannot describe its edits — the caller may
//     still fall back to [EditDocument].
//   - the parser's own error for a change the format cannot express as an edit.
//   - [ErrEditNotVerified] when the patched document holds something else.
//
// Nothing here touches the filesystem, and nothing writes to src.
func PatchDocument(p Parser, src []byte, want ConfigData) (DocumentPatch, error) {
	patcher, ok := p.(DocumentPatcher)
	if !ok {
		return DocumentPatch{}, fmt.Errorf("%w: %T", ErrNoDocumentPatcher, p)
	}
	patch, err := patcher.PatchDocument(src, want)
	if err != nil {
		return DocumentPatch{}, fmt.Errorf("kongfig: patch document: %w", err)
	}
	out, err := patch.Apply(src)
	if err != nil {
		return DocumentPatch{}, err
	}
	if err := verifyDocument(p, out, want); err != nil {
		return DocumentPatch{}, err
	}
	return patch, nil
}
