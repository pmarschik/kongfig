package kongfig

import "strings"

// ScanFlag scans args for the first occurrence of a named flag and returns its
// value. It handles the following forms:
//
//   - --long=value
//   - --long value
//   - -s value   (for each short name in shorts)
//
// Returns "" if the flag is not present.
//
// Use this to extract flags (e.g. --config) from os.Args before passing them to
// a full flag parser. This is useful when you need a config file path to initialize
// a [Kongfig] before calling kong.Parse or flag.Parse, which would otherwise consume
// the flag first.
func ScanFlag(args []string, long string, shorts ...string) string {
	prefix := "--" + long + "="
	for i, a := range args {
		if strings.HasPrefix(a, prefix) {
			return a[len(prefix):]
		}
		if a == "--"+long && i+1 < len(args) {
			return args[i+1]
		}
		for _, s := range shorts {
			if a == "-"+s && i+1 < len(args) {
				return args[i+1]
			}
		}
	}
	return ""
}
