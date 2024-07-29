package hub

import (
	"encoding/json"
)

type (
	Message interface {
		ID() string
		Group() (string, string)
		User() (string, string)
		Type() int
		MsgTime() int64
		Message() string
		Marshal() ([]byte, error)
	}

	// BaseMessage 基础公共消息
	BaseMessage struct {
		MsgType   int    `json:"msgType"`
		Time      int64  `json:"time"`
		MsgID     string `json:"msgID"`
		GID       string `json:"gid,omitempty"`
		GroupName string `json:"groupName,omitempty"`
		UID       string `json:"uid,omitempty"`
		Username  string `json:"username,omitempty"`
	}
)

func (m *BaseMessage) ID() string {
	return m.MsgID
}

func (m *BaseMessage) Group() (string, string) {
	return m.GID, m.GroupName
}

func (m *BaseMessage) User() (string, string) {
	return m.UID, m.Username
}

func (m *BaseMessage) Type() int {
	return m.MsgType
}

func (m *BaseMessage) MsgTime() int64 {
	return m.Time
}

type (
	Quote struct {
		UID     string `json:"uid"`
		Name    string `json:"name"`
		Bot     bool   `json:"bot"`
		Content string `json:"content"`
	}

	At struct {
		UID    string `json:"uid"`
		Name   string `json:"name"`
		Bot    bool   `json:"bot"`
		Offset int    `json:"offset"`
		Length int    `json:"length"`
	}

	// TextMessage 文本消息
	TextMessage struct {
		BaseMessage
		Content string `json:"content"`
		Quote   *Quote `json:"quote,omitempty"`
		At      *At    `json:"at,omitempty"`
	}
)

func (m *TextMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *TextMessage) Message() string {
	type part struct {
		Content string `json:"content"`
		Quote   *Quote `json:"quote,omitempty"`
		At      *At    `json:"at,omitempty"`
	}
	content := part{
		Content: m.Content,
		Quote:   m.Quote,
		At:      m.At,
	}
	bytes, _ := json.Marshal(content)
	return string(bytes)
}

type (
	// Revoke 撤回信息
	Revoke struct {
		OldMsgID   string `json:"oldMsgID"`
		ReplaceMsg string `json:"replaceMsg"`
	}

	// RevokedMessage 撤回消息
	RevokedMessage struct {
		BaseMessage
		Revoke Revoke `json:"revoke"`
	}
)

func (m *RevokedMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *RevokedMessage) Message() string {
	bytes, _ := json.Marshal(m.Revoke)
	return string(bytes)
}

type (
	Media struct {
		Filename string `json:"filename"`
		Src      string `json:"src"`
		Size     string `json:"size"`
	}

	// MediaMessage 文件消息
	MediaMessage struct {
		BaseMessage
		Media Media `json:"media"`
	}
)

func (m *MediaMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *MediaMessage) Message() string {
	bytes, _ := json.Marshal(m.Media)
	return string(bytes)
}

type (
	SystemMessage struct {
		BaseMessage
		Event string `json:"event"`
		Data  any    `json:"data"`
	}
	EventRenameGroup struct {
		UID       string `json:"uid"`       // 用户id
		Name      string `json:"name"`      // 用户名
		GroupName string `json:"groupName"` // 新群名称
	}
	EventExitGroupUser struct {
		UID  string `json:"uid"`  // 用户id
		Name string `json:"name"` // 用户名
	}
)

func (m *SystemMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *SystemMessage) Message() string {
	type part struct {
		Event string `json:"event,omitempty"`
		Data  any    `json:"data,omitempty"`
	}
	content := part{
		Event: m.Event,
		Data:  m.Data,
	}
	bytes, _ := json.Marshal(content)
	return string(bytes)
}
