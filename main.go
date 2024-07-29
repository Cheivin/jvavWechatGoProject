package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path"
	"strconv"
	"time"
	"wechat-hub/auth"
	"wechat-hub/hub"
	"wechat-hub/storage"
)

var (
	dataDir    string
	httpPort   int
	wsPort     int
	mqttPort   int
	mqttWSPort int
)

func init() {
	// 创建缓存目录
	dataDir = os.Getenv("DATA")
	if dataDir == "" {
		dataDir = "./data"
	}
	if err := os.MkdirAll(dataDir, os.ModePerm); err != nil {
		panic(err)
	}

	// http端口
	httpPort, _ = strconv.Atoi(os.Getenv("APP_PORT"))
	if httpPort == 0 {
		httpPort = 8080
	}

	wsPort, _ = strconv.Atoi(os.Getenv("WS_PORT"))
	if wsPort == 0 {
		wsPort = 18080
	}
	mqttPort, _ = strconv.Atoi(os.Getenv("MQTT_PORT"))
	if mqttPort == 0 {
		mqttPort = 1883
	}
	mqttWSPort, _ = strconv.Atoi(os.Getenv("MQTT_WS_PORT"))
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	// 数据库
	db := connectDB()

	authManager := auth.NewAuthManager(db)

	// 机器人
	bot := openwechat.NewBot(ctx)

	// 资源管理器
	memberManager := hub.NewMemberManger(bot, db)
	messageManager := hub.NewMessageManager(db)
	store := storage.NewLocalStorage(path.Join(dataDir, "files"))

	// 消息发送
	sender := NewMsgSender(bot, memberManager, store)
	// 消息转发器
	h := NewHub(ctx, memberManager, messageManager, store, authManager)
	h.UseWebsocketServerRedirect(wsPort, time.Second*10)
	h.UseMQTTServerRedirect(path.Join(dataDir, "mqtt"), mqttPort, mqttWSPort)
	// 消息处理器
	dispatcher := openwechat.NewMessageMatchDispatcher()
	dispatcher.SetAsync(true)
	h.Register(dispatcher)
	h.SetMessageSender(sender)

	// 扫码回调
	bot.UUIDCallback = func(uuid string) {
		slog.Info("QRCode", "URL", openwechat.GetQrcodeUrl(uuid))
	}
	bot.MessageHandler = dispatcher.AsMessageHandler()
	// 桌面模式
	openwechat.Desktop.Prepare(bot)
	if err := bot.HotLogin(openwechat.NewFileHotReloadStorage(path.Join(dataDir, "login.json")), &openwechat.RetryLoginOption{MaxRetryCount: 3}); err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Warn("Login canceled")
			os.Exit(0)
		}
	}
	bot.SyncCheckCallback = func(resp openwechat.SyncCheckResponse) {
		if resp.HasNewMessage() {
			slog.Info("NewMessage", "RetCode", resp.RetCode, "Selector", resp.Selector)
		} else if resp.Err() != nil {
			slog.Error("SyncCheck", "RetCode", resp.RetCode, "Selector", resp.Selector, "Error", resp.Err())
		}
	}
	bot.LoginCallBack = func(body openwechat.CheckLoginResponse) {
		code, err := body.Code()
		if err != nil {
			slog.Error("Login", "Error", err)
			return
		}
		slog.Info("Login", "code", code, "msg", code.String())
	}
	memberManager.RefreshGroupMember()
	h.StartWatchMembers()
	go NewHttpHandler(store, memberManager, sender, WithBaseAuth(authManager)).ListenAndServe(httpPort)
	<-ctx.Done()
}

func connectDB() *gorm.DB {
	var dialector gorm.Dialector
	dbType := os.Getenv("DB")
	switch dbType {
	case "mysql":
		username := os.Getenv("MYSQL_USERNAME")
		if username == "" {
			username = "root"
		}
		password := os.Getenv("MYSQL_PASSWORD")
		if password == "" {
			password = "root"
		}
		host := os.Getenv("MYSQL_HOST")
		if host == "" {
			host = "127.0.0.1"
		}
		port := os.Getenv("MYSQL_PORT")
		if port == "" {
			port = "3306"
		}
		database := os.Getenv("MYSQL_DATABASE")
		parameters := os.Getenv("MYSQL_PARAMETERS")
		dialector = mysql.Open(fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s", []interface{}{
			username,
			password,
			host,
			port,
			database,
			parameters,
		}...))
	case "sqlite":
	case "":
		dialector = sqlite.Open(path.Join(dataDir, "database.sqlite"))
		break
	default:
		panic("unknown db type")
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags),
			logger.Config{
				SlowThreshold:             time.Second,
				LogLevel:                  logger.Error,
				IgnoreRecordNotFoundError: true,
				ParameterizedQueries:      false,
				Colorful:                  true,
			},
		),
	})
	if err != nil {
		panic(errors.Join(err, errors.New("failed to connect connectDB")))
	}
	return db
}
