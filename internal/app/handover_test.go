package app

import (
	"strings"
	"testing"
)

// Code mode is OpenAI's; another vendor's model told its spelling joins
// the namespace into the tool's name, which the engine refuses.
func TestOnlyCodeModeIsToldTheCodeModeSpelling(t *testing.T) {
	code, plain := toolGuide("member", true), toolGuide("member", false)
	if !strings.Contains(code, "tools.mcp__cobot__<名字>") {
		t.Fatalf("code mode lost its spelling:\n%s", code)
	}
	if strings.Contains(plain, "tools.mcp__cobot__") || strings.Contains(plain, "工具名是 mcp__cobot__") {
		t.Fatalf("a plain-call model is told the code-mode spelling:\n%s", plain)
	}
	if !strings.Contains(plain, "mcp__cobot 这个命名空间") || !strings.Contains(plain, "不要通过 exec") {
		t.Fatalf("a plain-call model is not told how to call:\n%s", plain)
	}
}

func TestAQuotedReplyNamesToolsWithoutTheirNamespace(t *testing.T) {
	got := joinedToolName.ReplaceAllString("`mcp__cobot__browser` 返回 unsupported call；text(await tools.mcp__cobot__handoff({}))", "$1")
	if got != "`browser` 返回 unsupported call；text(await handoff({}))" {
		t.Fatalf("got %q", got)
	}
}

func TestReadablePathDropsTheExtendedLengthPrefix(t *testing.T) {
	if got := readablePath(`\\?\E:\data\rollout.jsonl`); got != `E:\data\rollout.jsonl` {
		t.Fatalf("got %q", got)
	}
	if got := readablePath(`/home/u/rollout.jsonl`); got != `/home/u/rollout.jsonl` {
		t.Fatalf("got %q", got)
	}
}
