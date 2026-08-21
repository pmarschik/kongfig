package kongfig

import (
	"errors"
	"fmt"
	"slices"
)

var (
	// ErrNoSuchLayer reports that no loaded layer carries the source label an
	// edit named. The label is the caller's own — the one it passed to Load —
	// so this is a mistake in the program rather than in the configuration.
	ErrNoSuchLayer = errors.New("kongfig: no layer with that source")

	// ErrLayerHasNoDocument reports that a layer was not read from a document:
	// environment variables, flags and defaults have no text to edit in place.
	ErrLayerHasNoDocument = errors.New("kongfig: layer was not loaded from a document")
)

// EditLayer rewrites one layer's document. mutate is handed the data that layer
// contributed — not the merged configuration — and whatever it leaves behind is
// written back into src through [EditDocument].
//
// Editing the merge is the mistake this exists to prevent. The merged view holds
// values from environment variables and flags that were never in the file, and
// is missing every key a higher layer overrode; writing it back would move those
// values into the file and delete the ones the merge hid.
//
// src is the document as the caller read it, and the returned bytes are the
// document as the caller should write it. Nothing here touches the filesystem —
// kongfig does not know where a layer's bytes came from, and reading a file
// again between load and edit is the caller's decision to make.
//
// When more than one layer carries source, the most recently loaded one is
// edited: that is the one whose values the merge shows. Errors:
//
//   - [ErrNoSuchLayer] when no loaded layer carries that source label.
//   - [ErrLayerHasNoDocument] when the layer was loaded without a parser.
//   - whatever mutate returns, unchanged, with no document.
//   - the errors of [EditDocument] for a change the format cannot express, or a
//     rewrite that did not come back holding the requested data.
func (k *Kongfig) EditLayer(source string, src []byte, mutate func(ConfigData) error) ([]byte, error) {
	layer, ok := k.layerNamed(source)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoSuchLayer, source)
	}
	if layer.Parser == nil {
		return nil, fmt.Errorf("%w: %q", ErrLayerHasNoDocument, source)
	}
	want := layer.Data
	if err := mutate(want); err != nil {
		return nil, err
	}
	return EditDocument(layer.Parser, src, want)
}

// layerNamed finds the last layer loaded under source, with its data copied out
// so an edit cannot reach back into loaded state.
func (k *Kongfig) layerNamed(source string) (Layer, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	for i := range slices.Backward(k.pipeline) {
		layer := k.pipeline[i].layer
		if layer.Meta.Name != source {
			continue
		}
		layer.Data = layer.Data.Clone()
		return layer, true
	}
	return Layer{}, false
}
