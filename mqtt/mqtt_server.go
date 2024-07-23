package mqtt

import (
	"fmt"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/hooks/storage/badger"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
	"log/slog"
	"wechat-hub/hub"
)

type ServerMessageHandler struct {
	server         *mqtt.Server
	onMessage      hub.OnMessage
	publishTopic   string
	subscribeTopic string
}
type Option = func(*ServerMessageHandler)

func WithTCP(port int) Option {
	return func(h *ServerMessageHandler) {
		_ = h.server.AddListener(listeners.NewTCP(listeners.Config{ID: fmt.Sprintf("tcp_%d", port), Address: fmt.Sprintf(":%d", port)}))
	}
}

func WithWS(port int) Option {
	return func(h *ServerMessageHandler) {
		_ = h.server.AddListener(listeners.NewWebsocket(listeners.Config{ID: fmt.Sprintf("ws_%d", port), Address: fmt.Sprintf(":%d", port)}))
	}
}

func WithSubscribeTopic(topic string) Option {
	return func(h *ServerMessageHandler) {
		h.subscribeTopic = topic
	}
}

func NewMQTTServerMessageHandler(dataDir string, publishTopic string, onMessage hub.OnMessage, options ...Option) *ServerMessageHandler {
	server := mqtt.New(&mqtt.Options{
		InlineClient: true, // 开启内联客户端
	})
	// 允许所有连接
	_ = server.AddHook(new(auth.AllowHook), nil)
	// 消息持久化
	if err := server.AddHook(new(badger.Hook), &badger.Options{
		Path: dataDir,
	}); err != nil {
		panic(err)
	}
	h := &ServerMessageHandler{
		server:       server,
		publishTopic: publishTopic,
		onMessage:    onMessage,
	}
	for _, option := range options {
		option(h)
	}
	return h
}

func (h *ServerMessageHandler) ListenAndServe() {
	if h.subscribeTopic != "" {
		if err := h.server.Subscribe(h.subscribeTopic, 1, h.subscribeCallback); err != nil {
			panic(err)
		}
	}
	slog.Info("MQTTServerMessageHandler serving")
	if err := h.server.Serve(); err != nil {
		slog.Error("ListenAndServe serve error", "err", err)
	}
}

func (h *ServerMessageHandler) subscribeCallback(cl *mqtt.Client, sub packets.Subscription, pk packets.Packet) {
	h.onMessage(pk.Payload)
}

func (h *ServerMessageHandler) Redirect(message hub.Message) error {
	bytes, err := message.Marshal()
	if err != nil {
		return err
	}
	return h.RedirectBytes(bytes)
}

func (h *ServerMessageHandler) RedirectBytes(bytes []byte) error {
	_ = h.server.Publish(h.publishTopic, bytes, true, 1)
	return nil
}
