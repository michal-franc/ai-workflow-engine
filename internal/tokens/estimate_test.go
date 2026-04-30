package tokens

import "testing"

func TestEstimate_Empty(t *testing.T) {
	if got := Estimate(""); got != 0 {
		t.Fatalf("Estimate(\"\") = %d, want 0", got)
	}
}

func TestEstimate_LenDivFour(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"a", 0},
		{"abcd", 1},
		{"abcdefgh", 2},
		{"abcdefghi", 2},
	}
	for _, c := range cases {
		if got := Estimate(c.in); got != c.want {
			t.Errorf("Estimate(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
