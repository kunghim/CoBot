package domain

import (
	"encoding/json"
	"regexp"
)

// AvatarKindCatalog is an avatar drawn from a composed catalog series.
const AvatarKindCatalog = "catalog"

// AvatarRef is the avatar a user chose for a member or a role: which parts
// of which catalog series. It names parts and never carries an image, so a
// newer catalog still draws it, and a part it no longer has falls back.
type AvatarRef struct {
	Kind   string `json:"kind"`
	Series string `json:"series"`
	Shape  string `json:"shape"`
	Eyes   string `json:"eyes"`
	Color  string `json:"color"`
}

var (
	avatarName  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	avatarColor = regexp.MustCompile(`^#[0-9a-f]{6}$`)
)

// Validate checks the reference is well formed; whether the catalog has the
// parts is the interface's concern, which falls back when it does not.
func (r AvatarRef) Validate() error {
	if r.Kind != AvatarKindCatalog {
		return InvalidRequest("error.avatar.invalid", map[string]any{"field": "kind"})
	}
	for field, value := range map[string]string{"series": r.Series, "shape": r.Shape, "eyes": r.Eyes} {
		if !avatarName.MatchString(value) {
			return InvalidRequest("error.avatar.invalid", map[string]any{"field": field})
		}
	}
	if !avatarColor.MatchString(r.Color) {
		return InvalidRequest("error.avatar.invalid", map[string]any{"field": "color"})
	}
	return nil
}

// EncodeAvatar stores a reference; nil stores "" (follow the default).
func EncodeAvatar(ref *AvatarRef) string {
	if ref == nil {
		return ""
	}
	data, _ := json.Marshal(ref)
	return string(data)
}

// DecodeAvatar reads a stored reference; "" or anything unreadable is nil.
func DecodeAvatar(stored string) *AvatarRef {
	if stored == "" {
		return nil
	}
	var ref AvatarRef
	if json.Unmarshal([]byte(stored), &ref) != nil || ref.Validate() != nil {
		return nil
	}
	return &ref
}
