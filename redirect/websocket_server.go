package redirect

import (
	"context"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/gorilla/websocket"
	"log/slog"
	"net/http"
	"time"
	"wechat-hub/auth"
)

type WSServerRedirector struct {
	upgrader   websocket.Upgrader
	messages   chan []byte
	heartbeat  time.Duration
	register   chan wsConnection
	unregister chan wsConnection
	clients    map[wsConnection]struct{}
	onMessage  OnMessage
	auth       *auth.Manager
}
type WSServerOption func(h *WSServerRedirector)

func WSServerHeartbeat(heartbeat time.Duration) WSServerOption {
	return func(h *WSServerRedirector) {
		// 最低5s心跳
		if heartbeat < time.Second*5 {
			heartbeat = time.Second * 5
		}
		h.heartbeat = heartbeat
	}
}

func WSServerAuth(manager *auth.Manager) WSServerOption {
	return func(h *WSServerRedirector) {
		h.auth = manager
	}
}

func NewWebsocketServerMessageHandler(ctx context.Context, options ...WSServerOption) *WSServerRedirector {
	h := &WSServerRedirector{
		messages: make(chan []byte, 10), // 消息缓冲区
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		register:   make(chan wsConnection),
		unregister: make(chan wsConnection),
		clients:    make(map[wsConnection]struct{}),
	}
	for _, option := range options {
		option(h)
	}
	go h.serve(ctx)
	return h
}
func (h *WSServerRedirector) serve(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
	}()
	go h.sendMessage()
	for {
		select {
		case c := <-h.register:
			h.clients[c] = struct{}{}
			go func() {
				_ = c.Serve(ctx)
			}()
		case c := <-h.unregister:
			delete(h.clients, c)
			c.Close()
		case <-ctx.Done():
			close(h.register)
			close(h.unregister)
			close(h.messages)
			clear(h.clients)
			return
		}
	}
}

func (h *WSServerRedirector) sendMessage() {
	for message := range h.messages {
		for c := range h.clients {
			if err := c.SendMessage(message); err != nil {
				slog.Error("发送消息失败", "err", err)
				h.unregister <- c
			}
		}
	}
}

func (h *WSServerRedirector) Register(dispatcher *openwechat.MessageMatchDispatcher) {
	dispatcher.OnText(func(ctx *openwechat.MessageContext) {
		h.messages <- []byte(ctx.Message.Content)
	})
}

func (h *WSServerRedirector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.auth != nil {
		username := r.URL.Query().Get("username")
		password := r.URL.Query().Get("password")
		if !h.auth.CheckUser(username, password) {
			slog.Error("WebsocketServerMessageHandler Auth 用户名或密码错误")
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("WebsocketServerMessageHandler ws upgrade", "err", err)
		http.Error(w, "ws upgrade error", http.StatusInternalServerError)
		return
	}
	h.register <- newClient(conn, h.heartbeat, func(messageType int, message []byte) {
		if h.onMessage != nil {
			_ = h.onMessage(message)
		}
	})
}

func (h *WSServerRedirector) SendMessage(bytes []byte) error {
	h.messages <- bytes
	return nil
}

func (h *WSServerRedirector) OnMessage(fn OnMessage) {
	h.onMessage = fn
}

func (h *WSServerRedirector) ListenAndServe(port int) {
	slog.Info("WebsocketServerMessageHandler listening on", "port", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), h); err != nil {
		slog.Error("ListenAndServe", "err", err)
	}
}
