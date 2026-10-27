package domain

import "time"

type WorkspaceChangeFile struct {
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	MovePath      string `json:"movePath,omitempty"`
	Additions     int    `json:"additions"`
	Deletions     int    `json:"deletions"`
	StatsComplete bool   `json:"statsComplete"`
	Binary        bool   `json:"binary"`
	DiffTruncated bool   `json:"diffTruncated"`
}

type WorkspaceChangeRun struct {
	ID            string    `json:"id"`
	BotID         string    `json:"botId"`
	Status        string    `json:"status"`
	StartedAt     time.Time `json:"startedAt"`
	EndedAt       time.Time `json:"endedAt"`
	Source        string    `json:"source"`
	FileCount     int       `json:"fileCount"`
	FilesComplete bool      `json:"filesComplete"`
	Additions     int       `json:"additions"`
	Deletions     int       `json:"deletions"`
	StatsComplete bool      `json:"statsComplete"`
}

type WorkspaceChangeRunsPage struct {
	Runs       []WorkspaceChangeRun `json:"runs"`
	NextCursor string               `json:"nextCursor"`
}

type WorkspaceChangeFilesPage struct {
	Run        WorkspaceChangeRun    `json:"run"`
	Files      []WorkspaceChangeFile `json:"files"`
	NextCursor string                `json:"nextCursor"`
}

type WorkspaceChangeDiff struct {
	Run           WorkspaceChangeRun  `json:"run"`
	File          WorkspaceChangeFile `json:"file"`
	Diff          string              `json:"diff"`
	DiffTruncated bool                `json:"diffTruncated"`
}
