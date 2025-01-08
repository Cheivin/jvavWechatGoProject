package redirect

import (
	"bytes"
	"fmt"
	"log/slog"
	authManager "wechat-hub/auth"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
)

type MQTTRedirector struct {
	server         *mqtt.Server
	publishTopic   string
	subscribeTopic string
	auth           bool
	onMessage      OnMessage
}
type MQTTOption = func(*MQTTRedirector)

func WithTCP(port int) MQTTOption {
	return func(h *MQTTRedirector) {
		_ = h.server.AddListener(listeners.NewTCP(listeners.Config{ID: fmt.Sprintf("tcp_%d", port), Address: fmt.Sprintf(":%d", port)}))
	}
}

func WithWS(port int) MQTTOption {
	return func(h *MQTTRedirector) {
		_ = h.server.AddListener(listeners.NewWebsocket(listeners.Config{ID: fmt.Sprintf("ws_%d", port), Address: fmt.Sprintf(":%d", port)}))
	}
}

func WithSubscribeTopic(topic string) MQTTOption {
	return func(h *MQTTRedirector) {
		h.subscribeTopic = topic
	}
}

func WithMQTTAuth(manager *authManager.Manager) MQTTOption {
	return func(h *MQTTRedirector) {
		h.auth = true
		_ = h.server.AddHook(new(UserAuthHook), &userAuthHookOption{
			auth: manager,
		})
	}
}

func NewMQTTServerMessageHandler(dataDir string, publishTopic string, options ...MQTTOption) *MQTTRedirector {
	server := mqtt.New(&mqtt.Options{
		InlineClient: true, // 开启内联客户端
	})
	// // 消息持久化
	// if err := server.AddHook(new(badger.Hook), &badger.Options{
	// 	Path: dataDir,
	// }); err != nil {
	// 	panic(err)
	// }
	h := &MQTTRedirector{
		server:       server,
		publishTopic: publishTopic,
	}
	for _, option := range options {
		option(h)
	}
	if !h.auth {
		// 允许所有连接
		_ = server.AddHook(new(auth.AllowHook), nil)
	}
	return h
}

func (h *MQTTRedirector) ListenAndServe() {
	if h.subscribeTopic != "" {
		if err := h.server.Subscribe(h.subscribeTopic, 1, func(cl *mqtt.Client, sub packets.Subscription, pk packets.Packet) {
			if h.onMessage != nil {
				_ = h.onMessage(pk.Payload, "MQTT", string(cl.Properties.Username))
			}
		}); err != nil {
			panic(err)
		}
	}
	slog.Info("MQTTServerMessageHandler serving")
	if err := h.server.Serve(); err != nil {
		slog.Error("ListenAndServe serve error", "err", err)
	}
}

func (h *MQTTRedirector) SendMessage(bytes []byte) error {
	_ = h.server.Publish(h.publishTopic, bytes, false, 1)
	return nil
}
func (h *MQTTRedirector) OnMessage(fn OnMessage) {
	h.onMessage = fn

}

type (
	UserAuthHook struct {
		mqtt.HookBase
		auth *authManager.Manager
	}
	userAuthHookOption struct {
		auth *authManager.Manager
	}
)

func (h *UserAuthHook) ID() string {
	return "user-auth"
}

func (h *UserAuthHook) Provides(b byte) bool {
	return bytes.Contains([]byte{
		mqtt.OnConnectAuthenticate,
		mqtt.OnACLCheck,
	}, []byte{b})
}

func (h *UserAuthHook) Init(config any) error {
	if cfg, ok := config.(*userAuthHookOption); !ok && config != nil {
		return mqtt.ErrInvalidConfigType
	} else {
		h.auth = cfg.auth
	}
	return nil
}

func (h *UserAuthHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	return true
}
func (h *UserAuthHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) bool {
	username := string(pk.Connect.Username)
	authed := h.auth.CheckUser(username, string(pk.Connect.Password))
	if !authed {
		slog.Error("MQTTServerMessageHandler OnConnectAuthenticate", "username", username, "password", string(pk.Connect.Password), "err", "auth failed")
	} else {
		slog.Info("MQTTServerMessageHandler OnConnectAuthenticate", "username", username, "password", string(pk.Connect.Password))
	}
	return authed
}
