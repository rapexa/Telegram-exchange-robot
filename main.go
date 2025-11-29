package main

import (
	"io"
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
		// Write to both file and stdout
		multiWriter := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(multiWriter)
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

// initializeSuperAdmins creates initial super admin accounts
func initializeSuperAdmins(db *gorm.DB) {
	// List of super admin telegram IDs (same as current hardcoded admins)
	superAdminIDs := []int64{7403868937, 7947533993}

	for _, telegramID := range superAdminIDs {
		// Check if admin already exists
		if !models.IsAdminExists(db, telegramID) {
			// Create super admin
			admin, err := models.CreateSuperAdmin(db, telegramID, "", "Super Admin")
			if err != nil {
				logError("Failed to create super admin %d: %v", telegramID, err)
			} else {
				logInfo("✅ Created super admin: %d (ID: %d)", telegramID, admin.ID)
			}
		} else {
			logInfo("ℹ️ Super admin %d already exists", telegramID)
		}
	}
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
	if err := db.AutoMigrate(&models.User{}, &models.Transaction{}, &models.TradeResult{}, &models.TradeRange{}, &models.Rate{}, &models.Settings{}, &models.BankAccount{}, &models.Admin{}, &models.AdminPermissionRecord{}, &models.AdminLog{}); err != nil {
		logError("Failed to migrate database: %v", err)
		os.Exit(1)
	}
	logInfo("✅ Database migration completed")

	// Initialize default settings
	logInfo("🔧 Initializing default settings...")
	handlers.InitializeDefaultSettings(db)
	logInfo("✅ Default settings initialized")

	// Initialize super admins
	logInfo("👑 Initializing super admins...")
	initializeSuperAdmins(db)
	logInfo("✅ Super admins initialized")

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

	// به‌روزرسانی موجودی کاربران از تراکنش‌های قبلی
	logInfo("💰 Updating user balances from existing transactions...")
	if err := models.UpdateUserBalancesFromTransactions(db); err != nil {
		logError("Failed to update user balances: %v", err)
	} else {
		logInfo("✅ User balances updated successfully")
	}

	// DISABLED: پاداش رفرال فقط برای تریدها پرداخت می‌شود، نه برای واریز
	// Referral rewards are only given when users perform TRADES, not deposits or withdrawals
	// logInfo("🎁 Processing referral rewards for existing deposits...")
	// handlers.ProcessReferralRewardsForDeposits(bot, db)

	// Run comprehensive blockchain sync at startup
	logInfo("🔗 Starting comprehensive blockchain sync at startup...")
	startupSyncStats := models.SyncAllUserDepositsWithStats(db, cfg.EtherscanAPIKey)
	if startupSyncStats.Error != nil {
		logError("Initial blockchain sync error: %v", startupSyncStats.Error)
	} else {
		logInfo("✅ Startup blockchain sync completed:")
		logInfo("   📊 Total users checked: %d", startupSyncStats.TotalUsers)
		logInfo("   💰 New deposits found: %d (ERC20: %d, BEP20: %d)",
			startupSyncStats.NewDeposits, startupSyncStats.NewERC20Deposits, startupSyncStats.NewBEP20Deposits)
		logInfo("   💸 New withdrawals found: %d (ERC20: %d, BEP20: %d)",
			startupSyncStats.NewWithdrawals, startupSyncStats.NewERC20Withdrawals, startupSyncStats.NewBEP20Withdrawals)
		logInfo("   ⏭️ Skipped (already exists): %d", startupSyncStats.SkippedTransactions)
		if startupSyncStats.NewDeposits > 0 || startupSyncStats.NewWithdrawals > 0 {
			logInfo("   🎉 Found %d new transactions from blockchain!",
				startupSyncStats.NewDeposits+startupSyncStats.NewWithdrawals)
		}
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

	// Start auto USDT price update service (every 3 minutes)
	go func() {
		// Wait a bit before starting to ensure everything is initialized
		time.Sleep(5 * time.Second)
		models.AutoUpdateUSDTPrice(db, 3*time.Minute)
	}()
	logInfo("✅ Auto USDT price update service started (updates every 3 minutes from Nobitex)")

	handlers.StartBot(bot, db, cfg)
}
