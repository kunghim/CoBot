package roles

import "testing"

func TestScenariosReferenceLibraryRoles(t *testing.T) {
	known := map[string]bool{}
	for _, id := range DefaultIDs {
		known[id] = true
	}
	for _, scenario := range Scenarios {
		offered := map[string]bool{}
		for _, id := range scenario.RoleIDs {
			if !known[id] || id == LeaderRoleID {
				t.Fatalf("scenario %s offers %q", scenario.ID, id)
			}
			offered[id] = true
		}
		for _, id := range scenario.DefaultRoleIDs {
			if !offered[id] {
				t.Fatalf("scenario %s preselects %q it does not offer", scenario.ID, id)
			}
		}
	}
	if got := ScenariosOf(FullstackRoleID); len(got) != 1 || got[0] != "software" {
		t.Fatal(got)
	}
	if got := ScenariosOf(LeaderRoleID); len(got) != 0 {
		t.Fatal(got)
	}
}
