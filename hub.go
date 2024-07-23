package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"log"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"wechat-hub/db"
	"wechat-hub/hub"
	"wechat-hub/storage"
	"wechat-hub/ws"
)

type Hub struct {
	db        db.Storage
	storage   storage.Storage
	redirects []hub.MessageRedirect
	member    *hub.MemberManager
	ctx       context.Context
}

func NewHub(ctx context.Context, db db.Storage, storage storage.Storage) *Hub {
	member := hub.NewIDStorage(db)
	return &Hub{
		db:        db,
		storage:   storage,
		redirects: []hub.MessageRedirect{},
		member:    member,
		ctx:       ctx,
	}
}

// AddRedirect 添加一个转发器
func (h *Hub) AddRedirect(redirect hub.MessageRedirect) {
	h.redirects = append(h.redirects, redirect)
}

// UseWebsocketClientRedirect 使用websocket客户端转发
func (h *Hub) UseWebsocketClientRedirect(serverUrl string, heartbeat time.Duration) {
	h.AddRedirect(ws.NewWebsocketClientMessageHandler(h.ctx, serverUrl, heartbeat, h.receive))
}

// UseWebsocketServerRedirect 使用websocket服务端转发
func (h *Hub) UseWebsocketServerRedirect(heartbeat time.Duration) *ws.ServerMessageHandler {
	server := ws.NewWebsocketServerMessageHandler(h.ctx, heartbeat, h.receive)
	h.AddRedirect(server)
	return server
}

func (h *Hub) dispatch(message hub.Message) {
	marshal, err := message.Marshal()
	if err != nil {
		slog.Error("消息序列化失败", "msgId", message.ID(), "err", err)
		return
	}
	for _, redirect := range h.redirects {
		go func(redirect hub.MessageRedirect) {
			if err := redirect.RedirectBytes(marshal); err != nil {
				slog.Error("消息转发失败", "msgId", message.ID(), "err", err)
			}
		}(redirect)
	}
}

func (h *Hub) receive(message []byte) {
	slog.Debug("收到上报消息", "msgId", string(message))
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
			if !h.db.PutIfAbsent(ctx.MsgId, ctx.MsgId) {
				slog.Warn("消息重复", "msgId", ctx.MsgId, "msgType", ctx.MsgType)
				ctx.Abort()
			}
		}
}

func (h *Hub) newMessage(ctx *openwechat.MessageContext) *hub.BaseMessage {
	gid := ""
	groupName := ""
	uid := ""
	username := ""
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
		MsgType:    int(ctx.MsgType),
		Time:       ctx.CreateTime,
		MsgID:      ctx.MsgId,
		GID:        gid,
		GroupName:  groupName,
		UID:        uid,
		Username:   username,
		RawMessage: ctx.Content,
	}
}

// onSystemMessage 系统消息解析
func (h *Hub) onSystemMessage(ctx *openwechat.MessageContext) {
	if ctx.IsRenameGroup() {
		defer ctx.Abort()
		matches := regexp.MustCompile(`"(.*?)"修改群名为“(.*?)”`).FindAllString(ctx.Content, -1)
		if len(matches) > 0 {
			parts := strings.SplitN(matches[0], "修改群名为", 2)
			userName := strings.Trim(parts[0], `"`)
			groupName := strings.TrimPrefix(strings.TrimSuffix(parts[1], `”`), `“`)
			slog.Info("检测到群名片已修改", "user", userName, "groupName", groupName)
			_ = ctx.AsRead()
			return
		}
	}
}

// onText 转发文字消息
func (h *Hub) onText(ctx *openwechat.MessageContext) {
	message := h.newMessage(ctx)
	if message == nil {
		return
	}
	// TODO 解析引用消息部分
	// TODO 解析AT消息部分

	h.dispatch(message)
}

// onRecalled 转发消息撤回消息
func (h *Hub) onRecalled(ctx *openwechat.MessageContext) {
	defer ctx.Abort()
	recalled, err := ctx.RevokeMsg()
	if err != nil || recalled == nil {
		slog.Error("解析撤回消息失败", "msgId", ctx.MsgId, "err", err)
		return
	}
	message := h.newMessage(ctx)
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
}

// onMedia 转发文件消息
func (h *Hub) onMedia(ctx *openwechat.MessageContext) {
	if !ctx.HasFile() {
		return
	}
	defer ctx.Abort()

	message := h.newMessage(ctx)
	if message == nil {
		return
	}

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
				log.Println("获取文件失败", err)
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
	h.dispatch(&hub.MediaMessage{
		BaseMessage: *message,
		Media: hub.Media{
			Filename: filename,
			Src:      savePath,
			Size:     ctx.FileSize,
		},
	})
}
