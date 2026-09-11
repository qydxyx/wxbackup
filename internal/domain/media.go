package domain

type MediaKind string

const (
	MediaKindImage MediaKind = "image"
	MediaKindVideo MediaKind = "video"
	MediaKindVoice MediaKind = "voice"
	MediaKindFile  MediaKind = "file"
	MediaKindOther MediaKind = "other"
)

func (k MediaKind) Valid() bool {
	switch k {
	case MediaKindImage, MediaKindVideo, MediaKindVoice, MediaKindFile, MediaKindOther:
		return true
	default:
		return false
	}
}

type MediaObject struct {
	AccountID string    `json:"account_id"`
	MediaID   string    `json:"media_id"`
	Kind      MediaKind `json:"kind"`
	SHA256    string    `json:"sha256"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Available bool      `json:"available"`
}
