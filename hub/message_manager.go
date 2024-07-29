package hub

import (
	"gorm.io/gorm"
	"wechat-hub/pkg/lru"
)

type (
	MessageManager interface {
		Exist(string) (bool, error)
		Save(Message) error
	}
	message struct {
		ID        string `gorm:"primaryKey;type:varchar(50)"`
		MsgType   int    `gorm:"type:int(2)"`
		Time      int64  `gorm:"type:int(20)"`
		GID       string `gorm:"column:gid;type:varchar(40)"`
		GroupName string `gorm:"type:varchar(255)"`
		UID       string `gorm:"column:uid;type:varchar(40)"`
		Nickname  string `gorm:"type:varchar(255)"`
		Content   string `gorm:""`
	}

	dbMessageManager struct {
		db           *gorm.DB
		existIdCache *lru.LRU[string, any]
	}
)

func NewMessageManager(db *gorm.DB) MessageManager {
	if err := db.AutoMigrate(message{}); err != nil {
		panic(err)
	}
	return &dbMessageManager{
		db:           db,
		existIdCache: lru.New[string, any](1000),
	}
}

func (d *dbMessageManager) Exist(id string) (bool, error) {
	if d.existIdCache.Exist(id) {
		return true, nil
	}
	db := d.db.Model(&message{}).Where("id = ?", id).Take(&message{})
	if db.Error == nil {
		d.existIdCache.Put(id, nil)
		return true, nil
	}
	return false, db.Error
}

func (d *dbMessageManager) Save(msg Message) error {
	groupId, groupName := msg.Group()
	userId, nickname := msg.User()
	return d.db.Create(&message{
		ID:        msg.ID(),
		MsgType:   msg.Type(),
		Time:      msg.MsgTime(),
		GID:       groupId,
		GroupName: groupName,
		UID:       userId,
		Nickname:  nickname,
		Content:   msg.Message(),
	}).Error
}
