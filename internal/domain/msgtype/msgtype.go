// Package msgtype is the canonical viewer catalog for Message.MsgType.
//
// Integers 1, 3, 34, 43, and 10000 match testdata/fixtures. Other codes are
// this rebuild's catalog IDs so each renderer has a stable integer. Extra["kind"]
// or Extra["app_type"] may refine a generic App (49) row.
package msgtype

import (
	"encoding/json"
	"strconv"
)

type Code int

const (
	Unknown     Code = 0
	Text        Code = 1
	Image       Code = 3
	Voice       Code = 34
	Card        Code = 42
	Video       Code = 43
	Emoji       Code = 47
	Location    Code = 48
	App         Code = 49
	AVCall      Code = 50
	File        Code = 1001
	Link        Code = 1002
	MiniProgram Code = 1003
	GIF         Code = 1004
	Quote       Code = 1005
	Merge       Code = 1006
	RedPacket   Code = 1007
	Transfer    Code = 1008
	Channels    Code = 1009
	Live        Code = 1010
	GroupChain  Code = 1011
	GroupNotice Code = 1012
	Gift        Code = 1013
	OANotify    Code = 1014
	System      Code = 10000
)

type Info struct {
	Code      Code
	Kind      string
	Label     string
	Preview   string
	Supported bool
}

var catalog = []Info{
	{Text, "text", "文本", "", true},
	{Image, "image", "图片", "[图片]", true},
	{Video, "video", "视频", "[视频]", true},
	{Voice, "voice", "语音", "[语音]", true},
	{File, "file", "文件", "[文件]", true},
	{Link, "link", "链接", "[链接]", true},
	{MiniProgram, "miniprogram", "小程序", "[小程序]", true},
	{Card, "card", "名片", "[名片]", true},
	{Location, "location", "位置", "[位置]", true},
	{GIF, "gif", "GIF", "[GIF]", true},
	{Emoji, "emoji", "表情", "[表情]", true},
	{Quote, "quote", "引用", "[引用]", true},
	{Merge, "merge", "合并转发", "[聊天记录]", true},
	{RedPacket, "redpacket", "红包", "[红包]", true},
	{Transfer, "transfer", "转账", "[转账]", true},
	{System, "system", "系统消息", "[系统]", true},
	{Channels, "channels", "视频号", "[视频号]", true},
	{Live, "live", "视频号直播", "[视频号直播]", true},
	{GroupChain, "groupchain", "群接龙", "[群接龙]", true},
	{GroupNotice, "groupnotice", "群公告", "[群公告]", true},
	{Gift, "gift", "礼物", "[礼物]", true},
	{OANotify, "oanotify", "公众号通知", "[公众号通知]", false},
	{AVCall, "avcall", "音视频通话", "[音视频通话]", false},
	{App, "app", "应用消息", "[消息]", false},
	{Unknown, "unknown", "未知消息", "[消息]", false},
}

var byCode = map[Code]Info{}
var byKind = map[string]Info{}

func init() {
	for _, info := range catalog {
		byCode[info.Code] = info
		byKind[info.Kind] = info
	}
}

func (c Code) Valid() bool {
	_, ok := byCode[c]
	return ok
}

func (c Code) Info() Info {
	if info, ok := byCode[c]; ok {
		return info
	}
	return byCode[Unknown]
}

func (c Code) Kind() string      { return c.Info().Kind }
func (c Code) Label() string     { return c.Info().Label }
func (c Code) Preview() string   { return c.Info().Preview }
func (c Code) Supported() bool   { return c.Info().Supported }
func (c Code) Placeholder() bool { return !c.Supported() }

func Catalog() []Info {
	out := make([]Info, len(catalog))
	copy(out, catalog)
	return out
}

// Parse maps a stored msg_type plus optional Extra fields onto a catalog code.
func Parse(msgType int, extra map[string]any) Code {
	if extra != nil {
		if kind, ok := extraString(extra, "kind"); ok {
			if info, found := byKind[kind]; found {
				return info.Code
			}
		}
		if n, ok := extraInt(extra, "app_type"); ok {
			c := Code(n)
			if c != App && c.Valid() {
				return c
			}
		}
	}
	c := Code(msgType)
	if c.Valid() {
		return c
	}
	return Unknown
}

func extraString(extra map[string]any, key string) (string, bool) {
	v, ok := extra[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	if s == "" {
		return "", false
	}
	return s, true
}

func extraInt(extra map[string]any, key string) (int, bool) {
	v, ok := extra[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	default:
		return 0, false
	}
}
