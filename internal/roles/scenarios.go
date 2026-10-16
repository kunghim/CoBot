package roles

// Scenario groups built-in roles for one kind of work. Creating a team starts
// by choosing a scenario, which offers its roles and preselects its usual
// members. The leader belongs to every team and is not listed here.
type Scenario struct {
	ID             string
	NameKey        string
	Name           string
	RoleIDs        []string
	DefaultRoleIDs []string
}

// Scenarios lists the built-in scenarios in display order. The first version
// only ships software development; later scenarios add entries here.
var Scenarios = []Scenario{
	{
		ID:             "software",
		NameKey:        "scenario.software.name",
		Name:           "Software development",
		RoleIDs:        []string{"product-manager", "frontend-dev", "backend-dev", "qa-engineer", "code-reviewer"},
		DefaultRoleIDs: []string{"frontend-dev", "backend-dev", "qa-engineer"},
	},
}

// ScenariosOf returns the IDs of the scenarios that offer a role.
func ScenariosOf(roleID string) []string {
	out := []string{}
	for _, scenario := range Scenarios {
		for _, id := range scenario.RoleIDs {
			if id == roleID {
				out = append(out, scenario.ID)
				break
			}
		}
	}
	return out
}
