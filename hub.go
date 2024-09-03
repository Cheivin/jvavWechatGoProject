package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/robfig/cron/v3"
	"golang.org/x/time/rate"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	authManager "wechat-hub/auth"
	"wechat-hub/hub"
	"wechat-hub/redirect"
	"wechat-hub/storage"
)

type Hub struct {
	ctx       context.Context
	member    hub.MemberManager
	message   hub.MessageManager
	storage   storage.Storage
	sender    *MsgSender
	auth      *authManager.Manager
	limit     *rate.Limiter
	redirects []redirect.MessageRedirector
}

func NewHub(ctx context.Context, member hub.MemberManager, message hub.MessageManager, storage storage.Storage, auth *authManager.Manager) *Hub {
	return &Hub{
		ctx:       ctx,
		member:    member,
		message:   message,
		storage:   storage,
		auth:      auth,
		limit:     rate.NewLimiter(rate.Every(10*time.Second), 1),
		redirects: []redirect.MessageRedirector{},
	}
}

func (h *Hub) SetMessageSender(sender *MsgSender) {
	h.sender = sender
}

// AddRedirect 添加一个转发器
func (h *Hub) AddRedirect(redirect redirect.MessageRedirector) {
	h.redirects = append(h.redirects, redirect)
}

// UseWebsocketClientRedirect 使用websocket客户端转发
func (h *Hub) UseWebsocketClientRedirect(serverUrl string, heartbeat time.Duration) {
	h.AddRedirect(redirect.NewWebsocketClientMessageHandler(h.ctx, serverUrl, redirect.WSClientHeartbeat(heartbeat)))
}

// UseWebsocketServerRedirect 使用websocket服务端转发
func (h *Hub) UseWebsocketServerRedirect(port int, heartbeat time.Duration) {
	server := redirect.NewWebsocketServerMessageHandler(h.ctx, redirect.WSServerHeartbeat(heartbeat), redirect.WSServerAuth(h.auth))
	server.OnMessage(h.receive)
	go server.ListenAndServe(port)
	h.AddRedirect(server)
}

func (h *Hub) UseMQTTServerRedirect(cacheDir string, tcpPort int, wsPort int) {
	options := []redirect.MQTTOption{
		redirect.WithSubscribeTopic("command"), redirect.WithMQTTAuth(h.auth),
		redirect.WithTCP(tcpPort),
	}
	if wsPort > 0 {
		options = append(options, redirect.WithWS(wsPort))
	}
	server := redirect.NewMQTTServerMessageHandler(cacheDir, "message", options...)
	server.OnMessage(h.receive)
	go server.ListenAndServe()
	h.AddRedirect(server)
}

// dispatch 用于将组装好的消息下发给转发器
func (h *Hub) dispatch(message hub.Message) {
	// 消息id不为空的才需要保存
	if message.ID() != "" {
		if err := h.message.Save(message); err != nil {
			slog.Error("消息保存失败", "msgId", message.ID(), "err", err)
		}
	}
	marshal, err := message.Marshal()
	if err != nil {
		slog.Error("消息序列化失败", "msgId", message.ID(), "err", err)
		return
	}
	for _, r := range h.redirects {
		go func(redirect redirect.MessageRedirector) {
			if err := redirect.SendMessage(marshal); err != nil {
				slog.Error("消息转发失败", "msgId", message.ID(), "err", err)
			}
		}(r)
	}
}

// 接受转发器上报的消息
func (h *Hub) receive(message []byte, from string, id string) (err error) {
	defer func() {
		if e := recover(); e != nil {
			slog.Error("处理上报消息出错", "receiver", from, "id", id, "Error", e)
			err = fmt.Errorf("panic: %v", e)
		}
	}()
	if h.sender == nil {
		slog.Debug("收到上报消息", "message", string(message), "receiver", from, "id", id)
		return
	}
	command := &hub.Command{}
	if err = json.Unmarshal(message, command); err != nil {
		slog.Error("命令消息解析失败", "message", string(message), "receiver", from, "id", id, "err", err)
		return
	}
	if command.Command != "sendMessage" {
		slog.Error("不支持的命令", "command", command.Command, "param", command.Param, "receiver", from, "id", id)
		return
	}
	err = h.sender.SendMsg(&command.Param)
	if err != nil {
		slog.Error("消息发送失败", "receiver", from, "id", id, "err", err)
	}
	return
}

// Register 注册消息监听器
func (h *Hub) Register(dispatcher *openwechat.MessageMatchDispatcher) {
	dispatcher.RegisterHandler(h.messageFilter())
	dispatcher.OnGroup(h.onSystemMessage, h.onMedia)
	dispatcher.OnText(h.onText)
	dispatcher.OnRecalled(h.onRecalled)
}

func (h *Hub) messageFilter() (matchFunc openwechat.MatchFunc, handlers openwechat.MessageContextHandler) {
	return func(_ *openwechat.Message) bool {
			return true
		}, func(ctx *openwechat.MessageContext) {
			if ctx.IsNotify() || ctx.IsSendBySelf() {
				ctx.Abort()
			}
			if exist, _ := h.message.Exist(ctx.MsgId); exist {
				slog.Warn("消息重复", "msgId", ctx.MsgId, "msgType", ctx.MsgType)
				ctx.Abort()
			}
		}
}

func (h *Hub) prepareMessage(ctx *openwechat.MessageContext) *hub.BaseMessage {
	var gid, groupName, uid, username string
	if ctx.IsSendByGroup() {
		g, _ := ctx.Sender()
		if id, err := h.member.GetID(g); err != nil {
			slog.Error("获取群组ID失败", "msgId", ctx.MsgId, "err", err)
			return nil
		} else {
			groupName = g.NickName
			gid = id
		}
		u, _ := ctx.SenderInGroup()
		if id, err := h.member.GetID(u); err != nil {
			slog.Error("获取用户ID失败", "msgId", ctx.MsgId, "err", err)
			return nil
		} else {
			uid = id
			username = u.NickName
		}
	} else {
		u, _ := ctx.Sender()
		if id, err := h.member.GetID(u); err != nil {
			slog.Error("获取用户ID失败", "msgId", ctx.MsgId, "err", err)
			return nil
		} else {
			uid = id
			username = u.NickName
		}
	}
	return &hub.BaseMessage{
		MsgType:   int(ctx.MsgType),
		Time:      ctx.CreateTime,
		MsgID:     ctx.MsgId,
		GID:       gid,
		GroupName: groupName,
		UID:       uid,
		Username:  username,
	}
}

func (h *Hub) prepareSystemMessage(ctx *openwechat.MessageContext) *hub.SystemMessage {
	var gid, groupName, uid, username string
	if ctx.IsSendByGroup() {
		g, _ := ctx.Sender()
		if id, err := h.member.GetID(g); err != nil {
			slog.Error("获取群组ID失败", "msgId", ctx.MsgId, "err", err)
			return nil
		} else {
			groupName = g.NickName
			gid = id
		}
	} else {
		u, _ := ctx.Sender()
		if id, err := h.member.GetID(u); err != nil {
			slog.Error("获取用户ID失败", "msgId", ctx.MsgId, "err", err)
			return nil
		} else {
			uid = id
			username = u.NickName
		}
	}
	return &hub.SystemMessage{
		BaseMessage: hub.BaseMessage{
			MsgType:   int(openwechat.MsgTypeSys),
			Time:      ctx.CreateTime,
			MsgID:     ctx.MsgId,
			GID:       gid,
			GroupName: groupName,
			UID:       uid,
			Username:  username,
		},
	}
}

// onSystemMessage 系统消息解析
func (h *Hub) onSystemMessage(ctx *openwechat.MessageContext) {
	if !ctx.IsSystem() {
		return
	}
	if username, groupName, ok := isRenameGroup(ctx.Content); ok {
		defer ctx.Abort()

		sender, _ := ctx.Sender()
		event := &hub.EventRenameGroup{
			Name:      username,
			GroupName: groupName,
		}
		user := sender.MemberList.Search(1, searchByName(username))
		if user != nil && user.First() != nil {
			id, err := h.member.GetID(user.First())
			if err != nil {
				slog.Error("获取用户ID失败", "msgId", ctx.MsgId, "err", err)
			} else {
				event.UID = id
			}
		}
		slog.Info("检测到群名修改", "user", username, "groupName", groupName)
		h.member.RefreshGroupMember()
		message := h.prepareSystemMessage(ctx)
		message.Event = "RenameGroup"
		message.Data = event
		h.dispatch(message)
		// 设置为已读
		if h.limit.Allow() {
			_ = ctx.AsRead()
		}
	}

}

// onText 转发文字消息
func (h *Hub) onText(ctx *openwechat.MessageContext) {
	defer ctx.Abort()
	message := h.prepareMessage(ctx)
	if message == nil {
		return
	}

	msgContent := strings.TrimSpace(openwechat.FormatEmoji(ctx.Content))

	var quote *hub.Quote
	var at *hub.At
	if ctx.IsSendByGroup() {
		sender, _ := ctx.Sender()

		// 解析引用消息部分
		if quoteContent, pureContent, separator, ok := getQuote(msgContent); ok {
			msgContent = pureContent

			username, user := getUserFromContent[openwechat.Members](quoteContent, separator, func(name string) (openwechat.Members, bool) {
				searched := sender.MemberList.Search(1, searchByName(name))
				return searched, searched != nil && searched.First() != nil
			})
			if user != nil && user.First() != nil && username != "" {
				id, err := h.member.GetID(user.First())
				if err != nil {
					slog.Error("获取用户ID失败", "msgId", ctx.MsgId, "err", err)
				} else {
					quoteContent = strings.TrimPrefix(quoteContent, username+separator)
					quote = &hub.Quote{
						UID:     id,
						Name:    username,
						Bot:     user.First().UserName == ctx.Owner().UserName,
						Content: quoteContent,
					}
				}
			}
		}

		// 解析AT消息部分
		var atName string
		var receiver openwechat.Members

		if ctx.IsAt() { // 是否AT机器人的
			receiver = sender.MemberList.SearchByUserName(1, ctx.ToUserName)
			if receiver != nil {
				displayName := receiver.First().DisplayName
				if displayName == "" {
					displayName = receiver.First().NickName
				}
				atName = openwechat.FormatEmoji(displayName)
			}
		} else if strings.Contains(msgContent, "@") { // 是否AT其他人的
			atPos := strings.Index(msgContent, "@")
			u2005Pos := strings.Index(msgContent[atPos:], "\u2005")
			if u2005Pos >= 0 { // 取特殊标记中间部分
				atName = strings.TrimSpace(msgContent[atPos+1 : atPos+u2005Pos])
				receiver = sender.MemberList.Search(1, searchByName(atName))
			} else {
				atName, receiver = getUserFromContent[openwechat.Members](msgContent[atPos+1:], " ", func(name string) (openwechat.Members, bool) {
					searched := sender.MemberList.Search(1, searchByName(name))
					return searched, searched != nil && searched.First() != nil
				})
			}
		}
		if receiver != nil && receiver.First() != nil && atName != "" {
			if strings.Contains(msgContent, "\u2005") {
				msgContent = strings.Replace(msgContent, "\u2005", "", 1)
			}
			offset := -1
			length := 0
			flags := []string{"@" + atName + " ", "@" + atName}
			for _, flag := range flags {
				offset = strings.Index(msgContent, flag)
				if offset != -1 {
					offset = len([]rune(msgContent[:offset]))
					length = len([]rune(flag))
					break
				}
			}
			id, err := h.member.GetID(receiver.First())
			if err != nil {
				slog.Error("获取用户ID失败", "msgId", ctx.MsgId, "err", err)
			} else {
				at = &hub.At{
					UID:    id,
					Name:   atName,
					Bot:    receiver.First().UserName == ctx.Owner().UserName,
					Offset: offset,
					Length: length,
				}
			}
		}
	}
	if at == nil {
		slog.Info("收到文本消息", "msgId", ctx.MsgId, "content", ctx.Content)
	} else {
		slog.Info("收到文本消息", "msgId", ctx.MsgId, "content", ctx.Content, "at", *at)
	}
	h.dispatch(&hub.TextMessage{
		BaseMessage: *message,
		Content:     msgContent,
		Quote:       quote,
		At:          at,
	})
	// 设置为已读
	if h.limit.Allow() {
		_ = ctx.AsRead()
	}
}

// onRecalled 转发消息撤回消息
func (h *Hub) onRecalled(ctx *openwechat.MessageContext) {
	defer ctx.Abort()
	recalled, err := ctx.RevokeMsg()
	if err != nil || recalled == nil {
		slog.Error("解析撤回消息失败", "msgId", ctx.MsgId, "err", err)
		return
	}
	message := h.prepareMessage(ctx)
	if message == nil {
		return
	}
	h.dispatch(&hub.RevokedMessage{
		BaseMessage: *message,
		Revoke: hub.Revoke{
			OldMsgID:   strconv.Itoa(int(recalled.RevokeMsg.MsgId)),
			ReplaceMsg: recalled.RevokeMsg.ReplaceMsg,
		},
	})
	// 设置为已读
	if h.limit.Allow() {
		_ = ctx.AsRead()
	}
}

// onMedia 转发文件消息
func (h *Hub) onMedia(ctx *openwechat.MessageContext) {
	if !ctx.HasFile() {
		return
	}
	defer ctx.Abort()

	var buf bytes.Buffer
	filename := ctx.FileName
	if filename == "" {
		fileExt := ""
		filename = fmt.Sprintf("%x", md5.Sum([]byte(ctx.Content)))
		if ctx.IsVideo() {
			fileExt = ".mp4"
		} else if ctx.IsVoice() {
			fileExt = ".mp3"
		} else if ctx.IsPicture() || ctx.IsEmoticon() {
			if err := ctx.SaveFile(&buf); err != nil {
				slog.Error("获取文件失败", "filename", filename, err)
				ctx.Content = strings.TrimSpace(filename)
				return
			}
			filetype := http.DetectContentType(buf.Bytes())
			filetype = filetype[6:]
			if strings.Contains(filetype, "-") || strings.EqualFold(filetype, "text/plain") {
				fileExt = ".jpg"
			} else {
				fileExt = "." + filetype
			}
		}
		filename = filename + fileExt
	}
	writer, savePath, err := h.storage.Writer(filename)
	if err != nil {
		slog.Error("获取文件Writer失败", "msgId", ctx.MsgId, "filename", filename, "err", err)
		return
	}
	defer func() {
		_ = writer.Close()
	}()
	if buf.Len() > 0 {
		if _, err = buf.WriteTo(writer); err != nil {
			slog.Error("写入文件失败", "msgId", ctx.MsgId, "filename", filename, "err", err)
			return
		}
	} else {
		if err := ctx.SaveFile(writer); err != nil {
			slog.Error("保存文件失败", "msgId", ctx.MsgId, "filename", filename, "err", err)
			return
		}
	}

	message := h.prepareMessage(ctx)
	if message == nil {
		return
	}
	h.dispatch(&hub.MediaMessage{
		BaseMessage: *message,
		Media: hub.Media{
			Filename: filename,
			Src:      savePath,
			Size:     ctx.FileSize,
		},
	})
	// 设置为已读
	if h.limit.Allow() {
		_ = ctx.AsRead()
	}
}

func (h *Hub) StartWatchMembers() {
	c := cron.New(cron.WithSeconds(), cron.WithLogger(cron.DefaultLogger))
	for _, spec := range []string{"0 0/5 6-23 * * *", "0 0/30 0-6 * * *"} {
		_, err := c.AddFunc(spec, h.watchMembersAndNotify)
		if err != nil {
			slog.Error("添加定时任务出错", "cron", spec, "error", err)
		}
	}
	slog.Info("开始监听群成员变动")
	c.Start()
}

func (h *Hub) watchMembersAndNotify() {
	exitGroupUserMap := h.member.RefreshGroupMember()
	if len(exitGroupUserMap) == 0 {
		return
	}

	for gid, users := range exitGroupUserMap {
		groupName, _ := h.member.GetName(gid)

		exits := make([]hub.EventExitGroupUser, 0, len(users))
		for _, user := range users {
			exits = append(exits, hub.EventExitGroupUser{
				UID:  user.UID,
				Name: user.Nickname,
			})
		}

		// 下发系统消息
		h.dispatch(&hub.SystemMessage{
			BaseMessage: hub.BaseMessage{
				MsgType:   int(openwechat.MsgTypeSys),
				Time:      time.Now().Unix(),
				GID:       gid,
				GroupName: groupName,
			},
			Event: "ExitGroup",
			Data:  exits,
		})
	}
}
