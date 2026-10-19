package roles

import (
	"embed"
	"strings"
)

//go:embed checklists/*.md
var checklistsFS embed.FS

// ReviewTypes lists the kinds of review a stage can be, each with its
// official checklist. The reviewer reads the checklist on the review
// card, so its own instructions stay short.
var ReviewTypes = []string{"requirements", "design", "test_plan", "code", "general"}

// ValidReviewType reports whether a review type has a checklist.
func ValidReviewType(reviewType string) bool {
	for _, known := range ReviewTypes {
		if known == reviewType {
			return true
		}
	}
	return false
}

// Checklist returns the official checklist of a review type, or "" for an
// unknown one.
func Checklist(reviewType string) string {
	if !ValidReviewType(reviewType) {
		return ""
	}
	data, err := checklistsFS.ReadFile("checklists/" + reviewType + ".md")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
