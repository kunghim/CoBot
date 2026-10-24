package app

import (
	"reflect"
	"testing"
)

// A model that marks no phase: the reply is what it said after its last piece
// of work, and what it said before was said on the way.
func TestTheReplyIsWhatIsSaidAfterTheLastWork(t *testing.T) {
	cases := map[string]struct {
		pieces []loosePiece
		work   int
		want   []string
	}{
		"said before and after the work": {
			[]loosePiece{{"我先看看", 0}, {"看完了，结果是 42", 1}}, 1, []string{"看完了，结果是 42"},
		},
		"said several things after the work": {
			[]loosePiece{{"开始", 0}, {"第一点", 1}, {"第二点", 1}}, 1, []string{"第一点", "第二点"},
		},
		"answered without any work": {
			[]loosePiece{{"好", 0}}, 0, []string{"好"},
		},
		"ended on work, the answer came before it": {
			[]loosePiece{{"先说一句", 0}, {"已完成，内容是 hello", 1}}, 2, []string{"已完成，内容是 hello"},
		},
		"nothing but blanks": {
			[]loosePiece{{"  ", 1}}, 1, nil,
		},
	}
	for name, tc := range cases {
		if got := looseReply(tc.pieces, tc.work); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: reply = %q, want %q", name, got, tc.want)
		}
	}
}

func TestOnlyDoingSomethingCountsAsWork(t *testing.T) {
	for kind, want := range map[string]bool{"command": true, "file_change": true, "tool": true, "web_search": true, "agent": true, "reasoning": false, "message": false, "diff": false, "plan": false, "compaction": false} {
		if isWorkStep(kind) != want {
			t.Errorf("%s: work = %v", kind, !want)
		}
	}
}
