package discover

import "context"

// LabeledDiscoverer wraps a discoverer and overrides its Name with a user-supplied
// label. It implements the full discoverer interface so it can be passed to [FirstOf],
// [Deprecated], and any other combinator, in addition to [First].
// Create with [WithLabel].
type LabeledDiscoverer struct {
	discoverer innerDiscoverer
	label      string
}

// WithLabel wraps d with a user-supplied label that overrides d.Name().
func WithLabel(label string, d innerDiscoverer) LabeledDiscoverer {
	return LabeledDiscoverer{label: label, discoverer: d}
}

// Name returns the user-supplied label.
func (l LabeledDiscoverer) Name() string { return l.label }

// Discover delegates to the wrapped discoverer.
func (l LabeledDiscoverer) Discover(ctx context.Context, exts []string) (string, error) {
	return l.discoverer.Discover(ctx, exts)
}

// DisplayPath delegates to the wrapped discoverer's DisplayPath if it supports it.
func (l LabeledDiscoverer) DisplayPath(ctx context.Context, foundPath string) string {
	type displayPather interface {
		DisplayPath(context.Context, string) string
	}
	if dp, ok := l.discoverer.(displayPather); ok {
		return dp.DisplayPath(ctx, foundPath)
	}
	return ""
}

// DiscoveryHit is returned by [First].
// Path is empty when no discoverer found a file.
type DiscoveryHit struct {
	Label string // label from the winning [LabeledDiscoverer]
	Path  string // found absolute path, or "" if nothing found
}

// First tries each [LabeledDiscoverer] in order and returns the first hit.
// The first error short-circuits; if all return empty, DiscoveryHit{} with a nil error.
//
// Replaces the common priority-chain pattern where each stage maps to a different
// config key:
//
//	hit, _ := discover.First(ctx, exts,
//	    discover.WithLabel("local",  discover.UpwardFunc(discover.LocateNames(".myapp.local"))),
//	    discover.WithLabel("system", discover.UserDirs()),
//	)
//	if hit.Path != "" {
//	    k.MustLoad(ctx, file.Provider(hit.Path), kongfig.WithSource(hit.Label))
//	}
func First(ctx context.Context, exts []string, ds ...LabeledDiscoverer) (DiscoveryHit, error) {
	for _, d := range ds {
		path, err := d.Discover(ctx, exts)
		if err != nil {
			return DiscoveryHit{}, err
		}
		if path != "" {
			return DiscoveryHit{Label: d.label, Path: path}, nil
		}
	}
	return DiscoveryHit{}, nil
}
