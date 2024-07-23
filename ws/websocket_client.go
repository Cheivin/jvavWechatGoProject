package ws

import (
	"context"
	"github.com/gorilla/websocket"
	"log/slog"
	"time"
	"wechat-hub/hub"
)

type ClientMessageHandler struct {
	serverUrl string
	messages  chan []byte
	heartbeat time.Duration
	server    Client
	onMessage hub.OnMessage
}

func NewWebsocketClientMessageHandler(ctx context.Context, serverUrl string, heartbeat time.Duration, onMessage hub.OnMessage) *ClientMessageHandler {
	h := &ClientMessageHandler{
		serverUrl: serverUrl,
		heartbeat: heartbeat,
		messages:  make(chan []byte, 10),
		onMessage: onMessage,
	}
	// 最低5s心跳
	if h.heartbeat > 0 && h.heartbeat < time.Second*5 {
		h.heartbeat = time.Second * 5
	}
	go h.serve(ctx)
	return h
}

func (h *ClientMessageHandler) serve(ctx context.Context) {
	for {
		conn, _, err := websocket.DefaultDialer.Dial(h.serverUrl, nil)
		if err != nil {
			slog.Error("websocket连接失败 等待重试", "server", h.serverUrl, "error", err)
			time.Sleep(time.Second * 5)
			continue
		}
		slog.Info("websocket连接成功", "server", h.serverUrl)
		// 创建client
		c := newClient(conn, h.heartbeat, h.onMessage)
		h.server = c
		go h.sendMessage()
		if err := c.Serve(ctx); err == nil {
			return
		}
		slog.Error("websocket连接断开 等待重连", "server", h.serverUrl, "error", err)
		time.Sleep(time.Second * 5)
	}
}

func (h *ClientMessageHandler) sendMessage() {
	for message := range h.messages {
		if err := h.server.SendMessage(message); err != nil {
			slog.Error("发送消息失败", "err", err)
			return
		}
	}
}

func (h *ClientMessageHandler) Redirect(message hub.Message) error {
	bytes, err := message.Marshal()
	if err != nil {
		return err
	}
	return h.RedirectBytes(bytes)
}

func (h *ClientMessageHandler) RedirectBytes(bytes []byte) error {
	h.messages <- bytes
	return nil
}
