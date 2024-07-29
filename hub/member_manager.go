package hub

import (
	"crypto/md5"
	"errors"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"gorm.io/gorm"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

func generateID(id string) string {
	hasher := md5.New()
	hasher.Write([]byte(id))
	digest := hasher.Sum(nil)
	return fmt.Sprintf("%x", digest)[:8]
}

type (
	MemberManager interface {
		GroupMemberManager
		GetID(user *openwechat.User) (string, error)
		GetName(id string) (string, error)
		GetByID(id string) (string, error)
	}

	GroupMemberManager interface {
		RefreshGroupMember() map[string][]GroupUser
		GetGroupUsers(gid string) (map[string]GroupUser, error)
		AddGroupUser(gid string, uid string, nickname string) error
		UpdateGroupUser(gid string, uid string, nickname string) error
		LeaveGroupUser(gid string, uid string) error
	}

	dBMemberManger struct {
		bot        *openwechat.Bot
		db         *gorm.DB
		idUserMap  sync.Map
		uidUserMap sync.Map
	}

	member struct {
		ID         string `gorm:"primaryKey;type:varchar(40)"`
		Type       int8   `gorm:""` // 0:群,1:用户
		AttrID     string `gorm:"type:varchar(255)"`
		Nickname   string `gorm:"type:varchar(255)"`
		UID        string `gorm:"column:uid;type:varchar(100)"`
		UpdateTime int64  `gorm:"autoCreateTime:milli;autoUpdateTime:milli"`
	}

	GroupUser struct {
		GID        string `gorm:"primaryKey;column:gid;type:varchar(40)" json:"gid"`  // 群id
		UID        string `gorm:"primaryKey;column:uid;type:varchar(40)" json:"uid"`  // 用户id
		Nickname   string `gorm:"type:varchar(255)" json:"nickname"`                  // 群名片
		UpdateTime int64  `gorm:"autoCreateTime:milli;autoUpdateTime:milli" json:"-"` // 更新时间
		LeaveTime  int64  `gorm:"" json:"leaveTime"`                                  // 退群时间
	}
)

func NewMemberManger(bot *openwechat.Bot, db *gorm.DB) MemberManager {
	if err := db.AutoMigrate(member{}); err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(GroupUser{}); err != nil {
		panic(err)
	}
	return &dBMemberManger{
		bot:        bot,
		db:         db,
		idUserMap:  sync.Map{},
		uidUserMap: sync.Map{},
	}
}

// compareAndUpdateMember 比较更新成员信息
func (m *dBMemberManger) compareAndUpdateMember(user *openwechat.User, u member) {
	attrId := strconv.Itoa(int(user.AttrStatus))
	if user.IsGroup() {
		attrId = user.AvatarID()
	}
	if u.AttrID != attrId || u.Nickname != user.NickName || u.UID != user.UserName {
		u.AttrID = attrId
		u.UID = user.UserName
		u.Nickname = user.NickName
		db := m.db.Model(&u).Updates(u)
		if db.Error != nil {
			slog.Error("更新成员信息出错", "id", u.ID, "uid", user.UserName, "error", db.Error)
		} else {
			slog.Info("更新成员信息", "id", u.ID, "uid", user.UserName)
		}
		m.idUserMap.Store(u.ID, u)
		m.uidUserMap.Store(u.UID, u)
	}
}

// GetID 根据用户获取id
func (m *dBMemberManger) GetID(user *openwechat.User) (string, error) {
	// 查询缓存
	if val, ok := m.uidUserMap.Load(user.UserName); ok {
		u := val.(member)
		m.compareAndUpdateMember(user, u)
		return u.ID, nil
	}
	// 查db
	var u member
	// 根据id查询
	db := m.db.Where("uid = ?", user.UserName).Take(&u)
	if db.Error == nil {
		m.compareAndUpdateMember(user, u)
		return u.ID, nil
	} else if db.Error != nil && !errors.Is(db.Error, gorm.ErrRecordNotFound) {
		slog.Error("查询uid出错", "uid", user.UserName, "error", db.Error)
		return "", db.Error
	}
	var attrId string
	var userType int8
	if user.IsGroup() {
		attrId = user.AvatarID()
		userType = 0
	} else {
		attrId = strconv.Itoa(int(user.AttrStatus))
		userType = 1
	}

	db = m.db.Where("nickname = ? and attr_id = ?", user.NickName, attrId).Take(&u)
	if db.Error == nil {
		m.compareAndUpdateMember(user, u)
		return u.ID, nil
	} else if db.Error != nil && !errors.Is(db.Error, gorm.ErrRecordNotFound) {
		slog.Error("查询id出错", "type", userType, "name", user.UserName, "attrId", attrId, "error", db.Error)
		return "", db.Error
	}

	// 群组还需要只使用群名匹配一次
	if user.IsGroup() {
		db = m.db.Where("nickname = ? and type = ?", user.NickName, userType).Take(&u)
		if db.Error == nil {
			m.compareAndUpdateMember(user, u)
			return u.ID, nil
		} else if db.Error != nil && !errors.Is(db.Error, gorm.ErrRecordNotFound) {
			slog.Error("查询id出错", "type", userType, "name", user.UserName, "error", db.Error)
			return "", db.Error
		}
	}
	// 没查到，新增
	u = member{
		ID:       generateID(user.UserName),
		Type:     userType,
		Nickname: user.NickName,
		UID:      user.UserName,
		AttrID:   attrId,
	}
	db = m.db.Create(&u)
	if db.Error != nil {
		slog.Error("新增成员信息出错", "id", u.ID, "uid", user.UserName, "name", user.NickName, "error", db.Error)
		return "", db.Error
	} else {
		slog.Info("新增成员信息", "id", u.ID, "uid", user.UserName, "name", user.NickName)
	}
	m.idUserMap.Store(u.ID, u)
	m.uidUserMap.Store(u.UID, u)
	return u.ID, nil
}

func (m *dBMemberManger) GetName(id string) (string, error) {
	// 插缓存
	if val, ok := m.idUserMap.Load(id); ok {
		u := val.(member)
		return u.Nickname, nil
	}
	// 查db
	var u member
	db := m.db.Where("id = ?", id).Take(&u)
	if db.Error != nil && !errors.Is(db.Error, gorm.ErrRecordNotFound) {
		slog.Error("查询uid出错", "id", id, "error", db.Error)
		return "", db.Error
	} else if errors.Is(db.Error, gorm.ErrRecordNotFound) {
		return "", nil
	} else {
		m.idUserMap.Store(id, u)
		m.uidUserMap.Store(u.UID, u)
		return u.Nickname, nil
	}
}

// GetByID 根据id获取微信临时uid
func (m *dBMemberManger) GetByID(id string) (string, error) {
	// 插缓存
	if val, ok := m.idUserMap.Load(id); ok {
		u := val.(member)
		return u.UID, nil
	}
	// 查db
	var u member
	db := m.db.Where("id = ?", id).Take(&u)
	if db.Error != nil && !errors.Is(db.Error, gorm.ErrRecordNotFound) {
		slog.Error("查询uid出错", "id", id, "error", db.Error)
		return "", db.Error
	} else if errors.Is(db.Error, gorm.ErrRecordNotFound) {
		return "", nil
	} else {
		m.idUserMap.Store(id, u)
		m.uidUserMap.Store(u.UID, u)
		return u.UID, nil
	}
}

// GetGroupUsers 获取群成员
func (m *dBMemberManger) GetGroupUsers(gid string) (map[string]GroupUser, error) {
	var users []GroupUser
	db := m.db.Where("gid = ?", gid).Find(&users)
	if db.Error != nil {
		slog.Error("查询群成员出错", "gid", gid, "error", db.Error)
		return nil, db.Error
	}
	userMap := make(map[string]GroupUser, len(users))
	for _, u := range users {
		userMap[u.UID] = u
	}
	return userMap, nil
}

// AddGroupUser 添加群成员
func (m *dBMemberManger) AddGroupUser(gid string, uid string, nickname string) error {
	return m.db.Create(&GroupUser{
		GID:      gid,
		UID:      uid,
		Nickname: nickname,
	}).Error
}

// AddGroupUser 添加群成员
func (m *dBMemberManger) reAddGroupUser(gid string, uid string) error {
	return m.db.Model(&GroupUser{}).Where("gid = ? and uid = ?", gid, uid).Update("leave_time", 0).Error
}

// UpdateGroupUser 更新群成员信息
func (m *dBMemberManger) UpdateGroupUser(gid string, uid string, nickname string) error {
	return m.db.Model(&GroupUser{}).Where("gid = ? and uid = ?", gid, uid).Update("nickname", nickname).Error
}

// LeaveGroupUser 成员退群
func (m *dBMemberManger) LeaveGroupUser(gid string, uid string) error {
	return m.db.Model(&GroupUser{}).Where("gid = ? and uid = ?", gid, uid).Update("leave_time", time.Now().UnixMilli()).Error
}

func (m *dBMemberManger) RefreshGroupMember() map[string][]GroupUser {
	self, err := m.bot.GetCurrentUser()
	if err != nil {
		slog.Error("获取当前用户失败", "error", err)
		return nil
	}
	groups, err := self.Groups(true)
	if err != nil {
		slog.Error("刷新群列表失败", "error", err)
		return nil
	}
	exitGroupUserMap := make(map[string][]GroupUser)
	for _, group := range groups {
		gid, err := m.GetID(group.User)
		if err != nil {
			slog.Error("获取群id失败", "error", err)
			continue
		}
		members, _ := group.Members()
		groupMembers, err := m.GetGroupUsers(gid)
		if err != nil {
			slog.Error("获取群成员信息失败", "error", err)
			continue
		}
		for _, member := range members {
			uid, err := m.GetID(member)
			if err != nil {
				slog.Error("获取群成员id失败", "error", err)
				continue
			}
			nickname := member.NickName
			if member.DisplayName != "" {
				nickname = member.DisplayName
			}
			if user, ok := groupMembers[uid]; !ok {
				if err = m.AddGroupUser(gid, uid, nickname); err != nil {
					slog.Error("添加群成员失败", "gid", gid, "id", uid, "nickname", nickname, "error", err)
				} else {
					slog.Info("添加群成员", "gid", gid, "id", uid, "nickname", nickname)
				}
			} else if nickname != user.Nickname {
				if err = m.UpdateGroupUser(gid, uid, nickname); err != nil {
					slog.Error("更新群成员信息失败", "gid", gid, "id", uid, "nickname", nickname, "error", err)
				} else {
					slog.Info("更新群成员信息", "gid", gid, "id", uid, "nickname", nickname)
				}
			} else if user.LeaveTime > 0 {
				if err = m.reAddGroupUser(gid, uid); err != nil {
					slog.Error("重置群成员信息失败", "gid", gid, "id", uid, "nickname", nickname, "error", err)
				} else {
					slog.Info("重置群成员信息", "gid", gid, "id", uid, "nickname", nickname)
				}
			}
			delete(groupMembers, uid)
		}
		// 剩下为离开群的成员
		existUsers := make([]GroupUser, 0, len(groupMembers))
		for uid, user := range groupMembers {
			if user.LeaveTime > 0 {
				continue
			}
			existUsers = append(existUsers, user)
			if err = m.LeaveGroupUser(gid, uid); err != nil {
				slog.Error("删除群成员失败", "gid", gid, "id", uid, "error", err)
			} else {
				slog.Info("删除群成员", "gid", gid, "id", uid)
			}
		}
		if len(existUsers) > 0 {
			exitGroupUserMap[gid] = existUsers
		}
	}
	return exitGroupUserMap

}
