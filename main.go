package main

import (
	"log"
	"os"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"telegram-exchange-robot/config"
	"telegram-exchange-robot/handlers"
	"telegram-exchange-robot/models"
)

const (
	Version = "dev"
	Build   = "unknown"
)

func initLogger() {
	// Set log format for production
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// Create logs directory if it doesn't exist
	if err := os.MkdirAll("logs", 0755); err != nil {
		log.Printf("WARNING: Could not create logs directory: %v", err)
	}

	// Open log file
	logFile, err := os.OpenFile("logs/bot.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("WARNING: Could not open log file: %v", err)
	} else {
		log.SetOutput(logFile)
	}
}

func logInfo(format string, v ...interface{}) {
	log.Printf("[INFO] "+format, v...)
}

func logError(format string, v ...interface{}) {
	log.Printf("[ERROR] "+format, v...)
}

func logDebug(format string, v ...interface{}) {
	log.Printf("[DEBUG] "+format, v...)
}

func main() {
	// Initialize logger
	initLogger()

	// Startup banner
	separator := strings.Repeat("=", 60)
	logInfo(separator)
	logInfo("🚀 Telegram Exchange Bot Starting...")
	logInfo("📦 Version: %s", Version)
	logInfo("🔨 Build: %s", Build)
	logInfo("⏰ Start Time: %s", time.Now().Format("2006-01-02 15:04:05"))
	logInfo(separator)

	// Load config
	logInfo("📋 Loading configuration...")
	cfg, err := config.LoadConfig()
	if err != nil {
		logError("Failed to load config: %v", err)
		os.Exit(1)
	}
	logInfo("✅ Configuration loaded successfully")

	// Connect to MySQL using GORM
	logInfo("🗄️ Connecting to MySQL database...")
	db, err := gorm.Open(mysql.Open(cfg.MySQL.DSN()), &gorm.Config{})
	if err != nil {
		logError("Failed to connect to MySQL: %v", err)
		os.Exit(1)
	}

	// Test database connection
	sqlDB, err := db.DB()
	if err != nil {
		logError("Failed to get underlying sql.DB: %v", err)
		os.Exit(1)
	}

	if err := sqlDB.Ping(); err != nil {
		logError("Failed to ping database: %v", err)
		os.Exit(1)
	}
	logInfo("✅ Database connection successful")

	// Auto-migrate the User and Transaction models
	logInfo("🔄 Running database migrations...")
	if err := db.AutoMigrate(&models.User{}, &models.Transaction{}, &models.TradeResult{}, &models.TradeRange{}); err != nil {
		logError("Failed to migrate database: %v", err)
		os.Exit(1)
	}
	logInfo("✅ Database migration completed")

	// Initialize Telegram Bot
	logInfo("🤖 Initializing Telegram bot...")
	bot, err := tgbotapi.NewBotAPI(cfg.Telegram.Token)
	if err != nil {
		logError("Failed to create Telegram bot: %v", err)
		os.Exit(1)
	}
	bot.Debug = cfg.Telegram.Debug

	logInfo("✅ Bot authorized successfully")
	logInfo("🤖 Bot Username: @%s", bot.Self.UserName)
	logInfo("🆔 Bot ID: %d", bot.Self.ID)
	logInfo("🔧 Debug Mode: %v", cfg.Telegram.Debug)

	// Start bot handler
	logInfo("🎯 Starting bot handler...")
	logInfo(separator)
	logInfo("🚀 Bot is now running and ready to receive messages!")
	logInfo(separator)

	// Run blockchain deposit sync once at startup
	err = models.SyncAllUserDeposits(db, cfg.EtherscanAPIKey)
	if err != nil {
		logError("Initial blockchain sync error: %v", err)
	} else {
		logInfo("Initial blockchain deposit sync completed successfully")
	}

	// Start blockchain deposit sync goroutine (every 5 minutes)
	go func() {
		for {
			err := models.SyncAllUserDeposits(db, cfg.EtherscanAPIKey)
			if err != nil {
				logError("Blockchain sync error: %v", err)
			} else {
				logInfo("Blockchain deposit sync completed successfully")
			}
			time.Sleep(5 * time.Minute)
		}
	}()

	handlers.StartBot(bot, db)
}
