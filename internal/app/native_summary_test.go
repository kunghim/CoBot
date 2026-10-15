package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kunghim/CoBot/internal/engine"
)

func TestNativeApprovalShowsCompleteOperationAndDirectory(t *testing.T) {
	req := engine.ApprovalRequest{Command: strings.Repeat("long argument ", 40) + "dangerous tail", Cwd: "selected-directory", Paths: []string{}}
	for i := 0; i < 20; i++ {
		req.Paths = append(req.Paths, strings.Repeat("path", i+1))
	}
	var got struct {
		Command string   `json:"command"`
		Cwd     string   `json:"cwd"`
		Paths   []string `json:"paths"`
	}
	if err := json.Unmarshal([]byte(approvalSummary(req)), &got); err != nil {
		t.Fatal(err)
	}
	if got.Command != req.Command || got.Cwd != req.Cwd || len(got.Paths) != len(req.Paths) {
		t.Fatal("an approval hid part of the authorized operation")
	}
}
