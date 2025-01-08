package main

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"wechat-hub/hub"
	"wechat-hub/storage"

	"github.com/eatmoreapple/openwechat"
	"github.com/go-resty/resty/v2"
	"golang.org/x/time/rate"
)

type MsgSender struct {
	member  hub.MemberManager
	Bot     *openwechat.Bot
	Resty   *resty.Client
	limit   *rate.Limiter
	storage storage.Storage
}
type SenderOption = func(sender *MsgSender)

func WithLimit(r rate.Limit, b int) SenderOption {
	return func(sender *MsgSender) {
		sender.limit = rate.NewLimiter(r, b)
	}
}
func NewMsgSender(bot *openwechat.Bot, member hub.MemberManager, storage storage.Storage, options ...SenderOption) *MsgSender {
	sender := &MsgSender{
		member:  member,
		Bot:     bot,
		Resty:   resty.New(),
		storage: storage,
	}
	for _, option := range options {
		option(sender)
	}
	if sender.limit == nil {
		sender.limit = rate.NewLimiter(rate.Every(time.Second), 1)
	}
	return sender
}

func (s *MsgSender) getSelf() (*openwechat.Self, error) {
	if !s.Bot.Alive() {
		return nil, errors.New("bot已掉线")
	}
	return s.Bot.GetCurrentUser()
}

func (s *MsgSender) SendMsg(msg *hub.SendMsgCommand) error {
	if msg.Gid == "" {
		return errors.New("群ID不能为空")
	}
	if msg.Body == "" {
		return errors.New("消息不能为空")
	}
	switch msg.Type {
	case 1:
		if _, err := s.SendGroupTextMsgByID(msg.Gid, msg.Body); err != nil {
			return err
		}
	case 2, 3, 4:
		if _, err := s.SendGroupMediaMsgByID(msg.Gid, msg.Type, msg.Body, msg.Filename, msg.Prompt); err != nil {
			return err
		}
	default:
		return errors.New("暂不支持该类型消息")
	}
	return nil
}

func (s *MsgSender) SendGroupTextMsgByID(id string, msg string) (string, error) {
	self, err := s.getSelf()
	if err != nil {
		return "", err
	}
	gid, err := s.member.GetByID(id)
	if err != nil {
		return "", err
	} else if gid == "" {
		return "", errors.New("群不存在")
	}

	groups, _ := self.Groups()
	group := groups.SearchByUserName(1, gid).First()
	if group == nil {
		return "", errors.New("群不存在")
	}

	// 限流最大等待
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	_ = s.limit.Wait(ctx) // 忽略限流，只是为了人为等待
	cancel()

	if sent, err := self.SendTextToGroup(group, msg); err != nil {
		return "", err
	} else {
		return sent.MsgId, nil
	}
}

func (s *MsgSender) SendGroupTextMsg(group *openwechat.Group, msg string) (string, error) {
	self, err := s.getSelf()
	if err != nil {
		return "", err
	}
	if group == nil {
		return "", errors.New("群不存在")
	}

	// 限流最大等待
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	_ = s.limit.Wait(ctx) // 忽略限流，只是为了人为等待
	cancel()

	if sent, err := self.SendTextToGroup(group, msg); err != nil {
		return "", err
	} else {
		return sent.MsgId, nil
	}
}

func (s *MsgSender) SendGroupMediaMsgByID(id string, mediaType int, src string, filename string, prompt string) (string, error) {
	self, err := s.getSelf()
	if err != nil {
		return "", err
	}
	gid, err := s.member.GetByID(id)
	if err != nil {
		return "", err
	} else if gid == "" {
		return "", errors.New("群不存在")
	}
	groups, _ := self.Groups()
	group := groups.SearchByUserName(1, gid).First()
	if group == nil {
		return "", errors.New("群不存在")
	}
	return s.SendGroupMediaMsg(group, mediaType, src, filename, prompt)
}

func (s *MsgSender) SendGroupMediaMsg(group *openwechat.Group, mediaType int, src string, filename string, prompt string) (string, error) {
	self, err := s.getSelf()
	if err != nil {
		return "", err
	}

	switch mediaType {
	case 2:
		if filename == "" {
			filename = fmt.Sprintf("%x.jpg", md5.Sum([]byte(src)))
		}
		reader, promptSent, err := s.prepareFile(self, group, src, filename, prompt)
		if err != nil {
			return "", err
		}
		defer func() {
			if promptSent != nil {
				_ = promptSent.Revoke()
			}
			_ = reader.Close()
		}()
		if sent, err := self.SendImageToGroup(group, reader); err != nil {
			return "", err
		} else {
			return sent.MsgId, nil
		}
	case 3:
		if filename == "" {
			filename = fmt.Sprintf("%x.mp4", md5.Sum([]byte(src)))
		}
		reader, promptSent, err := s.prepareFile(self, group, src, filename, prompt)
		if err != nil {
			return "", err
		}
		defer func() {
			if promptSent != nil {
				_ = promptSent.Revoke()
			}
			_ = reader.Close()
		}()
		if sent, err := self.SendVideoToGroup(group, reader); err != nil {
			return "", err
		} else {
			return sent.MsgId, nil
		}
	case 4:
		if filename == "" {
			filename = fmt.Sprintf("%x", md5.Sum([]byte(src)))
		}
		reader, promptSent, err := s.prepareFile(self, group, src, filename, prompt)
		if err != nil {
			return "", err
		}
		defer func() {
			if promptSent != nil {
				_ = promptSent.Revoke()
			}
			_ = reader.Close()
		}()
		if sent, err := self.SendFileToGroup(group, reader); err != nil {
			return "", err
		} else {
			return sent.MsgId, nil
		}
	default:
		return "", errors.New("暂不支持该类型")
	}

}

func (s *MsgSender) prepareFile(self *openwechat.Self, group *openwechat.Group, src string, filename string, prompt string) (reader io.ReadCloser, promptSent *openwechat.SentMessage, err error) {
	if prompt != "" {
		// 限流等待
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
		_ = s.limit.Wait(ctx) // 忽略限流，只是为了人为等待
		cancel()
		promptSent, _ = self.SendTextToGroup(group, prompt)
		defer func() {
			_ = promptSent.Revoke()
		}()
	}
	// 加载资源
	if strings.HasPrefix(src, "BASE64:") {
		src = strings.TrimPrefix(src, "BASE64:")
		if r, err := s.getResourceFromBase64(filename, src); err != nil {
			return nil, nil, err
		} else {
			reader = r
		}
	} else if strings.HasPrefix(src, "RESOURCE:") {
		f := strings.TrimPrefix(src, "RESOURCE:")
		if r, err := s.getResourceFromStorage(f); err != nil {
			return nil, nil, err
		} else {
			reader = r
		}
	} else {
		if r, err := s.getResourceFromURL(s.Resty, filename, src); err != nil {
			return nil, nil, err
		} else {
			reader = r
		}
	}
	// 限流等待
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	_ = s.limit.WaitN(ctx, 5) // 忽略限流，只是为了人为等待
	cancel()
	return reader, promptSent, nil
}

func (s *MsgSender) getResourceFromStorage(filename string) (io.ReadCloser, error) {
	slog.Info("加载资源", "filename", filename)
	reader, err := s.storage.Reader(filename)
	if err != nil {
		slog.Error("读取资源信息出错", "filename", filename, "filepath", filename, "Error", err)
		return nil, errors.New("加载资源出错")
	}
	return reader, nil
}

func (s *MsgSender) getResourceFromBase64(filename string, src string) (io.ReadCloser, error) {
	slog.Info("转换BASE64资源", "filename", filename)
	srcBytes, err := base64.RawStdEncoding.DecodeString(src)
	if err != nil {
		slog.Error("解析资源信息出错", "filename", filename, "Error", err)
		return nil, errors.New("解析资源信息出错")
	}
	writer, f, err := s.storage.Writer(filename)
	if err != nil {
		slog.Error("创建资源信息出错", "filename", filename, "Error", err)
		return nil, errors.New("创建资源信息出错")
	}
	defer func() {
		_ = writer.Close()
	}()
	if _, err := writer.Write(srcBytes); err != nil {
		slog.Error("写入资源信息出错", "filename", filename, "Error", err)
		return nil, errors.New("写入资源信息出错")
	}
	slog.Info("下载资源完成", "filename", filename, "src", src, "path", f)
	return s.getResourceFromStorage(f)
}

func (s *MsgSender) getResourceFromURL(client *resty.Client, filename string, src string) (io.ReadCloser, error) {
	slog.Info("下载资源", "filename", filename, "src", src)
	resource, err := client.R().Get(src)
	if err != nil {
		return nil, err
	}
	writer, f, err := s.storage.Writer(filename)
	if err != nil {
		slog.Error("创建资源信息出错", "filename", filename, "src", src, "Error", err)
		return nil, errors.New("创建资源信息出错")
	}
	defer func() {
		_ = writer.Close()
	}()
	if _, err := writer.Write(resource.Body()); err != nil {
		slog.Error("写入资源信息出错", "filename", filename, "src", src, "Error", err)
		return nil, errors.New("写入资源信息出错")
	}
	slog.Info("下载资源完成", "filename", filename, "src", src, "path", f)
	return s.getResourceFromStorage(f)
}
