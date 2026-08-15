package core

import "testing"

func TestLineRangeTouches(t *testing.T) {
	for _, tc := range []struct {
		name       string
		r          LineRange
		start, end int
		want       bool
	}{
		{"inside", LineRange{Start: 5, Count: 2}, 1, 10, true},
		{"exactly the declaration", LineRange{Start: 1, Count: 10}, 1, 10, true},
		{"straddling the top", LineRange{Start: 1, Count: 3}, 3, 10, true},
		{"straddling the bottom", LineRange{Start: 9, Count: 5}, 1, 10, true},
		{"entirely before", LineRange{Start: 1, Count: 2}, 5, 10, false},
		{"entirely after", LineRange{Start: 20, Count: 2}, 5, 10, false},
		{"adjacent above", LineRange{Start: 4, Count: 1}, 5, 10, false},
		{"adjacent below", LineRange{Start: 11, Count: 1}, 5, 10, false},
	} {
		if got := tc.r.Touches(tc.start, tc.end); got != tc.want {
			t.Errorf("%s: %+v.Touches(%d, %d) = %v, want %v", tc.name, tc.r, tc.start, tc.end, got, tc.want)
		}
	}
}

// Git reports a pure deletion as a zero-length insertion point. Treating those
// as empty would silently drop every commit that only removed code, which is a
// large share of the corrective commits the backtest cares about most.
func TestZeroCountRangeStillAttributes(t *testing.T) {
	del := LineRange{Start: 7, Count: 0}
	if !del.Touches(1, 10) {
		t.Error("a deletion inside a declaration did not attribute to it")
	}
	if !del.Touches(1, 6) {
		t.Error("a deletion at the boundary after line 6 should attribute to a declaration ending there")
	}
	if del.Touches(8, 20) {
		t.Error("a deletion before a declaration attributed to it")
	}
}
