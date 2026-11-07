package app

import "strings"

// What a turn said, read the same way for a member's turn and a chat's.

type loosePiece struct {
	text string
	work int
}

// looseReply is what a model that marks no phase said as its answer: the
// messages after the last work step. A turn that ended on a work step (the
// answer, then a hand-off call) answers with the last thing said.
func looseReply(pieces []loosePiece, work int) []string {
	var tail []string
	for _, piece := range pieces {
		if piece.work == work && strings.TrimSpace(piece.text) != "" {
			tail = append(tail, piece.text)
		}
	}
	if len(tail) == 0 {
		for i := len(pieces) - 1; i >= 0; i-- {
			if strings.TrimSpace(pieces[i].text) != "" {
				return []string{pieces[i].text}
			}
		}
	}
	return tail
}

// joinTexts keeps each reply segment of a turn (a note before the work, the
// final answer) its own paragraph.
func joinTexts(parts []string) string {
	return strings.Join(parts, "\n\n")
}
