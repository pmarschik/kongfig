package render_test

import (
	"context"
	"strings"
	"testing"

	kongfig "github.com/pmarschik/kongfig"
	"github.com/pmarschik/kongfig/render"
)

// rules is a map of two-key entries, the shape a sortby mark is written for: the
// keys carry no order of their own, and what makes one entry come first is a
// value inside it.
func rules() kongfig.ConfigData {
	return kongfig.ConfigData{
		"beta":  kongfig.ConfigData{"priority": 2},
		"alpha": kongfig.ConfigData{"priority": 3},
		"gamma": kongfig.ConfigData{"priority": 1},
	}
}

func keys(ctx context.Context, prefix string, data kongfig.ConfigData) string {
	return strings.Join(render.OrderedKeys(ctx, prefix, data), ",")
}

// A sortby mark orders a map's children by a value inside them, which is the
// whole point: alphabetical order says nothing about a priority field.
func TestOrderedKeys_SortByAscending(t *testing.T) {
	ctx := kongfig.KeySortByKey.WithCtx(context.Background(),
		map[string]string{"rules": "priority"})

	if got := keys(ctx, "rules", rules()); got != "gamma,beta,alpha" {
		t.Errorf("order = %s, want gamma,beta,alpha", got)
	}
}

// A leading minus reverses it, for the common case where the highest priority
// should be read first.
func TestOrderedKeys_SortByDescending(t *testing.T) {
	ctx := kongfig.KeySortByKey.WithCtx(context.Background(),
		map[string]string{"rules": "-priority"})

	if got := keys(ctx, "rules", rules()); got != "alpha,beta,gamma" {
		t.Errorf("order = %s, want alpha,beta,gamma", got)
	}
}

// The mark is written on a container that may sit inside a map, so it arrives as
// a pattern with "*" segments, matched the way the other path marks are.
func TestOrderedKeys_SortByMatchesWildcardPattern(t *testing.T) {
	ctx := kongfig.KeySortByKey.WithCtx(context.Background(),
		map[string]string{"profiles.*.rules": "priority"})

	if got := keys(ctx, "profiles.work.rules", rules()); got != "gamma,beta,alpha" {
		t.Errorf("order = %s, want gamma,beta,alpha", got)
	}
}

// A value that is missing, or of a kind the others are not, cannot be placed
// among them — it goes last rather than sorting as a zero, in either direction.
func TestOrderedKeys_SortByPutsUnsortableLast(t *testing.T) {
	data := kongfig.ConfigData{
		"beta":    kongfig.ConfigData{"priority": 2},
		"missing": kongfig.ConfigData{},
		"text":    kongfig.ConfigData{"priority": "high"},
		"gamma":   kongfig.ConfigData{"priority": 1},
	}
	for _, spec := range []string{"priority", "-priority"} {
		ctx := kongfig.KeySortByKey.WithCtx(context.Background(),
			map[string]string{"rules": spec})
		got := render.OrderedKeys(ctx, "rules", data)
		if last := got[len(got)-1]; last != "missing" {
			t.Errorf("sortby=%s put %q last, want missing:\n%v", spec, last, got)
		}
		if got[len(got)-2] != "text" {
			t.Errorf("sortby=%s left the string value out of place: %v", spec, got)
		}
	}
}

// Entries that compare equal keep the order they would have had without the
// sort, so a stable result does not depend on map iteration.
func TestOrderedKeys_SortByIsStable(t *testing.T) {
	data := kongfig.ConfigData{
		"zebra": kongfig.ConfigData{"priority": 1},
		"apple": kongfig.ConfigData{"priority": 1},
		"mango": kongfig.ConfigData{"priority": 1},
	}
	ctx := kongfig.KeySortByKey.WithCtx(context.Background(),
		map[string]string{"rules": "priority"})

	// No order is bound, so the baseline the sort starts from is alphabetical.
	if got := keys(ctx, "rules", data); got != "apple,mango,zebra" {
		t.Errorf("order = %s, want the alphabetical baseline apple,mango,zebra", got)
	}
}

// Ties fall back to the document order when there is one, not to the alphabet:
// the sort reorders what the lower-precedence rules produced.
func TestOrderedKeys_SortByTiesKeepDocumentOrder(t *testing.T) {
	data := kongfig.ConfigData{
		"zebra": kongfig.ConfigData{"priority": 1},
		"apple": kongfig.ConfigData{"priority": 1},
	}
	ctx := kongfig.RenderDerivedKeyOrderKey.WithCtx(context.Background(),
		map[string][]string{"rules": {"zebra", "apple"}})
	ctx = kongfig.KeySortByKey.WithCtx(ctx, map[string]string{"rules": "priority"})

	if got := keys(ctx, "rules", data); got != "zebra,apple" {
		t.Errorf("order = %s, want the document's zebra,apple", got)
	}
}

// A dotted spec reaches a value one level down, since what an entry is ranked by
// is not always a direct key of it.
func TestOrderedKeys_SortByDottedField(t *testing.T) {
	data := kongfig.ConfigData{
		"beta":  kongfig.ConfigData{"meta": kongfig.ConfigData{"priority": 2}},
		"alpha": kongfig.ConfigData{"meta": kongfig.ConfigData{"priority": 1}},
	}
	ctx := kongfig.KeySortByKey.WithCtx(context.Background(),
		map[string]string{"rules": "meta.priority"})

	if got := keys(ctx, "rules", data); got != "alpha,beta" {
		t.Errorf("order = %s, want alpha,beta", got)
	}
}

// Rendering wraps every leaf in a RenderedValue, so the sort has to look through
// the wrapper or it would rank every entry as unsortable.
func TestOrderedKeys_SortByReadsThroughRenderedValue(t *testing.T) {
	data := kongfig.ConfigData{
		"beta":  kongfig.ConfigData{"priority": kongfig.RenderedValue{Value: 2}},
		"alpha": kongfig.ConfigData{"priority": kongfig.RenderedValue{Value: 1}},
	}
	ctx := kongfig.KeySortByKey.WithCtx(context.Background(),
		map[string]string{"rules": "priority"})

	if got := keys(ctx, "rules", data); got != "alpha,beta" {
		t.Errorf("order = %s, want alpha,beta", got)
	}
}

// A comparator says what a tag cannot, and has the last word over one. It is
// handed the keys in the order the rules it outranks produced — the sortby order
// here — so it can refine that rather than start from map iteration.
func TestOrderedKeys_ComparatorOverridesSortBy(t *testing.T) {
	var sawKeys []string
	ctx := kongfig.RenderKeySortKey.WithCtx(context.Background(),
		func(_ string, ks []string, _ kongfig.ConfigData) []string {
			sawKeys = ks
			return []string{"alpha", "beta", "gamma"}
		})
	ctx = kongfig.KeySortByKey.WithCtx(ctx, map[string]string{"rules": "priority"})

	if got := keys(ctx, "rules", rules()); got != "alpha,beta,gamma" {
		t.Errorf("order = %s, want the comparator's alpha,beta,gamma", got)
	}
	if strings.Join(sawKeys, ",") != "gamma,beta,alpha" {
		t.Errorf("comparator was handed %v, want the sortby order [gamma beta alpha]", sawKeys)
	}
}

// A comparator is caller code: whatever it forgets or invents must not change
// which keys get rendered.
func TestOrderedKeys_ComparatorCannotAddOrDropKeys(t *testing.T) {
	ctx := kongfig.RenderKeySortKey.WithCtx(context.Background(),
		func(_ string, _ []string, _ kongfig.ConfigData) []string {
			return []string{"gamma", "nope"}
		})

	if got := keys(ctx, "rules", rules()); got != "gamma,alpha,beta" {
		t.Errorf("order = %s, want gamma first then the rest alphabetically", got)
	}
}

// The call site has the last word over both hooks: an explicit order is a
// caller saying exactly what it wants.
func TestOrderedKeys_ExplicitOrderBeatsBothHooks(t *testing.T) {
	ctx := kongfig.RenderKeyOrderKey.WithCtx(context.Background(),
		map[string][]string{"rules": {"beta", "alpha", "gamma"}})
	ctx = kongfig.KeySortByKey.WithCtx(ctx, map[string]string{"rules": "priority"})
	ctx = kongfig.RenderKeySortKey.WithCtx(ctx,
		func(_ string, _ []string, _ kongfig.ConfigData) []string {
			return []string{"gamma", "beta", "alpha"}
		})

	if got := keys(ctx, "rules", rules()); got != "beta,alpha,gamma" {
		t.Errorf("order = %s, want the explicit beta,alpha,gamma", got)
	}
}

// A mark for one parent says nothing about another, so an unmarked path keeps
// the order it had.
func TestOrderedKeys_SortByLeavesOtherPathsAlone(t *testing.T) {
	ctx := kongfig.KeySortByKey.WithCtx(context.Background(),
		map[string]string{"rules": "priority"})

	if got := keys(ctx, "other", rules()); got != "alpha,beta,gamma" {
		t.Errorf("order = %s, want alphabetical alpha,beta,gamma", got)
	}
}

// The derived order still applies where no hook does — the merge of document and
// field order is the default, not a fallback the hooks replaced.
func TestOrderedKeys_DerivedOrderApplies(t *testing.T) {
	ctx := kongfig.RenderDerivedKeyOrderKey.WithCtx(context.Background(),
		map[string][]string{"rules": {"gamma", "alpha"}})

	if got := keys(ctx, "rules", rules()); got != "gamma,alpha,beta" {
		t.Errorf("order = %s, want gamma,alpha then the rest alphabetically", got)
	}
}

// An explicit order overrides the derived one rather than merging with it.
func TestOrderedKeys_ExplicitOrderBeatsDerived(t *testing.T) {
	ctx := kongfig.RenderDerivedKeyOrderKey.WithCtx(context.Background(),
		map[string][]string{"rules": {"gamma", "alpha", "beta"}})
	ctx = kongfig.RenderKeyOrderKey.WithCtx(ctx,
		map[string][]string{"rules": {"beta", "gamma"}})

	if got := keys(ctx, "rules", rules()); got != "beta,gamma,alpha" {
		t.Errorf("order = %s, want beta,gamma then the rest alphabetically", got)
	}
}

// The ctx-aware marshalers ask one question before choosing their fast path:
// is there anything here that orders keys at all?
func TestHasKeyOrder(t *testing.T) {
	base := context.Background()
	for name, ctx := range map[string]context.Context{
		"explicit":   kongfig.RenderKeyOrderKey.WithCtx(base, map[string][]string{"": {"a"}}),
		"derived":    kongfig.RenderDerivedKeyOrderKey.WithCtx(base, map[string][]string{"": {"a"}}),
		"sortby":     kongfig.KeySortByKey.WithCtx(base, map[string]string{"rules": "priority"}),
		"comparator": kongfig.RenderKeySortKey.WithCtx(base, func(_ string, ks []string, _ kongfig.ConfigData) []string { return ks }),
	} {
		if !render.HasKeyOrder(ctx) {
			t.Errorf("HasKeyOrder with a %s order = false, want true", name)
		}
	}
	if render.HasKeyOrder(base) {
		t.Error("HasKeyOrder on a bare context = true, want false")
	}
}
