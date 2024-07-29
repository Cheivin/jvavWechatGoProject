package auth

import (
	"crypto/md5"
	"errors"
	"fmt"
	"gorm.io/gorm"
)

type (
	Manager struct {
		db *gorm.DB
	}
	User struct {
		ID       int    `gorm:"primarykey;AUTO_INCREMENT"`
		Username string `gorm:"unique"`
		Password string `gorm:"not null"`
	}
)

func (User) TableName() string {
	return "auth_user"
}
func NewAuthManager(db *gorm.DB) *Manager {
	if err := db.AutoMigrate(&User{}); err != nil {
		panic(err)
	}
	return &Manager{db: db}
}

func (m *Manager) CreateUser(username, password string) error {
	password = fmt.Sprintf("%x", md5.Sum([]byte(password)))
	return m.db.Create(&User{Username: username, Password: password}).Error
}

func (m *Manager) FindUser(username string) (*User, error) {
	var user User
	err := m.db.Where("username = ?", username).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &user, err
}

func (m *Manager) CheckUser(username, password string) bool {
	user, err := m.FindUser(username)
	if err != nil || user == nil {
		return false
	}
	return user.Password == fmt.Sprintf("%x", md5.Sum([]byte(password)))
}

func (m *Manager) DeleteUser(username string) error {
	return m.db.Delete(&User{}, "username = ?", username).Error
}
