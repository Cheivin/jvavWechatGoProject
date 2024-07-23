package hub

import "encoding/json"

type (
	Message interface {
		ID() string
		Marshal() ([]byte, error)
	}

	BaseMessage struct {
		MsgType    int    `json:"msgType"`
		Time       int64  `json:"time"`
		MsgID      string `json:"msgID"`
		GID        string `json:"gid,omitempty"`
		GroupName  string `json:"groupName,omitempty"`
		UID        string `json:"uid"`
		Username   string `json:"username"`
		RawMessage string `json:"rawMessage,omitempty"`
	}
)

type (
	// TextMessage 文本消息
	TextMessage struct {
		BaseMessage
		Content string `json:"content"`
		Quote   Quote  `json:"quote"`
	}

	// Quote 引用信息
	Quote struct {
		UID      string `json:"uid"`
		Username string `json:"username"`
		Quote    string `json:"quote"`
	}

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

	Media struct {
		Filename string `json:"filename"`
		Src      string `json:"src"`
		Size     string `json:"size"`
	}

	MediaMessage struct {
		BaseMessage
		Media Media `json:"media"`
	}
)

type (
	User struct {
		UID        string `json:"uid"`
		Nickname   string `json:"nickname"`
		WechatName string `json:"wechatName"`
		AttrStatus int64  `json:"attrStatus"`
	}
	Group struct {
		GID       string `json:"gid"`
		GroupName string `json:"groupName"`
	}
	GroupUser struct {
		Group
		User
		GroupNickname string `json:"groupNickname,omitempty"`
	}
)

func (m *BaseMessage) ID() string {
	return m.MsgID
}

func (m *BaseMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *TextMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *RevokedMessage) Marshal() ([]byte, error) {
	m.RawMessage = ""
	return json.Marshal(m)
}
func (m *MediaMessage) Marshal() ([]byte, error) {
	m.RawMessage = ""
	return json.Marshal(m)
}

type MessageRedirect interface {
	Redirect(message Message) error
	RedirectBytes([]byte) error
}
type OnMessage func(message []byte)
