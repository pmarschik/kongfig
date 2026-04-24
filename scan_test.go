package kongfig_test

import (
	"testing"

	kongfig "github.com/pmarschik/kongfig"
)

func TestScanFlag(t *testing.T) {
	tests := []struct {
		long   string
		want   string
		args   []string
		shorts []string
	}{
		{args: []string{"--config=foo.yaml"}, long: "config", shorts: []string{"c"}, want: "foo.yaml"},
		{args: []string{"--config", "foo.yaml"}, long: "config", shorts: []string{"c"}, want: "foo.yaml"},
		{args: []string{"-c", "foo.yaml"}, long: "config", shorts: []string{"c"}, want: "foo.yaml"},
		{args: []string{"--host=prod", "--config=bar.toml"}, long: "config", shorts: []string{"c"}, want: "bar.toml"},
		{args: []string{"--host=prod"}, long: "config", shorts: []string{"c"}, want: ""},
		{args: []string{}, long: "config", shorts: nil, want: ""},
		{args: []string{"--config"}, long: "config", shorts: nil, want: ""},
		{args: []string{"-c"}, long: "config", shorts: []string{"c"}, want: ""},
	}
	for _, tt := range tests {
		got := kongfig.ScanFlag(tt.args, tt.long, tt.shorts...)
		if got != tt.want {
			t.Errorf("ScanFlag(%v, %q, %v) = %q, want %q", tt.args, tt.long, tt.shorts, got, tt.want)
		}
	}
}
