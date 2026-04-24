// Package codec provides ready-made [kongfig.Codec] values for common stdlib types.
// Register them on a [kongfig.Kongfig] instance via [Default] (recommended) or individually
// with [kongfig.WithCodec]:
//
//	// Easiest — register all standard codecs at once:
//	kf := kongfig.New(kongfig.WithCodecRegistry(codec.Default))
//
//	// Or individually:
//	kf := kongfig.New(
//	    kongfig.WithCodec("ip", codec.IP),
//	    kongfig.WithCodec("duration", codec.Duration),
//	    kongfig.WithCodec("time-rfc3339", codec.TimeRFC3339),
//	)
//
// Codecs registered on [kongfig.NewFor][T] are applied automatically at load time for
// fields whose Go type matches, or explicitly via the codec= struct tag annotation:
//
//	type Config struct {
//	    Addr    net.IP    `kongfig:"addr"`                       // auto-matched by type
//	    Created time.Time `kongfig:"created,codec=time-rfc3339"` // explicit
//	    Updated time.Time `kongfig:"updated,codec=time-date"`    // different layout
//	}
package codec

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"time"

	kongfig "github.com/pmarschik/kongfig"
)

// Of wraps c as a [kongfig.CodecEntry] for use with [(*kongfig.CodecRegistry).Register]:
//
//	r.Register("ip", codec.Of(codec.IP))
//	r.Register("duration", codec.Of(codec.Duration))
//
// It is a convenience re-export of [kongfig.Of]; use that directly when working with
// custom codecs that are not defined in this package.
func Of[T any](c kongfig.Codec[T]) kongfig.CodecEntry {
	return kongfig.Of(c)
}

// Default is a [kongfig.CodecRegistry] pre-populated with all standard codecs in
// this package. Pass it to [kongfig.WithCodecRegistry] to register all at once:
//
//	kf := kongfig.New(kongfig.WithCodecRegistry(codec.Default))
//
// The first codec registered for each Go type wins for auto-detection; registration
// order within Default is: IP, Duration, URL, Regexp, TimeRFC3339.
// TimeDate and custom TimeFormat layouts are only reachable via explicit codec= tags.
var Default = func() *kongfig.CodecRegistry {
	r := kongfig.NewCodecRegistry()
	r.Register("ip", Of(IP)).
		Register("duration", Of(Duration)).
		Register("url", Of(URL)).
		Register("regexp", Of(Regexp)).
		Register("time-rfc3339", Of(TimeRFC3339)).
		Register("time-date", Of(TimeDate))
	return r
}()

// toString converts v to string; returns ("", false) if v is not a string.
func toString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// IP parses net.IP from a string (IPv4 or IPv6).
var IP = kongfig.Codec[net.IP]{
	Decode: func(v any) (net.IP, error) {
		if ip, ok := v.(net.IP); ok {
			return ip, nil
		}
		s, ok := toString(v)
		if !ok {
			return nil, fmt.Errorf("kongfig/codec: IP: expected string, got %T", v)
		}
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("kongfig/codec: IP: invalid address %q", s)
		}
		return ip, nil
	},
	Encode: func(ip net.IP) string { return ip.String() },
}

// Duration parses time.Duration from a string (e.g. "1h30m").
var Duration = kongfig.Codec[time.Duration]{
	Decode: func(v any) (time.Duration, error) {
		if d, ok := v.(time.Duration); ok {
			return d, nil
		}
		s, ok := toString(v)
		if !ok {
			return 0, fmt.Errorf("kongfig/codec: Duration: expected string, got %T", v)
		}
		return time.ParseDuration(s)
	},
	Encode: func(d time.Duration) string { return d.String() },
}

// URL parses *url.URL from a string.
var URL = kongfig.Codec[*url.URL]{
	Decode: func(v any) (*url.URL, error) {
		if u, ok := v.(*url.URL); ok {
			return u, nil
		}
		s, ok := toString(v)
		if !ok {
			return nil, fmt.Errorf("kongfig/codec: URL: expected string, got %T", v)
		}
		return url.Parse(s)
	},
	Encode: func(u *url.URL) string { return u.String() },
}

// Regexp compiles *regexp.Regexp from a pattern string.
var Regexp = kongfig.Codec[*regexp.Regexp]{
	Decode: func(v any) (*regexp.Regexp, error) {
		if re, ok := v.(*regexp.Regexp); ok {
			return re, nil
		}
		s, ok := toString(v)
		if !ok {
			return nil, fmt.Errorf("kongfig/codec: Regexp: expected string, got %T", v)
		}
		return regexp.Compile(s)
	},
	Encode: func(re *regexp.Regexp) string { return re.String() },
}

// TimeRFC3339 parses time.Time using RFC3339 layout.
var TimeRFC3339 = kongfig.Codec[time.Time]{
	Decode: func(v any) (time.Time, error) {
		if t, ok := v.(time.Time); ok {
			return t, nil
		}
		s, ok := toString(v)
		if !ok {
			return time.Time{}, fmt.Errorf("kongfig/codec: TimeRFC3339: expected string, got %T", v)
		}
		return time.Parse(time.RFC3339, s)
	},
	Encode: func(t time.Time) string { return t.Format(time.RFC3339) },
}

// TimeDate parses time.Time using "2006-01-02" date-only layout.
var TimeDate = kongfig.Codec[time.Time]{
	Decode: func(v any) (time.Time, error) {
		if t, ok := v.(time.Time); ok {
			return t, nil
		}
		s, ok := toString(v)
		if !ok {
			return time.Time{}, fmt.Errorf("kongfig/codec: TimeDate: expected string, got %T", v)
		}
		return time.Parse("2006-01-02", s)
	},
	Encode: func(t time.Time) string { return t.Format("2006-01-02") },
}

// TimeFormat returns a Codec[time.Time] with a custom layout string.
// Register it under a name of your choice:
//
//	kongfig.WithCodec("time-kitchen", codec.TimeFormat(time.Kitchen))
func TimeFormat(layout string) kongfig.Codec[time.Time] {
	return kongfig.Codec[time.Time]{
		Decode: func(v any) (time.Time, error) {
			if t, ok := v.(time.Time); ok {
				return t, nil
			}
			s, ok := toString(v)
			if !ok {
				return time.Time{}, fmt.Errorf("kongfig/codec: TimeFormat: expected string, got %T", v)
			}
			return time.Parse(layout, s)
		},
		Encode: func(t time.Time) string { return t.Format(layout) },
	}
}
