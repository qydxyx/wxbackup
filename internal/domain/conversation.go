package domain

import "time"

type ConversationKind string

const (
	ConversationFriend ConversationKind = "friend"
	ConversationGroup  ConversationKind = "group"
	ConversationOA     ConversationKind = "oa"
	ConversationWecom  ConversationKind = "wecom"
)

func (k ConversationKind) Valid() bool {
	switch k {
	case ConversationFriend, ConversationGroup, ConversationOA, ConversationWecom:
		return true
	default:
		return false
	}
}

type Conversation struct {
	AccountID   string           `json:"account_id"`
	TalkerID    string           `json:"talker_id"`
	Kind        ConversationKind `json:"kind"`
	DisplayName string           `json:"display_name"`
	Avatar      string           `json:"avatar"`
	LastMsgTime time.Time        `json:"last_msg_time"`
	MsgCount    int64            `json:"msg_count"`
}
