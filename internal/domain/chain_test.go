package domain

import "testing"

func TestChainBudgetPredicates(t *testing.T) {
	c := Chain{LimitCount: 6}
	if c.OverLimit(6) {
		t.Fatal("the 6th wake is inside the limit")
	}
	if !c.OverLimit(7) {
		t.Fatal("the 7th wake must be over the limit")
	}
}
