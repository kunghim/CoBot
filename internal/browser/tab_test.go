package browser

import "testing"

func TestTabQuotaCountsUserTabsSeparately(t *testing.T) {
	tabs := map[string]*tab{}
	for i := 0; i < 3; i++ {
		tabs[string(rune('a'+i))] = &tab{member: "bot-a"}
	}
	tabs["u1"] = &tab{member: OwnerUser}
	tabs["u2"] = &tab{member: OwnerUser}

	// A member sees its own tabs against its cap, and every member tab
	// against the team cap: the user's tabs count toward neither.
	mine, all := tabQuota(tabs, "bot-a")
	if mine != 3 || all != 3 {
		t.Fatalf("bot-a quota = %d mine, %d all; want 3, 3", mine, all)
	}
	mine, all = tabQuota(tabs, "bot-b")
	if mine != 0 || all != 3 {
		t.Fatalf("bot-b quota = %d mine, %d all; want 0, 3", mine, all)
	}

	// The user has a cap of their own, over their tabs alone.
	if maxTabsPerUser != 5 {
		t.Fatalf("user tab limit = %d, want 5", maxTabsPerUser)
	}
	mine, _ = tabQuota(tabs, OwnerUser)
	if mine != 2 {
		t.Fatalf("user quota = %d, want 2", mine)
	}
	for i := 0; i < 3; i++ {
		tabs["u"+string(rune('3'+i))] = &tab{member: OwnerUser}
	}
	mine, all = tabQuota(tabs, OwnerUser)
	if mine != 5 || all != 0 {
		t.Fatalf("user quota after five = %d mine, %d all; want 5, 0", mine, all)
	}
	mine, all = tabQuota(tabs, "bot-a")
	if mine != 3 || all != 3 {
		t.Fatalf("bot-a quota with five user tabs = %d mine, %d all; want 3, 3", mine, all)
	}
}
