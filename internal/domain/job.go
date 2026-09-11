package domain

type BackupMode string

const (
	BackupModeFull        BackupMode = "full"
	BackupModeIncremental BackupMode = "incremental"
	BackupModeResume      BackupMode = "resume"
)

func (m BackupMode) Valid() bool {
	switch m {
	case BackupModeFull, BackupModeIncremental, BackupModeResume:
		return true
	default:
		return false
	}
}

type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobTransfer  JobStatus = "transfer"
	JobOrganize  JobStatus = "organize"
	JobIndex     JobStatus = "index"
	JobDone      JobStatus = "done"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

func (s JobStatus) Valid() bool {
	switch s {
	case JobQueued, JobTransfer, JobOrganize, JobIndex, JobDone, JobFailed, JobCancelled:
		return true
	default:
		return false
	}
}

func (s JobStatus) Terminal() bool {
	switch s {
	case JobDone, JobFailed, JobCancelled:
		return true
	default:
		return false
	}
}

type BackupJob struct {
	ID           string     `json:"id"`
	AccountID    string     `json:"account_id"`
	Mode         BackupMode `json:"mode"`
	Status       JobStatus  `json:"status"`
	BytesIn      int64      `json:"bytes_in"`
	SessionsDone int        `json:"sessions_done"`
	Error        string     `json:"error,omitempty"`
}

type RestoreSelectorKind string

const (
	RestoreAll        RestoreSelectorKind = "all"
	RestoreSessionIDs RestoreSelectorKind = "session_ids"
)

func (k RestoreSelectorKind) Valid() bool {
	switch k {
	case RestoreAll, RestoreSessionIDs:
		return true
	default:
		return false
	}
}

type RestoreSelector struct {
	Kind       RestoreSelectorKind `json:"kind"`
	SessionIDs []string            `json:"session_ids,omitempty"`
}

type RestoreJob struct {
	ID        string          `json:"id"`
	AccountID string          `json:"account_id"`
	Selector  RestoreSelector `json:"selector"`
	Status    JobStatus       `json:"status"`
	Error     string          `json:"error,omitempty"`
}
