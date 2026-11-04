package collaboration

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/tools.golden.json")

// TestWhatTheModelSeesIsPinned: the tools each role is offered, in order, and
// what the model reads about them are product behavior. A change here is a
// change to what every member can do, on purpose or not.
func TestWhatTheModelSeesIsPinned(t *testing.T) {
	type tool struct {
		Name        string          `json:"name"`
		SubAgent    bool            `json:"subAgent"`
		SideEffect  bool            `json:"sideEffect"`
		Description string          `json:"description"`
		Schema      json.RawMessage `json:"schema"`
	}
	roles := map[string][]tool{}
	for _, role := range []string{RoleLeader, RoleMember} {
		for _, name := range ToolsForRole(role) {
			roles[role] = append(roles[role], tool{
				Name: name, SubAgent: toolByName[name].SubAgentOK, SideEffect: IsSideEffectTool(name),
				Description: toolByName[name].Description, Schema: toolByName[name].Schema(),
			})
		}
	}
	var got bytes.Buffer
	encoder := json.NewEncoder(&got)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(roles); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "tools.golden.json")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got.Bytes()) {
		t.Fatalf("what the model sees changed; if on purpose, run with -update-golden and say so in the PR:\n%s", got.String())
	}
}
