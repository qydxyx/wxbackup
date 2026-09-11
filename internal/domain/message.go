package domain

import "time"

type Message struct {
	AccountID  string         `json:"account_id"`
	TalkerID   string         `json:"talker_id"`
	MsgID      string         `json:"msg_id"`
	MsgSeq     int64          `json:"msg_seq"`
	MsgType    int            `json:"msg_type"`
	IsSend     bool           `json:"is_send"`
	CreateTime time.Time      `json:"create_time"`
	Text       string         `json:"text"`
	XML        string         `json:"xml"`
	Extra      map[string]any `json:"extra,omitempty"`
}
