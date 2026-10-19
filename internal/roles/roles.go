// Package roles embeds the first-version role library, materializes an
// editable copy into the profile and loads the user's current versions.
// Role files are instructions, never permission configuration: editing them
// cannot grant approvals or cross-team capabilities.
package roles

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

//go:embed defaults/*.json
var defaultsFS embed.FS

// Role is one entry of the role library.
type Role struct {
	SchemaVersion  int    `json:"schemaVersion"`
	ID             string `json:"id"`
	NameKey        string `json:"nameKey"`
	Name           string `json:"name"`
	Instructions   string `json:"instructions"`
	ContentVersion string `json:"contentVersion"`
}

// LeaderRoleID is the coordination role every team receives at creation.
const LeaderRoleID = "leader"

// FullstackRoleID builds the whole change, interface and server alike; it
// replaced the separate frontend and backend roles (migration 00022).
const FullstackRoleID = "fullstack-engineer"

// DefaultIDs lists the embedded library in creation order.
var DefaultIDs = []string{
	LeaderRoleID,
	"product-manager",
	FullstackRoleID,
	"qa-engineer",
	"code-reviewer",
}

// Defaults returns the embedded read-only library with content digests.
func Defaults() ([]Role, error) {
	roles := make([]Role, 0, len(DefaultIDs))
	for _, id := range DefaultIDs {
		data, err := fs.ReadFile(defaultsFS, "defaults/"+fileNameFor(id)+".json")
		if err != nil {
			return nil, fmt.Errorf("read default role %s: %w", id, err)
		}
		var role Role
		if err := json.Unmarshal(data, &role); err != nil {
			return nil, fmt.Errorf("decode default role %s: %w", id, err)
		}
		if role.ID != id {
			return nil, fmt.Errorf("default role file %s declares id %q", id, role.ID)
		}
		role.ContentVersion = Digest(role.Instructions)
		roles = append(roles, role)
	}
	return roles, nil
}

// Default returns one embedded role by id.
func Default(id string) (Role, error) {
	all, err := Defaults()
	if err != nil {
		return Role{}, err
	}
	for _, role := range all {
		if role.ID == id {
			return role, nil
		}
	}
	return Role{}, fmt.Errorf("unknown default role %q", id)
}

// Digest is the content version of role instructions.
func Digest(instructions string) string {
	sum := sha256.Sum256([]byte(instructions))
	return hex.EncodeToString(sum[:8])
}

// retiredDigests lists the content versions of earlier shipped defaults. A
// profile copy still carrying one of them was never edited by the user, so it
// is upgraded to the current default instead of pinning the old text forever.
var retiredDigests = map[string][]string{
	LeaderRoleID:      {"d4ff9b5d42b58330", "47096f47252a1cbe", "3b5aa00ad45e47af", "ae14e84830ebad5d", "38bc6f07efd942c9", "92a6bd4169037e83"},
	"code-reviewer":   {"356c52217a6c921b", "db1b0c37c4b47538", "032439ba35ed412c", "4289268f7df53b82"},
	"product-manager": {"04df35093d7484bc", "d7b88a88385bccf8"},
	"qa-engineer":     {"8d36dda792696699", "29b8207cd15a0e6e", "9670ddfd14423239"},
	FullstackRoleID:   {"91426b923693aba4"},
}

// EnsureProfileCopy materializes the editable role files on first use.
// User-edited files are never overwritten; new default roles are added and
// unedited copies of retired defaults are upgraded.
func EnsureProfileCopy(rolesDir string) error {
	if err := os.MkdirAll(rolesDir, 0o700); err != nil {
		return fmt.Errorf("create roles dir: %w", err)
	}
	defaults, err := Defaults()
	if err != nil {
		return err
	}
	for _, role := range defaults {
		path := filepath.Join(rolesDir, fileNameFor(role.ID)+".json")
		if _, err := os.Stat(path); err == nil && !isRetiredCopy(path, role.ID) {
			continue // user copy wins
		}
		if err := writeRoleFile(path, role); err != nil {
			return err
		}
	}
	return nil
}

// LoadProfile reads the user's current role files from the profile.
func LoadProfile(rolesDir string) ([]Role, error) {
	entries, err := os.ReadDir(rolesDir)
	if err != nil {
		return nil, fmt.Errorf("read roles dir: %w", err)
	}
	var roles []Role
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(rolesDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read role %s: %w", entry.Name(), err)
		}
		var role Role
		if err := json.Unmarshal(data, &role); err != nil {
			return nil, fmt.Errorf("decode role %s: %w", entry.Name(), err)
		}
		if role.SchemaVersion != 1 || role.ID == "" || role.Instructions == "" {
			return nil, fmt.Errorf("role %s is missing required fields or schemaVersion!=1", entry.Name())
		}
		role.ContentVersion = Digest(role.Instructions)
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].ID < roles[j].ID })
	return roles, nil
}

func isRetiredCopy(path, id string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var role Role
	if err := json.Unmarshal(data, &role); err != nil {
		return false
	}
	digest := Digest(role.Instructions)
	for _, retired := range retiredDigests[id] {
		if digest == retired {
			return true
		}
	}
	return false
}

func writeRoleFile(path string, role Role) error {
	data, err := json.MarshalIndent(role, "", "  ")
	if err != nil {
		return fmt.Errorf("encode role %s: %w", role.ID, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write role %s: %w", role.ID, err)
	}
	return nil
}

func fileNameFor(id string) string {
	switch id {
	case LeaderRoleID:
		return "leader"
	case "product-manager":
		return "product_manager"
	case FullstackRoleID:
		return "fullstack_engineer"
	case "qa-engineer":
		return "qa_engineer"
	case "code-reviewer":
		return "code_reviewer"
	default:
		return id
	}
}
