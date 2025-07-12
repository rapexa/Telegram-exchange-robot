package main

import (
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"telegram-exchange-robot/config"
	"telegram-exchange-robot/handlers"
	"telegram-exchange-robot/models"
)

func main() {
	// Load config
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Connect to MySQL using GORM
	db, err := gorm.Open(mysql.Open(cfg.MySQL.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("Error connecting to MySQL: %v", err)
	}

	// Auto-migrate the User model
	if err := db.AutoMigrate(&models.User{}); err != nil {
		log.Fatalf("Error migrating database: %v", err)
	}

	// Initialize Telegram Bot
	bot, err := tgbotapi.NewBotAPI(cfg.Telegram.Token)
	if err != nil {
		log.Fatalf("Error creating Telegram bot: %v", err)
	}
	bot.Debug = cfg.Telegram.Debug

	log.Printf("Authorized on account %s", bot.Self.UserName)

	// Start bot handler
	handlers.StartBot(bot, db)
}
