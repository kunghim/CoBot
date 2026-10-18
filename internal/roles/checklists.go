package roles

import (
	"embed"
	"strings"
)

// ReviewerRoleID is the role whose members review the deliveries of tasks the
// leader marks for review; their verdict decides the task.
const ReviewerRoleID = "code-reviewer"

//go:embed checklists/*.md
var checklistsFS embed.FS

// ReviewTypes lists the kinds of review a task can ask for, each with its
// official checklist. The reviewer reads the checklist with the review
// request, so its own instructions stay short.
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
