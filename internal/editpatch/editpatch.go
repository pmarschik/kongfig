// Package editpatch turns the edits an in-place editor collected into the
// [kongfig.DocumentPatch] it hands to a caller, so every parser reports its edits
// the same way: in ascending order, with the offsets of the document it read.
package editpatch

import (
	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/internal/editsplice"
)

// From returns the edits as a patch, sorted the way a caller walking the document
// alongside them needs. It errors for a set of edits no document can take, which
// is what [editsplice.Apply] would have refused to write.
func From(edits []editsplice.Edit) (kongfig.DocumentPatch, error) {
	ordered, err := editsplice.Ordered(edits)
	if err != nil {
		return kongfig.DocumentPatch{}, err
	}
	patch := kongfig.DocumentPatch{Edits: make([]kongfig.DocumentEdit, len(ordered))}
	for i, ed := range ordered {
		patch.Edits[i] = kongfig.DocumentEdit{Text: ed.Text, Start: ed.Start, End: ed.End}
	}
	return patch, nil
}
