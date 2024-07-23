package main

import (
	"context"
	"github.com/eatmoreapple/openwechat"
	"log/slog"
	"os"
	"os/signal"
	"path"
	"time"
	"wechat-hub/db"
	"wechat-hub/storage"
)

var dataDir string

func init() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	dataDir = "./cache"

	// 创建缓存目录
	if err := os.MkdirAll(dataDir, os.ModePerm); err != nil {
		panic(err)
	}
}

func main() {

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	// 数据存储
	data, err := db.NewBadgerStorage(path.Join(dataDir, "badger"))
	if err != nil {
		panic(err)
	}

	// 文件资源保持
	store := storage.NewLocalStorage(path.Join(dataDir, "files"))

	// 消息处理器
	dispatcher := openwechat.NewMessageMatchDispatcher()
	dispatcher.SetAsync(true)

	// 消息转发器
	hub := NewHub(ctx, data, store)
	hub.Register(dispatcher)
	hub.UseWebsocketClientRedirect("ws://124.222.224.186:8800", time.Second*10)
	wsServer := hub.UseWebsocketServerRedirect(time.Second * 10)
	go wsServer.ListenAndServe(18080)

	// 机器人
	bot := openwechat.NewBot(ctx)
	// 扫码回调
	bot.UUIDCallback = func(uuid string) {
		slog.Info("QRCode", "URL", openwechat.GetQrcodeUrl(uuid))
	}
	bot.MessageHandler = dispatcher.AsMessageHandler()
	// 桌面模式
	openwechat.Desktop.Prepare(bot)
	if err = bot.HotLogin(openwechat.NewFileHotReloadStorage(path.Join(dataDir, "db.data")), &openwechat.RetryLoginOption{MaxRetryCount: 3}); err != nil {
		panic(err)
	}
	bot.SyncCheckCallback = func(resp openwechat.SyncCheckResponse) {
		if resp.HasNewMessage() {
			slog.Info("NewMessage", "RetCode", resp.RetCode, "Selector", resp.Selector)
		} else if resp.Err() != nil {
			slog.Error("SyncCheck", "RetCode", resp.RetCode, "Selector", resp.Selector, "error", resp.Err())
		}
	}

	self, _ := bot.GetCurrentUser()
	slog.Info("Bot", "User", self.NickName)
	<-ctx.Done()
	// websocket服务端和客户端效果可以去 http://www.websocket-test.com/ 网站测试
}
