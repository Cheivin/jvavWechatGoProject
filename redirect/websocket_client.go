package redirect

import (
	"context"
	"github.com/gorilla/websocket"
	"log/slog"
	"time"
)

type WSClientRedirector struct {
	serverUrl string
	messages  chan []byte
	heartbeat time.Duration
	server    wsConnection
	onMessage OnMessage
}

type WSClientOption func(h *WSClientRedirector)

func WSClientHeartbeat(heartbeat time.Duration) WSClientOption {
	return func(h *WSClientRedirector) {
		// 最低5s心跳
		if heartbeat < time.Second*5 {
			heartbeat = time.Second * 5
		}
		h.heartbeat = heartbeat
	}
}
func NewWebsocketClientMessageHandler(ctx context.Context, serverUrl string, options ...WSClientOption) *WSClientRedirector {
	h := &WSClientRedirector{
		serverUrl: serverUrl,
		messages:  make(chan []byte, 10),
	}
	for _, option := range options {
		option(h)
	}
	go h.serve(ctx)
	return h
}

func (h *WSClientRedirector) serve(ctx context.Context) {
	for {
		conn, _, err := websocket.DefaultDialer.Dial(h.serverUrl, nil)
		if err != nil {
			slog.Error("websocket连接失败 等待重试", "server", h.serverUrl, "error", err)
			time.Sleep(time.Second * 5)
			continue
		}
		slog.Info("websocket连接成功", "server", h.serverUrl)
		// 创建client
		c := newClient(conn, h.heartbeat, func(messageType int, message []byte) {
			if h.onMessage != nil {
				_ = h.onMessage(message)
			}
		})
		h.server = c
		go h.sendMessage()
		if err := c.Serve(ctx); err == nil {
			return
		}
		slog.Error("websocket连接断开 等待重连", "server", h.serverUrl, "error", err)
		time.Sleep(time.Second * 5)
	}
}

func (h *WSClientRedirector) sendMessage() {
	for message := range h.messages {
		if err := h.server.SendMessage(message); err != nil {
			slog.Error("发送消息失败", "err", err)
			return
		}
	}
}

func (h *WSClientRedirector) SendMessage(bytes []byte) error {
	h.messages <- bytes
	return nil
}

func (h *WSClientRedirector) OnMessage(fn OnMessage) {
	h.onMessage = fn
}
