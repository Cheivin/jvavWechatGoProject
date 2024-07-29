package redirect

import (
	"context"
	"fmt"
	"github.com/gorilla/websocket"
	"log/slog"
	"time"
)

type wsConnection interface {
	Serve(ctx context.Context) error
	Close()
	SendMessage(message []byte) error
}

type connection struct {
	*websocket.Conn
	heartbeat         time.Duration
	messageBufferPool chan []byte
	exit              chan error
	cancelFn          context.CancelFunc
	receiveMessage    func(messageType int, message []byte)
}

func newClient(conn *websocket.Conn, heartbeat time.Duration, receiveMessage func(messageType int, message []byte)) wsConnection {
	conn.SetReadLimit(1024 * 1024 * 10)
	return &connection{
		Conn:              conn,
		heartbeat:         heartbeat,
		messageBufferPool: make(chan []byte, 5),
		exit:              make(chan error),
		receiveMessage:    receiveMessage,
	}
}

func (c *connection) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancelFn = cancel
	var ticker *time.Ticker
	if c.heartbeat > 0 {
		ticker = time.NewTicker(c.heartbeat)
	}
	defer func() {
		cancel()
		if ticker != nil {
			ticker.Stop()
		}
		close(c.messageBufferPool)
		_ = c.Conn.Close()
	}()
	go c.readMessage()
	go c.sendMessage()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := c.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(2*time.Second)); err != nil {
				slog.Error("心跳消息出错", "error", err)
				return err
			}
		case err := <-c.exit:
			return err
		}
	}
}
func (c *connection) Close() {
	if c.cancelFn != nil {
		c.cancelFn()
	} else {
		_ = c.Conn.Close()
	}
}
func (c *connection) SendMessage(message []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	c.messageBufferPool <- message
	return nil
}

func (c *connection) readMessage() {
	for {
		messageType, message, err := c.ReadMessage()
		if err != nil {
			slog.Error("读取消息出错", "error", err)
			c.exit <- err
			return
		}
		switch messageType {
		case websocket.PingMessage:
			// 当接收到 Ping 消息时，自动发送 Pong 消息
			if err := c.WriteMessage(websocket.PongMessage, message); err != nil {
				slog.Error("pong消息出错", "error", err)
				c.exit <- err
				return
			}
		default:
			c.onReceiveMessage(messageType, message)
		}
	}
}

func (c *connection) onReceiveMessage(messageType int, message []byte) {
	defer func() {
		if e := recover(); e != nil {
			slog.Error("onMessage出错", "error", e)
		}
	}()
	if c.receiveMessage != nil {
		c.receiveMessage(messageType, message)
	} else {
		slog.Info("收到消息", "messageType", messageType, "message", string(message))
	}
}

// sendMessage 将缓冲队列中的信息转发到服务器
func (c *connection) sendMessage() {
	for {
		message := <-c.messageBufferPool
		err := c.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			slog.Error("发送消息出错", "error", err)
			c.exit <- err
			return
		}
	}
}
