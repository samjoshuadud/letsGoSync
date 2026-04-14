package model

import "time"

type Assignment struct {
	Title         string `json:"title"`
	TitleNorm     string `json:"title_normalized"`
	RawTitle      string `json:"raw_title"`
	DueDate       string `json:"due_date"`
	OpeningDate   string `json:"opening_date"`
	Course        string `json:"course"`
	CourseCode    string `json:"course_code"`
	Status        string `json:"status"`
	TaskID        string `json:"task_id"`
	ActivityType  string `json:"activity_type"`
	Source        string `json:"source"`
	OriginURL     string `json:"origin_url"`
	AddedDate     string `json:"added_date"`
	LastUpdatedAt string `json:"last_updated"`
}

func (a Assignment) IsCompleted() bool {
	return a.Status == "Completed"
}

type SyncedTask struct {
	TaskID        string
	TodoistTaskID string
	SyncedAt      int64
	LastSyncedAt  int64
}

type SyncResult struct {
	Added   []string      `json:"added"`
	Updated []string      `json:"updated"`
	Skipped SkippedResult `json:"skipped"`
	Errors  []SyncError   `json:"errors"`
	Summary SyncSummary   `json:"summary"`
}

type SkippedResult struct {
	Local     []string `json:"local"`
	Todoist   []string `json:"todoist"`
	NoChanges []string `json:"noChanges"`
}

type SyncError struct {
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

type SyncSummary struct {
	Total     int `json:"total"`
	Processed int `json:"processed"`
	Failed    int `json:"failed"`
}

type LocalStats struct {
	AssignmentsByStatus map[string]int `json:"assignments_by_status"`
	TotalAssignments    int            `json:"total_assignments"`
	LastMergeAt         *time.Time     `json:"last_merge_at,omitempty"`
	LastSyncAt          *time.Time     `json:"last_sync_at,omitempty"`
}
