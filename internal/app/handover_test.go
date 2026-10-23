package app

import (
	"strings"
	"testing"
)

func TestAQuotedReplyKeepsWhatComesNext(t *testing.T) {
	if got := quoteReply("做完了"); got != "做完了" {
		t.Fatalf("a short reply changed: %q", got)
	}
	long := "①和②都做完了。" + strings.Repeat("细节", 300) + "等你说继续再做③。"
	got := quoteReply(long)
	if !strings.HasPrefix(got, "①和②都做完了。") || !strings.HasSuffix(got, "等你说继续再做③。") {
		t.Fatalf("the start or the end was lost: %q", got)
	}
	if n := len([]rune(got)); n != lastReplyRunes+len([]rune("……")) {
		t.Fatalf("quoted %d runes, want %d", n, lastReplyRunes+2)
	}
}
