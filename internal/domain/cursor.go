package domain

import "time"

// BackupCursor is the per-talker resume point for incremental/resume jobs.
type BackupCursor struct {
	AccountID   string    `json:"account_id"`
	TalkerID    string    `json:"talker_id"`
	LastEndTime time.Time `json:"last_end_time"`
	SegmentMeta string    `json:"segment_meta"`
	Received    int64     `json:"received"`
	Total       int64     `json:"total"`
}
