package rules

import "testing"

// The cost model is ordered, not binary (#22): a hook that can afford the
// range-scoped escapes but not the whole-tree ones needs the classes between
// fast and heavy to exist and to sort.
func TestRank_OrdersTheFourClasses(t *testing.T) {
	order := []Cost{CostFast, CostRange, CostTree, CostHeavy}
	for i := 1; i < len(order); i++ {
		if Rank(order[i-1]) >= Rank(order[i]) {
			t.Fatalf("Rank(%s)=%d must be below Rank(%s)=%d", order[i-1], Rank(order[i-1]), order[i], Rank(order[i]))
		}
	}
	if Rank(Cost("bogus")) != Rank(CostHeavy) {
		t.Fatalf("an unknown class must rank as heavy (fail closed), got %d", Rank(Cost("bogus")))
	}
}

func TestValidCost_AcceptsEveryClassAndNothingElse(t *testing.T) {
	for _, c := range []string{"fast", "range", "tree", "heavy"} {
		if !ValidCost(c) {
			t.Errorf("ValidCost(%q) must be true", c)
		}
	}
	for _, c := range []string{"", "cheap", "Heavy"} {
		if ValidCost(c) {
			t.Errorf("ValidCost(%q) must be false", c)
		}
	}
}
