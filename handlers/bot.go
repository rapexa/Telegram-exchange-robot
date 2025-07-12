package handlers

import (
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gorm.io/gorm"

	"telegram-exchange-robot/models"
)

// Registration state tracking
var regState = struct {
	m map[int64]string
	sync.RWMutex
}{m: make(map[int64]string)}

var regTemp = struct {
	m map[int64]map[string]string
	sync.RWMutex
}{m: make(map[int64]map[string]string)}

func StartBot(bot *tgbotapi.BotAPI, db *gorm.DB) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		// Registration flow state machine
		if handleRegistration(bot, db, update.Message) {
			continue
		}

		if update.Message.IsCommand() {
			switch update.Message.Command() {
			case "start":
				handleStart(bot, db, update.Message)
			}
			continue
		}

		// Main menu navigation
		handleMainMenu(bot, db, update.Message)
	}
}

// handleRegistration manages the registration state machine. Returns true if in registration flow.
func handleRegistration(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) bool {
	userID := int64(msg.From.ID)
	regState.RLock()
	state, inReg := regState.m[userID]
	regState.RUnlock()
	if !inReg {
		return false
	}

	if state == "full_name" {
		// Validate Persian full name format
		if !models.ValidatePersianFullName(msg.Text) {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "فرمت نام صحیح نیست. لطفاً نام و نام خانوادگی را به فارسی وارد کنید:\nمثال: علی احمدی\n\nنکات مهم:\n• نام و نام خانوادگی باید به فارسی باشد\n• حداقل دو کلمه (نام و نام خانوادگی) الزامی است\n• هر کلمه حداقل ۲ حرف باشد"))
			return true
		}
		// Save full name, ask for Sheba
		saveRegTemp(userID, "full_name", msg.Text)
		setRegState(userID, "sheba")
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "لطفاً شماره شبا را وارد کنید:"))
		return true
	} else if state == "sheba" {
		// Validate Sheba format
		if !models.ValidateSheba(msg.Text) {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "فرمت شماره شبا صحیح نیست. لطفاً شماره شبا را به فرمت صحیح وارد کنید:\nمثال: IR520630144905901219088011"))
			return true
		}
		// Save Sheba, ask for card number
		saveRegTemp(userID, "sheba", msg.Text)
		setRegState(userID, "card_number")
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "لطفاً شماره کارت را وارد کنید:"))
		return true
	} else if state == "card_number" {
		// Validate card number format
		if !models.ValidateCardNumber(msg.Text) {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "فرمت شماره کارت صحیح نیست. لطفاً شماره کارت را به فرمت صحیح وارد کنید:\nمثال: 6037998215325563"))
			return true
		}
		// Save card number, complete registration
		saveRegTemp(userID, "card_number", msg.Text)
		regTemp.RLock()
		info := regTemp.m[userID]
		regTemp.RUnlock()
		// Register user using GORM
		err := registerUser(db, userID, info["full_name"], info["sheba"], info["card_number"])
		if err != nil {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "خطا در ثبت اطلاعات. لطفاً دوباره تلاش کنید."))
			return true
		}
		clearRegState(userID)
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "ثبت‌نام با موفقیت انجام شد!"))
		showMainMenu(bot, msg.Chat.ID)
		return true
	}
	return false
}

func handleStart(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		// User doesn't exist, create new user record
		newUser := &models.User{
			Username:   msg.From.UserName,
			TelegramID: userID,
			Registered: false,
		}
		if err := db.Create(newUser).Error; err != nil {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "خطا در ایجاد کاربر. لطفاً دوباره تلاش کنید."))
			return
		}
		// Start registration
		setRegState(userID, "full_name")
		regTemp.Lock()
		regTemp.m[userID] = make(map[string]string)
		regTemp.Unlock()
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "لطفاً نام و نام خانوادگی خود را وارد کنید:"))
		return
	}

	if !user.Registered {
		// User exists but not registered, start registration
		setRegState(userID, "full_name")
		regTemp.Lock()
		regTemp.m[userID] = make(map[string]string)
		regTemp.Unlock()
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "لطفاً نام و نام خانوادگی خود را وارد کنید:"))
		return
	}

	// Already registered
	showMainMenu(bot, msg.Chat.ID)
}

func handleMainMenu(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	switch msg.Text {
	case "💰 کیف پول":
		showWalletMenu(bot, msg.Chat.ID)
	case "🎁 پاداش":
		showRewardsMenu(bot, msg.Chat.ID)
	case "📊 آمار":
		showStatsMenu(bot, msg.Chat.ID)
	case "🆘 پشتیبانی":
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "برای پشتیبانی با ادمین تماس بگیرید: @YourAdminUsername"))
	default:
		showMainMenu(bot, msg.Chat.ID)
	}
}

func showMainMenu(bot *tgbotapi.BotAPI, chatID int64) {
	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💰 کیف پول"),
			tgbotapi.NewKeyboardButton("🎁 پاداش"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 آمار"),
			tgbotapi.NewKeyboardButton("🆘 پشتیبانی"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, "منوی اصلی را انتخاب کنید:")
	msg.ReplyMarkup = menu
	bot.Send(msg)
}

func showWalletMenu(bot *tgbotapi.BotAPI, chatID int64) {
	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💵 برداشت"),
			tgbotapi.NewKeyboardButton("📋 تاریخچه"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💳 واریز USDT"),
			tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, "منوی کیف پول:")
	msg.ReplyMarkup = menu
	bot.Send(msg)
}

func showRewardsMenu(bot *tgbotapi.BotAPI, chatID int64) {
	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔗 لینک رفرال"),
			tgbotapi.NewKeyboardButton("💰 دریافت پاداش"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, "منوی پاداش:")
	msg.ReplyMarkup = menu
	bot.Send(msg)
}

func showStatsMenu(bot *tgbotapi.BotAPI, chatID int64) {
	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📈 آمار شخصی"),
			tgbotapi.NewKeyboardButton("👥 زیرمجموعه‌ها"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, "منوی آمار:")
	msg.ReplyMarkup = menu
	bot.Send(msg)
}

// --- Registration state helpers ---
func setRegState(userID int64, state string) {
	regState.Lock()
	regState.m[userID] = state
	regState.Unlock()
}

func clearRegState(userID int64) {
	regState.Lock()
	delete(regState.m, userID)
	regState.Unlock()
	regTemp.Lock()
	delete(regTemp.m, userID)
	regTemp.Unlock()
}

func saveRegTemp(userID int64, key, value string) {
	regTemp.Lock()
	if regTemp.m[userID] == nil {
		regTemp.m[userID] = make(map[string]string)
	}
	regTemp.m[userID][key] = value
	regTemp.Unlock()
}

// --- GORM-based user helpers ---
func getUserByTelegramID(db *gorm.DB, telegramID int64) (*models.User, error) {
	var user models.User
	err := db.Where("telegram_id = ?", telegramID).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func registerUser(db *gorm.DB, telegramID int64, fullName, sheba, cardNumber string) error {
	return db.Model(&models.User{}).
		Where("telegram_id = ?", telegramID).
		Updates(map[string]interface{}{
			"full_name":   fullName,
			"sheba":       sheba,
			"card_number": cardNumber,
			"registered":  true,
		}).Error
}
