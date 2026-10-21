package app

import "testing"

func TestReadablePathDropsTheExtendedLengthPrefix(t *testing.T) {
	if got := readablePath(`\\?\E:\data\rollout.jsonl`); got != `E:\data\rollout.jsonl` {
		t.Fatalf("got %q", got)
	}
	if got := readablePath(`/home/u/rollout.jsonl`); got != `/home/u/rollout.jsonl` {
		t.Fatalf("got %q", got)
	}
}
