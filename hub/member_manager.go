package hub

import (
	"crypto/md5"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"sync"
	"time"
	"wechat-hub/db"
)

func generateID(id string) string {
	hasher := md5.New()
	hasher.Write([]byte(id))
	digest := hasher.Sum(nil)
	return fmt.Sprintf("%x", digest)[:8]
}

type MemberManager struct {
	storage db.Storage
	idMap   sync.Map
	infoMap sync.Map
}

func NewIDStorage(storage db.Storage) *MemberManager {
	return &MemberManager{
		storage: storage,
		idMap:   sync.Map{},
		infoMap: sync.Map{},
	}
}

func (s *MemberManager) getKeys(user *openwechat.User) []string {
	keys := []string{
		user.UserName, // 微信id
		fmt.Sprintf("%s_%s", user.NickName, user.AvatarID()), // 名称、头像id
	}
	if !user.IsGroup() {
		keys = append(keys, fmt.Sprintf("%d_%s", user.AttrStatus, user.NickName))
		if user.DisplayName != "" && user.DisplayName != user.NickName {
			keys = append(keys, fmt.Sprintf("%d_%s", user.AttrStatus, user.DisplayName)) // attr、群名/备注
			keys = append(keys, fmt.Sprintf("%s_%s", user.NickName, user.DisplayName))   // 微信名、群名/备注
		}
	}
	keys = append(keys, user.NickName)
	return keys
}

func (s *MemberManager) getInfo(user *openwechat.User) string {
	if user.AvatarID() == "" {
		_ = user.Detail()
	}
	if user.IsGroup() {
		return fmt.Sprintf("%s_%s", user.NickName, user.AvatarID())
	}
	return fmt.Sprintf("%s_%s_%d", user.NickName, user.AvatarID(), user.AttrStatus)
}

func (s *MemberManager) GetUID(id string) string {
	val, ok := s.idMap.Load(id)
	if !ok {
		return ""
	}
	return val.(string)
}

func (s *MemberManager) GetID(user *openwechat.User) (string, error) {
	keys := s.getKeys(user)
	if id, ok := s.idMap.Load(user.UserName); ok {
		compare := s.getInfo(user)
		if info, ok := s.infoMap.Load(id); ok && info == compare {
			return id.(string), nil
		}
		s.infoMap.Store(id, compare)
		return id.(string), s.storage.SaveKeysWithTTL(s.getKeys(user), generateID(keys[0]), time.Hour*24*60)
	}
	return s.storage.GetOrSaveKeys(keys, func(keys []string) (string, time.Duration) {
		return generateID(keys[0]), time.Hour * 24 * 60
	})
}
