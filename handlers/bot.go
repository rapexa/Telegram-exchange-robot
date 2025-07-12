package handlers

import (
	"fmt"
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
			case "fixuser":
				handleFixUser(bot, db, update.Message)
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

	fmt.Printf("Registration state for user %d: %s\n", userID, state)

	if state == "full_name" {
		// Validate Persian full name format
		if !models.ValidatePersianFullName(msg.Text) {
			errorMsg := `❌ *خطا در فرمت نام*

فرمت نام صحیح نیست. لطفاً نام و نام خانوادگی را به فارسی وارد کنید:

📝 *مثال صحیح:* علی احمدی

💡 *نکات مهم:*
• نام و نام خانوادگی باید به فارسی باشد
• حداقل دو کلمه (نام و نام خانوادگی) الزامی است
• هر کلمه حداقل ۲ حرف باشد

🔄 لطفاً دوباره تلاش کنید:`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "Markdown"
			bot.Send(message)
			return true
		}
		// Save full name, ask for Sheba
		fmt.Printf("Saving full name: %s for user %d\n", msg.Text, userID)
		saveRegTemp(userID, "full_name", msg.Text)
		setRegState(userID, "sheba")

		shebaMsg := `✅ *مرحله ۱ تکمیل شد!*

👤 نام و نام خانوادگی: *%s*

📝 *مرحله ۲: شماره شبا*

لطفاً شماره شبا حساب بانکی خود را وارد کنید:
مثال: IR520630144905901219088011

💡 *نکات مهم:*
• شماره شبا باید با IR شروع شود
• شامل ۲۴ رقم بعد از IR باشد
• بدون فاصله یا کاراکتر اضافی`

		message := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(shebaMsg, msg.Text))
		message.ParseMode = "Markdown"
		bot.Send(message)
		return true
	} else if state == "sheba" {
		// Validate Sheba format
		fmt.Printf("Validating sheba: '%s'\n", msg.Text)
		if !models.ValidateSheba(msg.Text) {
			fmt.Printf("Sheba validation failed for: '%s'\n", msg.Text)

			errorMsg := `❌ *خطا در فرمت شماره شبا*

فرمت شماره شبا صحیح نیست. لطفاً شماره شبا را به فرمت صحیح وارد کنید:

🏦 *مثال صحیح:* IR520630144905901219088011

💡 *نکات مهم:*
• شماره شبا باید با IR شروع شود
• شامل ۲۴ رقم بعد از IR باشد
• بدون فاصله یا کاراکتر اضافی

🔄 لطفاً دوباره تلاش کنید:`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "Markdown"
			bot.Send(message)
			return true
		}
		// Save Sheba, ask for card number
		fmt.Printf("Saving sheba: %s for user %d\n", msg.Text, userID)
		saveRegTemp(userID, "sheba", msg.Text)
		setRegState(userID, "card_number")

		cardMsg := `✅ *مرحله ۲ تکمیل شد!*

🏦 شماره شبا: *%s*

📝 *مرحله ۳: شماره کارت*

لطفاً شماره کارت بانکی خود را وارد کنید:
مثال: 6037998215325563

💡 *نکات مهم:*
• شماره کارت باید ۱۶ رقم باشد
• بدون فاصله یا کاراکتر اضافی
• فقط اعداد مجاز هستند`

		message := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(cardMsg, msg.Text))
		message.ParseMode = "Markdown"
		bot.Send(message)
		return true
	} else if state == "card_number" {
		// Validate card number format
		if !models.ValidateCardNumber(msg.Text) {
			errorMsg := `❌ *خطا در فرمت شماره کارت*

فرمت شماره کارت صحیح نیست. لطفاً شماره کارت را به فرمت صحیح وارد کنید:

💳 *مثال صحیح:* 6037998215325563

💡 *نکات مهم:*
• شماره کارت باید ۱۶ رقم باشد
• بدون فاصله یا کاراکتر اضافی
• فقط اعداد مجاز هستند

🔄 لطفاً دوباره تلاش کنید:`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "Markdown"
			bot.Send(message)
			return true
		}
		// Save card number, complete registration
		fmt.Printf("Saving card number: %s for user %d\n", msg.Text, userID)
		saveRegTemp(userID, "card_number", msg.Text)
		regTemp.RLock()
		info := regTemp.m[userID]
		regTemp.RUnlock()

		fmt.Printf("Completing registration for user %d with info: %+v\n", userID, info)

		err := registerUser(db, userID, info["full_name"], info["sheba"], info["card_number"])
		if err != nil {
			fmt.Printf("Error registering user: %v\n", err)
			errorMsg := `❌ *خطا در ثبت اطلاعات*

متأسفانه خطایی در ثبت اطلاعات رخ داد. لطفاً دوباره تلاش کنید.

🔄 برای شروع مجدد، دستور /start را بزنید.`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "Markdown"
			bot.Send(message)
			return true
		}

		fmt.Printf("Registration completed successfully for user %d\n", userID)
		clearRegState(userID)

		successMsg := `🎉 *ثبت‌نام با موفقیت تکمیل شد!*

✅ تمام مراحل ثبت‌نام با موفقیت انجام شد.

👤 *اطلاعات ثبت شده:*
• نام و نام خانوادگی: *%s*
• شماره شبا: *%s*
• شماره کارت: *%s*

🚀 حالا می‌توانید از تمام خدمات ربات استفاده کنید!

👇 منوی اصلی را انتخاب کنید:`

		message := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(successMsg, info["full_name"], info["sheba"], info["card_number"]))
		message.ParseMode = "Markdown"
		bot.Send(message)

		showMainMenu(bot, msg.Chat.ID)
		return true
	}
	return false
}

func handleStart(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	user, err := getUserByTelegramID(db, userID)

	// Debug logging
	fmt.Printf("User ID: %d, Error: %v, User: %+v\n", userID, err, user)

	// If user doesn't exist, create new user record
	if err != nil || user == nil {
		fmt.Printf("Creating new user for ID: %d\n", userID)
		newUser := &models.User{
			Username:   msg.From.UserName,
			TelegramID: userID,
			Registered: false,
		}
		if err := db.Create(newUser).Error; err != nil {
			fmt.Printf("Error creating user: %v\n", err)
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ خطا در ایجاد کاربر. لطفاً دوباره تلاش کنید."))
			return
		}
		// Start registration for new user
		setRegState(userID, "full_name")
		regTemp.Lock()
		regTemp.m[userID] = make(map[string]string)
		regTemp.Unlock()

		welcomeMsg := `🎉 *خوش آمدید به ربات صرافی ارز دیجیتال!*

🔐 برای شروع استفاده از خدمات ما، لطفاً اطلاعات خود را تکمیل کنید.

📝 *مرحله ۱: نام و نام خانوادگی*

لطفاً نام و نام خانوادگی خود را به فارسی وارد کنید:
مثال: علی احمدی

💡 *نکات مهم:*
• نام و نام خانوادگی باید به فارسی باشد
• حداقل دو کلمه (نام و نام خانوادگی) الزامی است
• هر کلمه حداقل ۲ حرف باشد`

		message := tgbotapi.NewMessage(msg.Chat.ID, welcomeMsg)
		message.ParseMode = "Markdown"
		bot.Send(message)
		return
	}

	// User exists, check if registered
	fmt.Printf("User found, registered: %v, full_name: '%s', sheba: '%s', card_number: '%s'\n",
		user.Registered, user.FullName, user.Sheba, user.CardNumber)

	// Check if user has incomplete registration (exists but missing data)
	if !user.Registered || user.FullName == "" || user.Sheba == "" || user.CardNumber == "" {
		fmt.Printf("User has incomplete registration, starting registration process\n")
		// User exists but not registered or has incomplete data, start registration
		setRegState(userID, "full_name")
		regTemp.Lock()
		regTemp.m[userID] = make(map[string]string)
		regTemp.Unlock()

		welcomeBackMsg := `🔄 *تکمیل ثبت‌نام*

👋 سلام! به نظر می‌رسد ثبت‌نام شما ناتمام مانده است.

📝 *مرحله ۱: نام و نام خانوادگی*

لطفاً نام و نام خانوادگی خود را به فارسی وارد کنید:
مثال: علی احمدی

💡 *نکات مهم:*
• نام و نام خانوادگی باید به فارسی باشد
• حداقل دو کلمه (نام و نام خانوادگی) الزامی است
• هر کلمه حداقل ۲ حرف باشد`

		message := tgbotapi.NewMessage(msg.Chat.ID, welcomeBackMsg)
		message.ParseMode = "Markdown"
		bot.Send(message)
		return
	}

	// User is already registered, show their information and main menu
	fmt.Printf("Showing info for registered user: %s\n", user.FullName)
	showUserInfo(bot, msg.Chat.ID, user)
	showMainMenu(bot, msg.Chat.ID)
}

func showUserInfo(bot *tgbotapi.BotAPI, chatID int64, user *models.User) {
	info := fmt.Sprintf(`👤 *اطلاعات کاربر*

📝 *نام و نام خانوادگی:* %s
🆔 *نام کاربری:* @%s
💳 *شماره کارت:* %s
🏦 *شماره شبا:* %s
✅ *وضعیت:* ثبت‌نام شده

🎉 *خوش آمدید!* حالا می‌توانید از تمام خدمات ربات استفاده کنید.`,
		user.FullName, user.Username, user.CardNumber, user.Sheba)

	message := tgbotapi.NewMessage(chatID, info)
	message.ParseMode = "Markdown"
	bot.Send(message)
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
	case "⬅️ بازگشت":
		showMainMenu(bot, msg.Chat.ID)
	default:
		// Check if it's a submenu action
		handleSubmenuActions(bot, db, msg)
	}
}

func handleSubmenuActions(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	switch msg.Text {
	case "💵 برداشت":
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "💵 منوی برداشت:\n\nاین قابلیت به زودی اضافه خواهد شد."))
	case "📋 تاریخچه":
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "📋 تاریخچه تراکنش‌ها:\n\nاین قابلیت به زودی اضافه خواهد شد."))
	case "💳 واریز USDT":
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "💳 منوی واریز USDT:\n\nاین قابلیت به زودی اضافه خواهد شد."))
	case "🔗 لینک رفرال":
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "🔗 لینک رفرال شما:\n\nاین قابلیت به زودی اضافه خواهد شد."))
	case "💰 دریافت پاداش":
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "💰 منوی دریافت پاداش:\n\nاین قابلیت به زودی اضافه خواهد شد."))
	case "📈 آمار شخصی":
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "📈 آمار شخصی شما:\n\nاین قابلیت به زودی اضافه خواهد شد."))
	case "👥 زیرمجموعه‌ها":
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "👥 لیست زیرمجموعه‌ها:\n\nاین قابلیت به زودی اضافه خواهد شد."))
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
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	msg := tgbotapi.NewMessage(chatID, `🏠 *منوی اصلی*

لطفاً یکی از گزینه‌های زیر را انتخاب کنید:

💰 *کیف پول* - مدیریت موجودی و تراکنش‌ها
🎁 *پاداش* - سیستم رفرال و پاداش‌ها
📊 *آمار* - آمار شخصی و زیرمجموعه‌ها
🆘 *پشتیبانی* - ارتباط با پشتیبانی`)
	msg.ReplyMarkup = menu
	msg.ParseMode = "Markdown"
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
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	msg := tgbotapi.NewMessage(chatID, `💰 *منوی کیف پول*

لطفاً یکی از گزینه‌های زیر را انتخاب کنید:

💵 *برداشت* - درخواست برداشت ریالی
📋 *تاریخچه* - مشاهده تراکنش‌های قبلی
💳 *واریز USDT* - واریز ارز دیجیتال
⬅️ *بازگشت* - بازگشت به منوی اصلی`)
	msg.ReplyMarkup = menu
	msg.ParseMode = "Markdown"
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
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	msg := tgbotapi.NewMessage(chatID, `🎁 *منوی پاداش*

لطفاً یکی از گزینه‌های زیر را انتخاب کنید:

🔗 *لینک رفرال* - دریافت لینک معرفی
💰 *دریافت پاداش* - انتقال پاداش به کیف پول
⬅️ *بازگشت* - بازگشت به منوی اصلی`)
	msg.ReplyMarkup = menu
	msg.ParseMode = "Markdown"
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
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	msg := tgbotapi.NewMessage(chatID, `📊 *منوی آمار*

لطفاً یکی از گزینه‌های زیر را انتخاب کنید:

📈 *آمار شخصی* - آمار تراکنش‌ها و موجودی
👥 *زیرمجموعه‌ها* - لیست کاربران معرفی شده
⬅️ *بازگشت* - بازگشت به منوی اصلی`)
	msg.ReplyMarkup = menu
	msg.ParseMode = "Markdown"
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
		if err == gorm.ErrRecordNotFound {
			// User not found, this is expected for new users
			return nil, nil
		}
		// Other database error
		return nil, err
	}
	return &user, nil
}

func registerUser(db *gorm.DB, telegramID int64, fullName, sheba, cardNumber string) error {
	result := db.Model(&models.User{}).
		Where("telegram_id = ?", telegramID).
		Updates(map[string]interface{}{
			"full_name":   fullName,
			"sheba":       sheba,
			"card_number": cardNumber,
			"registered":  true,
		})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("no user found with telegram_id: %d", telegramID)
	}

	return nil
}

func handleFixUser(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	user, err := getUserByTelegramID(db, userID)

	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "کاربر یافت نشد. لطفاً ابتدا /start را بزنید."))
		return
	}

	if user.Registered && user.FullName != "" && user.Sheba != "" && user.CardNumber != "" {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "کاربر شما قبلاً ثبت‌نام شده است."))
		return
	}

	// Start registration process for incomplete user
	setRegState(userID, "full_name")
	regTemp.Lock()
	regTemp.m[userID] = make(map[string]string)
	regTemp.Unlock()
	bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "لطفاً نام و نام خانوادگی خود را وارد کنید:"))
}
