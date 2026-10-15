package domain

import "testing"

// TestRiskZoneStart pins the accounting rule: the last ceil(limit*0.2) wakes
// are the risk zone, and a limit of 6 means wakes 5 and 6 are evaluated
// before they start.
func TestRiskZoneStart(t *testing.T) {
	cases := []struct {
		limit int64
		want  int64
	}{
		{6, 5}, // the documented default
		{3, 3},
		{1, 1},
		{5, 5},
		{10, 9},
		{100, 81},
		{0, 1},
	}
	for _, c := range cases {
		if got := RiskZoneStart(c.limit); got != c.want {
			t.Fatalf("RiskZoneStart(%d) = %d, want %d", c.limit, got, c.want)
		}
	}
}

func TestChainBudgetPredicates(t *testing.T) {
	c := Chain{LimitCount: 6, RiskStart: 5}
	if c.NeedsEvaluation(4) {
		t.Fatal("wake 4 of 6 is outside the risk zone")
	}
	if !c.NeedsEvaluation(5) || !c.NeedsEvaluation(6) || !c.NeedsEvaluation(7) {
		t.Fatal("wakes 5+ must be evaluated")
	}
	if c.OverLimit(6) {
		t.Fatal("the 6th wake is inside the limit")
	}
	if !c.OverLimit(7) {
		t.Fatal("the 7th wake must be over the hard limit")
	}
}
