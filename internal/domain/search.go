package domain

import "time"

// Conversation search fills the compact search field at the top of the app:
// each hit names the conversation to open, not a single message. Results are
// newest first, a few per group, and archived or deleted conversations never
// appear.
type SearchResults struct {
	Query   string               `json:"query"`
	Chats   []ChatSearchResult   `json:"chats"`
	Members []MemberSearchResult `json:"members"`
}

// ChatSearchResult is an unarchived chat whose title or message text matched.
// Snippet holds the excerpt of the latest matching message; a title-only
// match leaves it empty.
type ChatSearchResult struct {
	ChatID     string    `json:"chatId"`
	Title      string    `json:"title"`
	Snippet    string    `json:"snippet"`
	ActivityAt time.Time `json:"activityAt"`
}

// MemberSearchResult is a live team member whose conversation matched; the
// snippet is the excerpt of that member's latest matching message.
type MemberSearchResult struct {
	TeamID     string    `json:"teamId"`
	TeamName   string    `json:"teamName"`
	BotID      string    `json:"botId"`
	BotName    string    `json:"botName"`
	Snippet    string    `json:"snippet"`
	ActivityAt time.Time `json:"activityAt"`
}
