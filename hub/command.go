package hub

type (
	Command struct {
		Command string         `json:"command"` // SendMsg:发送消息
		Param   SendMsgCommand `json:"param"`
	}

	SendMsgCommand struct {
		Gid      string `json:"gid" form:"gid"`           // 群id
		Type     int    `json:"type" form:"type"`         // 回复类型 1:文本,2:图片,3:视频,4:文件
		Body     string `json:"body" form:"body"`         // 回复内容,type=1时为文本内容,type=2/3/4时为资源地址
		Filename string `json:"filename" form:"filename"` // 文件名称
		Prompt   string `json:"prompt" form:"prompt"`     // 回复提示
	}
)
