package kongfig

import "context"

var appNameKey = NewOptionsKey[string]()

// WithAppName injects an application name into ctx for use by components that
// need it (e.g. XDG file discoverers, kong integration).
// Read the stored name with [AppName].
func WithAppName(ctx context.Context, name string) context.Context {
	return appNameKey.With(ctx, name)
}

// AppName returns the app name stored in ctx by [WithAppName], or "" if not set.
func AppName(ctx context.Context) string {
	v, _ := appNameKey.From(ctx)
	return v
}
