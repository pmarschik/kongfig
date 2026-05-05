package discover

import "context"

// LabeledDiscoverer pairs a user label with a discoverer for use with [First].
// Create with [WithLabel].
type LabeledDiscoverer struct {
	discoverer innerDiscoverer
	label      string
}

// WithLabel creates a [LabeledDiscoverer] pairing label with d.
func WithLabel(label string, d innerDiscoverer) LabeledDiscoverer {
	return LabeledDiscoverer{label: label, discoverer: d}
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
		path, err := d.discoverer.Discover(ctx, exts)
		if err != nil {
			return DiscoveryHit{}, err
		}
		if path != "" {
			return DiscoveryHit{Label: d.label, Path: path}, nil
		}
	}
	return DiscoveryHit{}, nil
}
