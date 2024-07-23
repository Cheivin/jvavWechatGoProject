package ws

import (
	"context"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/gorilla/websocket"
	"log/slog"
	"net/http"
	"time"
	"wechat-hub/hub"
)

type ServerMessageHandler struct {
	upgrader   websocket.Upgrader
	messages   chan []byte
	heartbeat  time.Duration
	register   chan Client
	unregister chan Client
	clients    map[Client]struct{}
	onMessage  hub.OnMessage
}

func NewWebsocketServerMessageHandler(ctx context.Context, heartbeat time.Duration, onMessage hub.OnMessage) *ServerMessageHandler {
	h := &ServerMessageHandler{
		heartbeat: heartbeat,
		messages:  make(chan []byte, 10), // 消息缓冲区
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		register:   make(chan Client),
		unregister: make(chan Client),
		clients:    make(map[Client]struct{}),
		onMessage:  onMessage,
	}
	// 最低5s心跳
	if h.heartbeat > 0 && h.heartbeat < time.Second*5 {
		h.heartbeat = time.Second * 5
	}
	go h.serve(ctx)
	return h
}
func (h *ServerMessageHandler) serve(ctx context.Context) {
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

func (h *ServerMessageHandler) sendMessage() {
	for message := range h.messages {
		for c := range h.clients {
			if err := c.SendMessage(message); err != nil {
				slog.Error("发送消息失败", "err", err)
				h.unregister <- c
			}
		}
	}
}

func (h *ServerMessageHandler) Register(dispatcher *openwechat.MessageMatchDispatcher) {
	dispatcher.OnText(func(ctx *openwechat.MessageContext) {
		h.messages <- []byte(ctx.Message.Content)
	})
}

func (h *ServerMessageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("ws upgrade", "err", err)
	}
	h.register <- newClient(conn, h.heartbeat, h.onMessage)
}

func (h *ServerMessageHandler) Redirect(message hub.Message) error {
	bytes, err := message.Marshal()
	if err != nil {
		return err
	}
	return h.RedirectBytes(bytes)
}

func (h *ServerMessageHandler) RedirectBytes(bytes []byte) error {
	h.messages <- bytes
	return nil
}

func (h *ServerMessageHandler) ListenAndServe(port int) {
	slog.Info("WebsocketServerMessageHandler listening on", "port", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), h); err != nil {
		slog.Error("ListenAndServe", "err", err)
	}
}
