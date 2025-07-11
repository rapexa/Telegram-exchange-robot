package models

import (
	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Username   string `gorm:"size:255"`
	TelegramID int64  `gorm:"uniqueIndex"`
	FullName   string `gorm:"size:255"`
	Sheba      string `gorm:"size:32"`
	CardNumber string `gorm:"size:32"`
	Registered bool   `gorm:"default:false"`
}
