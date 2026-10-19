package domain

import (
	"encoding/json"
	"regexp"
)

const (
	// AvatarKindCatalog is an avatar drawn from a composed catalog series.
	AvatarKindCatalog = "catalog"
	// AvatarKindUpload is a picture the user picked on this computer, kept
	// in the data root and never sent anywhere.
	AvatarKindUpload = "upload"
)

// AvatarRef is the avatar a user chose for a member or a role: parts of a
// catalog series, or a local picture by its file name. It never carries the
// image itself; a part or file that is gone falls back to the default.
type AvatarRef struct {
	Kind   string `json:"kind"`
	Series string `json:"series,omitempty"`
	Shape  string `json:"shape,omitempty"`
	Eyes   string `json:"eyes,omitempty"`
	Color  string `json:"color,omitempty"`
	File   string `json:"file,omitempty"`
}

// UploadFileName is how a local picture is stored: its content's sha256.
var UploadFileName = regexp.MustCompile(`^[0-9a-f]{64}\.png$`)

var (
	avatarName  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	avatarColor = regexp.MustCompile(`^#[0-9a-f]{6}$`)
)

// Validate checks the reference is well formed; whether the catalog has the
// parts is the interface's concern, which falls back when it does not.
func (r AvatarRef) Validate() error {
	if r.Kind == AvatarKindUpload {
		if !UploadFileName.MatchString(r.File) || r.Series != "" || r.Shape != "" || r.Eyes != "" || r.Color != "" {
			return InvalidRequest("error.avatar.invalid", map[string]any{"field": "file"})
		}
		return nil
	}
	if r.Kind != AvatarKindCatalog || r.File != "" {
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
