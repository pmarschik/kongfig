package plain_test

import (
	"testing"

	"github.com/pmarschik/kongfig/style/plain"
)

func TestPlainReturnsInputUnchanged(t *testing.T) {
	p := plain.New()
	cases := []string{"hello", "", "  spaces  ", "--flag=value"}
	for _, s := range cases {
		if got := p.Key(s); got != s {
			t.Errorf("Key(%q) = %q", s, got)
		}
		if got := p.String(s); got != s {
			t.Errorf("String(%q) = %q", s, got)
		}
		if got := p.Number(s); got != s {
			t.Errorf("Number(%q) = %q", s, got)
		}
		if got := p.Bool(s); got != s {
			t.Errorf("Bool(%q) = %q", s, got)
		}
		if got := p.Comment(s); got != s {
			t.Errorf("Comment(%q) = %q", s, got)
		}
		if got := p.Annotation("src", s); got != s {
			t.Errorf("Annotation(%q) = %q", s, got)
		}
	}
}
