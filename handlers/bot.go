package handlers

import (
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gorm.io/gorm"

	"telegram-exchange-robot/config"
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

// --- Admin Panel ---
const adminUserID int64 = 7403868937

func isAdmin(userID int64) bool {
	return userID == adminUserID
}

func showAdminMenu(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64) {
	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 آمار کلی"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("👥 مشاهده همه کاربران"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📢 پیام همگانی"),
			tgbotapi.NewKeyboardButton("📋 مدیریت برداشت‌ها"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⚙️ تنظیمات محدودیت‌ها"),
			tgbotapi.NewKeyboardButton("💱 مدیریت نرخ‌ها"),
		),
	)
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	helpText := "🛠️ *سلام ادمین عزیز!*\n\n" +
		"به پنل مدیریت خوش اومدی! 😊\n\n" +
		"*دستورات سریع برای مدیریت:*\n\n" +
		"• `/addbalance USER_ID AMOUNT` — افزایش موجودی کاربر\n" +
		"• `/subbalance USER_ID AMOUNT` — کاهش موجودی کاربر\n" +
		"• `/setbalance USER_ID AMOUNT` — تنظیم موجودی کاربر\n" +
		"• `/userinfo USER_ID` — مشاهده اطلاعات کامل کاربر و کیف پول\n" +
		"• `/backup` — دریافت فایل پشتیبان دیتابیس\n\n" +
		"• `/settrade [شماره معامله] [حداقل درصد] [حداکثر درصد]`\n" +
		"  └ تنظیم بازه سود/ضرر برای هر ترید\n\n" +
		"• `/setrate [ارز] [نرخ به تومان]`\n" +
		"  └ تنظیم نرخ به تومان برای ارز مشخص\n\n" +
		"• `/rates`\n" +
		"  └ نمایش نرخ‌های فعلی\n\n" +
		"همه چیز آماده‌ست! از منوی زیر هر کاری که نیاز داری رو انجام بده 👇"

	msg := tgbotapi.NewMessage(chatID, helpText)
	msg.ReplyMarkup = menu
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func handleAdminMenu(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	// Check if admin is in broadcast mode first
	if adminBroadcastState[msg.From.ID] == "awaiting_broadcast" {
		// Check if admin wants to go back to admin panel
		if msg.Text == "⬅️ بازگشت به پنل ادمین" {
			// Clear broadcast state
			adminState[msg.From.ID] = ""
			adminBroadcastState[msg.From.ID] = ""
			adminBroadcastDraft[msg.From.ID] = nil

			// Show admin menu again
			showAdminMenu(bot, db, msg.Chat.ID)
			return
		}
		// Skip menu handling, let the broadcast handler take care of it
		return
	}

	switch msg.Text {
	case "📊 آمار کلی":
		// Show global stats
		var userCount int64
		db.Model(&models.User{}).Count(&userCount)
		var regCount int64
		db.Model(&models.User{}).Where("registered = ?", true).Count(&regCount)
		var totalDeposit, totalWithdraw float64
		db.Model(&models.Transaction{}).Where("type = ? AND status = ?", "deposit", "confirmed").Select("COALESCE(SUM(amount),0)").Scan(&totalDeposit)
		db.Model(&models.Transaction{}).Where("type = ? AND status = ?", "withdraw", "confirmed").Select("COALESCE(SUM(amount),0)").Scan(&totalWithdraw)
		statsMsg := fmt.Sprintf(`📊 <b>آمار کلی ربات</b>

👥 <b>کل کاربران:</b> %d نفر
✅ <b>ثبت‌نام کامل:</b> %d نفر
💰 <b>مجموع واریز:</b> %.2f USDT
💸 <b>مجموع برداشت:</b> %.2f USDT`, userCount, regCount, totalDeposit, totalWithdraw)
		message := tgbotapi.NewMessage(msg.Chat.ID, statsMsg)
		message.ParseMode = "HTML"
		bot.Send(message)
		return
	case "👥 مشاهده همه کاربران":
		adminUsersPage[msg.From.ID] = 0 // Reset to first page
		showUsersPage(bot, db, msg.Chat.ID, msg.From.ID, 0)
		return
	case "📢 پیام همگانی":
		// Set admin state for broadcast
		adminState[msg.From.ID] = "awaiting_broadcast"
		adminBroadcastState[msg.From.ID] = "awaiting_broadcast"

		// Create keyboard with back button
		cancelKeyboard := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("⬅️ بازگشت به پنل ادمین"),
			),
		)
		cancelKeyboard.ResizeKeyboard = true
		cancelKeyboard.OneTimeKeyboard = false

		m := tgbotapi.NewMessage(msg.Chat.ID, "✏️ پیام خود را برای ارسال همگانی بنویسید:")
		m.ReplyMarkup = cancelKeyboard
		bot.Send(m)
		return
	case "📋 مدیریت برداشت‌ها":
		showAllPendingWithdrawals(bot, db, msg.Chat.ID)
		return
	case "⚙️ تنظیمات محدودیت‌ها":
		showLimitsSettings(bot, db, msg.Chat.ID)
		return
	case "💱 مدیریت نرخ‌ها":
		showRatesManagement(bot, db, msg.Chat.ID)
		return
	case "⬅️ بازگشت":
		showMainMenu(bot, db, msg.Chat.ID, msg.From.ID)
		return
	case "⬅️ بازگشت به پنل ادمین":
		showAdminMenu(bot, db, msg.Chat.ID)
		return
	}

	if adminBroadcastState[msg.From.ID] == "confirm_broadcast" {
		// Send broadcast to all users
		var users []models.User
		db.Find(&users)
		draft := adminBroadcastDraft[msg.From.ID]
		for _, u := range users {
			if u.TelegramID == msg.From.ID {
				continue // don't send to self
			}
			if draft.Text != "" {
				broadcastText := "📢 پیام از ادمین:\n\n" + draft.Text
				m := tgbotapi.NewMessage(u.TelegramID, broadcastText)
				bot.Send(m)
			} else if draft.Photo != nil {
				photo := draft.Photo[len(draft.Photo)-1]
				m := tgbotapi.NewPhoto(u.TelegramID, tgbotapi.FileID(photo.FileID))
				m.Caption = "📢 پیام از ادمین:"
				bot.Send(m)
			} else if draft.Video != nil {
				m := tgbotapi.NewVideo(u.TelegramID, tgbotapi.FileID(draft.Video.FileID))
				m.Caption = "📢 پیام از ادمین:"
				bot.Send(m)
			} else if draft.Voice != nil {
				m := tgbotapi.NewVoice(u.TelegramID, tgbotapi.FileID(draft.Voice.FileID))
				m.Caption = "📢 پیام از ادمین:"
				bot.Send(m)
			} else if draft.Document != nil {
				m := tgbotapi.NewDocument(u.TelegramID, tgbotapi.FileID(draft.Document.FileID))
				m.Caption = "📢 پیام از ادمین:"
				bot.Send(m)
			}
		}
		adminBroadcastState[msg.From.ID] = ""
		adminBroadcastDraft[msg.From.ID] = nil
		message := tgbotapi.NewMessage(msg.Chat.ID, "✅ پیام همگانی با موفقیت ارسال شد.")
		bot.Send(message)
		return
	}

	// If none matched, show invalid command
	message := tgbotapi.NewMessage(msg.Chat.ID, "🤔 این دستور رو نمی‌شناسم! \n\nاز منوی زیر استفاده کن یا راهنمای دستورات رو ببین 👇")
	bot.Send(message)
	return
}

// Track admin state for broadcast
var adminState = make(map[int64]string)

var adminBroadcastState = make(map[int64]string) // "awaiting_broadcast", "confirm_broadcast", ""
var adminBroadcastDraft = make(map[int64]*tgbotapi.Message)

// Track admin users list pagination
var adminUsersPage = make(map[int64]int) // userID -> current page number

func logInfo(format string, v ...interface{}) {
	log.Printf("[INFO] "+format, v...)
}

func logError(format string, v ...interface{}) {
	log.Printf("[ERROR] "+format, v...)
}

func logDebug(format string, v ...interface{}) {
	log.Printf("[DEBUG] "+format, v...)
}

func StartBot(bot *tgbotapi.BotAPI, db *gorm.DB, cfg *config.Config) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	logInfo("🔄 Bot update channel started, waiting for messages...")

	for update := range updates {
		// --- هندل دستور ادمین برای /settrade و /setrate و /rates ---
		if update.Message != nil && update.Message.IsCommand() && isAdmin(int64(update.Message.From.ID)) {
			if update.Message.Command() == "settrade" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) == 3 {
					tradeIndex, _ := strconv.Atoi(args[0])
					minPercent, _ := strconv.ParseFloat(args[1], 64)
					maxPercent, _ := strconv.ParseFloat(args[2], 64)
					var tr models.TradeRange
					if tradeIndex < 1 || tradeIndex > 3 {
						bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "شماره معامله باید فقط ۱، ۲ یا ۳ باشد."))
						continue
					}
					if err := db.Where("trade_index = ?", tradeIndex).First(&tr).Error; err == nil {
						tr.MinPercent = minPercent
						tr.MaxPercent = maxPercent
						db.Save(&tr)
					} else {
						tr = models.TradeRange{TradeIndex: tradeIndex, MinPercent: minPercent, MaxPercent: maxPercent}
						db.Create(&tr)
					}
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("رنج معامله %d به %.2f تا %.2f تنظیم شد.", tradeIndex, minPercent, maxPercent)))
				} else {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "فرمت دستور: /settrade [شماره معامله] [حداقل درصد] [حداکثر درصد]"))
				}
				continue
			}
			if update.Message.Command() == "setrate" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) == 2 {
					asset := strings.ToUpper(args[0])
					value, err := strconv.ParseFloat(args[1], 64)
					if err != nil || value <= 0 {
						bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "مقدار نرخ نامعتبر است. فقط عدد مثبت وارد کنید."))
						continue
					}
					var rate models.Rate
					if err := db.Where("asset = ?", asset).First(&rate).Error; err == nil {
						rate.Value = value
						db.Save(&rate)
					} else {
						rate = models.Rate{Asset: asset, Value: value}
						db.Create(&rate)
					}
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("نرخ *%s* به *%s تومان* با موفقیت ثبت شد.\n\nمثال کاربرد: اگر کاربر ۱۰۰ تتر بخواهد، مبلغ معادل: *%s تومان* خواهد بود.", asset, formatToman(value), formatToman(value*100))))
				} else {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😅 *فرمت درستش اینطوریه:* \n`/setrate [ارز] [نرخ به تومان]` \n\n*مثال:* `/setrate USDT 58500`"))
				}
				continue
			}
			if update.Message.Command() == "rates" {
				var rates []models.Rate
				db.Find(&rates)
				if len(rates) == 0 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😔 هنوز هیچ نرخی ثبت نشده! \n\nبرای ثبت نرخ اول از دستور `/setrate` استفاده کن 👆"))
					continue
				}
				rateMsg := "💱 *نرخ‌های فعلی ارزها*\n\n"
				rateMsg += "ارز      نرخ (تومان)\n"
				rateMsg += "--------------------------\n"
				for _, r := range rates {
					rateMsg += fmt.Sprintf("%-8s %s\n", r.Asset, formatToman(r.Value))
				}
				rateMsg += "\n✏️ برای تغییر نرخ هر ارز، از دستور `/setrate [ارز] [نرخ]` استفاده کن."
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, rateMsg)
				msg.ParseMode = "Markdown"
				bot.Send(msg)
				continue
			}
			if update.Message.Command() == "addbalance" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 2 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😅 *فرمت درستش اینطوریه:* \n`/addbalance USER_ID AMOUNT`"))
					continue
				}
				userID, err1 := strconv.ParseInt(args[0], 10, 64)
				amount, err2 := strconv.ParseFloat(args[1], 64)
				if err1 != nil || err2 != nil || amount <= 0 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "🤔 مقدار یا شناسه کاربر درست نیست. یه چک کن!"))
					continue
				}
				user, err := getUserByTelegramID(db, userID)
				if err != nil || user == nil {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😔 این کاربر رو پیدا نکردم!"))
					continue
				}
				user.ERC20Balance += amount
				db.Save(user)
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("✅ *انجام شد!* \n\n🎉 موجودی ERC20 کاربر *%s* (آیدی: `%d`) به میزان *%s* تتر افزایش یافت.", user.FullName, user.TelegramID, formatToman(amount))))
				continue
			}
			if update.Message.Command() == "subbalance" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 2 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😅 *فرمت درستش اینطوریه:* \n`/subbalance USER_ID AMOUNT`"))
					continue
				}
				userID, err1 := strconv.ParseInt(args[0], 10, 64)
				amount, err2 := strconv.ParseFloat(args[1], 64)
				if err1 != nil || err2 != nil || amount <= 0 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "🤔 مقدار یا شناسه کاربر درست نیست. یه چک کن!"))
					continue
				}
				user, err := getUserByTelegramID(db, userID)
				if err != nil || user == nil {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😔 این کاربر رو پیدا نکردم!"))
					continue
				}
				if user.ERC20Balance < amount {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😬  موجودی کافی نیست."))
					continue
				}
				user.ERC20Balance -= amount
				db.Save(user)
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("✅ *انجام شد!* \n\n📉 موجودی ERC20 کاربر *%s* (آیدی: `%d`) به میزان *%s* تتر کاهش یافت.", user.FullName, user.TelegramID, formatToman(amount))))
				continue
			}
			if update.Message.Command() == "setbalance" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 2 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😅 *فرمت درستش اینطوریه:* \n`/setbalance USER_ID AMOUNT`"))
					continue
				}
				userID, err1 := strconv.ParseInt(args[0], 10, 64)
				amount, err2 := strconv.ParseFloat(args[1], 64)
				if err1 != nil || err2 != nil || amount < 0 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "🤔 مقدار یا شناسه کاربر درست نیست. یه چک کن!"))
					continue
				}
				user, err := getUserByTelegramID(db, userID)
				if err != nil || user == nil {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😔 این کاربر رو پیدا نکردم!"))
					continue
				}
				user.ERC20Balance = amount
				db.Save(user)
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("✅ *تمام!* \n\n🎯 موجودی ERC20 کاربر *%s* (آیدی: `%d`) روی *%s* تتر تنظیم شد.", user.FullName, user.TelegramID, formatToman(amount))))
				continue
			}
			if update.Message.Command() == "userinfo" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 1 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😅 *فرمت درستش اینطوریه:* \n`/userinfo USER_ID`"))
					continue
				}
				userID, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "🤔 شناسه کاربر درست نیست. یه چک کن!"))
					continue
				}
				user, err := getUserByTelegramID(db, userID)
				if err != nil || user == nil {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😔 این کاربر رو پیدا نکردم!"))
					continue
				}
				msg := fmt.Sprintf(`👤 *اطلاعات کامل کاربر*

نام: %s
یوزرنیم: @%s
آیدی عددی: %d
ثبت‌نام: %v

💳 *کیف پول*
ERC20: %s
Mnemonic: %s

BEP20: %s
Mnemonic: %s

موجودی ERC20: %s تتر
موجودی BEP20: %s تتر
سود/ضرر ترید: %s تتر
پاداش: %s تتر

👥 رفرر: %v

*برای مدیریت بیشتر، از بخش مدیریت کاربران استفاده کن.*`,
					user.FullName, user.Username, user.TelegramID, user.Registered,
					user.ERC20Address, user.ERC20Mnemonic,
					user.BEP20Address, user.BEP20Mnemonic,
					formatToman(user.ERC20Balance), formatToman(user.BEP20Balance), formatToman(user.TradeBalance), formatToman(user.RewardBalance), user.ReferrerID)
				m := tgbotapi.NewMessage(update.Message.Chat.ID, msg)
				m.ParseMode = "Markdown"
				bot.Send(m)
				continue
			}
			if update.Message.Command() == "setmindeposit" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 1 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😅 *فرمت درستش اینطوریه:* \n`/setmindeposit AMOUNT`"))
					continue
				}
				amount, err := strconv.ParseFloat(args[0], 64)
				if err != nil || amount <= 0 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "🤔 مقدار درست نیست. فقط عدد مثبت وارد کن!"))
					continue
				}
				if err := setSetting(db, models.SETTING_MIN_DEPOSIT_USDT, args[0], "حداقل مبلغ واریز (USDT)"); err != nil {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😞 متاسفانه مشکلی پیش اومد!"))
					continue
				}
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("✅ *انجام شد!* \n\n🎯 حداقل واریز به *%.0f USDT* تنظیم شد.", amount)))
				continue
			}
			if update.Message.Command() == "setminwithdraw" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 1 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😅 *فرمت درستش اینطوریه:* \n`/setminwithdraw AMOUNT`"))
					continue
				}
				amount, err := strconv.ParseFloat(args[0], 64)
				if err != nil || amount <= 0 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "🤔 مقدار درست نیست. فقط عدد مثبت وارد کن!"))
					continue
				}
				if err := setSetting(db, models.SETTING_MIN_WITHDRAW_TOMAN, args[0], "حداقل مبلغ برداشت (تومان)"); err != nil {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😞 متاسفانه مشکلی پیش اومد!"))
					continue
				}
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("✅ *انجام شد!* \n\n🎯 حداقل برداشت به *%s تومان* تنظیم شد.", formatToman(amount))))
				continue
			}
			if update.Message.Command() == "setmaxwithdraw" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 1 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😅 *فرمت درستش اینطوریه:* \n`/setmaxwithdraw AMOUNT`"))
					continue
				}
				amount, err := strconv.ParseFloat(args[0], 64)
				if err != nil || amount <= 0 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "🤔 مقدار درست نیست. فقط عدد مثبت وارد کن!"))
					continue
				}
				if err := setSetting(db, models.SETTING_MAX_WITHDRAW_TOMAN, args[0], "حداکثر مبلغ برداشت (تومان)"); err != nil {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😞 متاسفانه مشکلی پیش اومد!"))
					continue
				}
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("✅ *انجام شد!* \n\n🎯 حداکثر برداشت به *%s تومان* تنظیم شد.", formatToman(amount))))
				continue
			}
			if update.Message.Command() == "backup" {
				// اجرای بکاپ دیتابیس و ارسال فایل به ادمین
				go func(chatID int64) {
					bot.Send(tgbotapi.NewMessage(chatID, "⏳ صبر کن، دارم فایل بکاپ رو آماده می‌کنم..."))
					user := cfg.MySQL.User
					pass := cfg.MySQL.Password
					dbName := cfg.MySQL.DBName
					backupFile := fmt.Sprintf("backup_%d.sql", time.Now().Unix())
					cmd := exec.Command("mysqldump", "-u"+user, "-p"+pass, dbName, "--result-file="+backupFile)
					err := cmd.Run()
					if err != nil {
						bot.Send(tgbotapi.NewMessage(chatID, "😞 متاسفانه مشکلی پیش اومد: "+err.Error()))
						return
					}
					file := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(backupFile))
					file.Caption = "📦 فایل بکاپ آماده!"
					bot.Send(file)
					// پاک کردن فایل بعد از ارسال (اختیاری)
					_ = os.Remove(backupFile)
				}(update.Message.Chat.ID)
				continue
			}
		}
		// Handle CallbackQuery first!
		if update.CallbackQuery != nil {
			userID := int64(update.CallbackQuery.From.ID)
			// --- In CallbackQuery handler for request_trade_ ---
			if strings.HasPrefix(update.CallbackQuery.Data, "request_trade_") {
				txIDstr := strings.TrimPrefix(update.CallbackQuery.Data, "request_trade_")
				txID, _ := strconv.Atoi(txIDstr)
				var tx models.Transaction
				if err := db.First(&tx, txID).Error; err == nil && tx.TradeCount < 3 {
					tradeIndex := tx.TradeCount + 1
					// خواندن رنج درصد از تنظیمات ادمین
					var tr models.TradeRange
					if err := db.Where("trade_index = ?", tradeIndex).First(&tr).Error; err != nil {
						bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "رنج درصد برای این معامله تنظیم نشده است!"))
						continue
					}
					// تولید درصد رندوم در بازه
					percent := tr.MinPercent + rand.Float64()*(tr.MaxPercent-tr.MinPercent)
					// محاسبه مبلغ جدید
					var lastAmount float64 = tx.Amount
					var lastTrade models.TradeResult
					db.Where("transaction_id = ? AND user_id = ?", tx.ID, tx.UserID).Order("trade_index desc").First(&lastTrade)
					if lastTrade.ID != 0 {
						lastAmount = lastTrade.ResultAmount
					}
					resultAmount := lastAmount * (1 + percent/100)

					// به‌روزرسانی سود/ضرر ترید در TradeBalance
					var user models.User
					if err := db.First(&user, tx.UserID).Error; err == nil {
						profit := resultAmount - lastAmount
						user.TradeBalance += profit

						// اگر ضرر بود، از موجودی بلاکچین کم کن و به هیچ وجه زیر صفر نیاور
						if profit < 0 {
							loss := -profit
							var deducted float64
							var network, walletAddr string
							if tx.Network == "ERC20" {
								deducted = min(loss, user.ERC20Balance)
								user.ERC20Balance -= deducted
								if user.ERC20Balance < 0 {
									user.ERC20Balance = 0
								}
								network = "ERC20"
								walletAddr = user.ERC20Address
							} else if tx.Network == "BEP20" {
								deducted = min(loss, user.BEP20Balance)
								user.BEP20Balance -= deducted
								if user.BEP20Balance < 0 {
									user.BEP20Balance = 0
								}
								network = "BEP20"
								walletAddr = user.BEP20Address
							}
							// پیام به ادمین
							adminMsg := fmt.Sprintf("⚠️ کاربر %s (ID: %d) در معامله %s به مقدار %.2f USDT ضرر کرد.\nلطفاً %.2f USDT را از ولت %s کاربر (%s) کسر و به ولت صرافی منتقل کن.", user.FullName, user.TelegramID, network, loss, deducted, network, walletAddr)
							bot.Send(tgbotapi.NewMessage(adminUserID, adminMsg))
						} else if profit > 0 {
							// پیام سود به ادمین
							var network, walletAddr string
							if tx.Network == "ERC20" {
								network = "ERC20"
								walletAddr = user.ERC20Address
							} else if tx.Network == "BEP20" {
								network = "BEP20"
								walletAddr = user.BEP20Address
							}
							adminMsg := fmt.Sprintf("ℹ️ کاربر %s (ID: %d) در معامله %s %.2f USDT سود کرد.\nآدرس ولت کاربر: %s", user.FullName, user.TelegramID, network, profit, walletAddr)
							bot.Send(tgbotapi.NewMessage(adminUserID, adminMsg))
						}
						db.Save(&user)
					}

					// به‌روزرسانی مبلغ تراکنش
					tx.Amount = resultAmount
					tx.TradeCount++
					db.Save(&tx)

					// ذخیره نتیجه ترید
					tradeResult := models.TradeResult{
						TransactionID: tx.ID,
						UserID:        tx.UserID,
						TradeIndex:    tradeIndex,
						Percent:       percent,
						ResultAmount:  resultAmount,
						CreatedAt:     time.Now(),
					}
					db.Create(&tradeResult)

					// بعد از ذخیره نتیجه ترید (tradeResult) و قبل از ارسال پیام نتیجه به کاربر:
					// --- Referral reward logic ---
					tradeAmount := lastAmount
					userPtr, _ := getUserByTelegramID(db, int64(tx.UserID))
					if userPtr != nil {
						user := userPtr
						if user != nil && user.ReferrerID != nil {
							var referrer1 models.User
							if err := db.First(&referrer1, *user.ReferrerID).Error; err == nil {
								// پلن ویژه: اگر 20 زیرمجموعه مستقیم دارد
								var count int64
								db.Model(&models.User{}).Where("referrer_id = ? AND registered = ?", referrer1.ID, true).Count(&count)
								percent := 0.5
								if count >= 20 {
									percent = 0.6
									if !referrer1.PlanUpgradedNotified {
										bot.Send(tgbotapi.NewMessage(referrer1.TelegramID, "🏆 تبریک! شما به خاطر داشتن ۲۰ زیرمجموعه فعال، درصد پاداش Level 1 شما به ۰.۶٪ افزایش یافت."))
										referrer1.PlanUpgradedNotified = true
									}
								}
								reward1 := tradeAmount * percent / 100
								referrer1.RewardBalance += reward1
								db.Save(&referrer1)
								bot.Send(tgbotapi.NewMessage(referrer1.TelegramID, fmt.Sprintf("🎉 شما به خاطر معامله زیرمجموعه‌تان %s مبلغ %.4f USDT پاداش گرفتید!", user.FullName, reward1)))
							}
							// Level 2
							if referrer1.ReferrerID != nil {
								var referrer2 models.User
								if err := db.First(&referrer2, *referrer1.ReferrerID).Error; err == nil {
									reward2 := tradeAmount * 0.25 / 100
									referrer2.RewardBalance += reward2
									db.Save(&referrer2)
									bot.Send(tgbotapi.NewMessage(referrer2.TelegramID, fmt.Sprintf("🎉 شما به خاطر معامله زیرمجموعه غیرمستقیم %s مبلغ %.4f USDT پاداش گرفتید!", user.FullName, reward2)))
								}
							}
						}
					}
					// پیام به کاربر: بعد از ۱ ثانیه نتیجه را ارسال کن
					go func(chatID int64, amount float64, percent float64, resultAmount float64, tradeIndex int) {
						time.Sleep(30 * time.Minute)
						msg := fmt.Sprintf("نتیجه معامله %d شما: %+.2f%%\nمبلغ جدید: %.2f USDT", tradeIndex, percent, resultAmount)
						bot.Send(tgbotapi.NewMessage(chatID, msg))
					}(update.CallbackQuery.From.ID, lastAmount, percent, resultAmount, tradeIndex)
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, fmt.Sprintf("درخواست معامله %d ثبت شد. نتیجه تا ۳۰ دقیقه دیگر اعلام می‌شود.", tradeIndex)))
				} else {
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "امکان ترید بیشتر وجود ندارد"))
				}
				continue
			}
			// --- Add a command for user to see trade results for a deposit ---
			if update.Message != nil && strings.HasPrefix(update.Message.Text, "/trades ") {
				txIDstr := strings.TrimPrefix(update.Message.Text, "/trades ")
				txID, _ := strconv.Atoi(txIDstr)
				var trades []models.TradeResult
				db.Where("transaction_id = ? AND user_id = ?", txID, update.Message.From.ID).Order("trade_index").Find(&trades)
				if len(trades) == 0 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "برای این واریز هیچ معامله‌ای انجام نشده است."))
				} else {
					msg := "نتایج معاملات این واریز:\n"
					for _, t := range trades {
						msg += fmt.Sprintf("معامله %d: %+.2f%% → %.2f USDT\n", t.TradeIndex, t.Percent, t.ResultAmount)
					}
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, msg))
				}
				continue
			}
			if isAdmin(userID) {
				data := update.CallbackQuery.Data

				// Handle users pagination callbacks
				if strings.HasPrefix(data, "users_page_") {
					pageStr := strings.TrimPrefix(data, "users_page_")
					page, err := strconv.Atoi(pageStr)
					if err == nil {
						// Edit the existing message with new page
						showUsersPageEdit(bot, db, update.CallbackQuery.Message.Chat.ID, userID, page, update.CallbackQuery.Message.MessageID)
						bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, fmt.Sprintf("صفحه %d", page+1)))
						continue
					}
				}
				if data == "users_close" {
					// Delete the users list message
					deleteMsg := tgbotapi.NewDeleteMessage(update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID)
					bot.Request(deleteMsg)
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "لیست بسته شد"))
					continue
				}
				if data == "users_current_page" {
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "شما در این صفحه هستید"))
					continue
				}

				state := adminBroadcastState[userID]
				if strings.HasPrefix(data, "approve_withdraw_") {
					txIDstr := strings.TrimPrefix(data, "approve_withdraw_")
					txID, _ := strconv.Atoi(txIDstr)
					var tx models.Transaction
					if err := db.First(&tx, txID).Error; err == nil && tx.Status == "pending" {
						var user models.User
						db.First(&user, tx.UserID)
						amount := tx.Amount
						remaining := amount

						// 1. کم کردن از پاداش
						if user.RewardBalance >= remaining {
							user.RewardBalance -= remaining
							remaining = 0
						} else {
							remaining -= user.RewardBalance
							user.RewardBalance = 0
						}

						// 2. کم کردن از سود/ضرر ترید
						if remaining > 0 {
							if user.TradeBalance >= remaining {
								user.TradeBalance -= remaining
								remaining = 0
							} else {
								remaining -= user.TradeBalance
								user.TradeBalance = 0
							}
						}

						// 3. کم کردن از موجودی بلاکچین (اول ERC20 بعد BEP20)
						if remaining > 0 {
							if user.ERC20Balance >= remaining {
								user.ERC20Balance -= remaining
								remaining = 0
							} else {
								remaining -= user.ERC20Balance
								user.ERC20Balance = 0
							}
						}
						if remaining > 0 {
							if user.BEP20Balance >= remaining {
								user.BEP20Balance -= remaining
								remaining = 0
							} else {
								remaining -= user.BEP20Balance
								user.BEP20Balance = 0
							}
						}

						if remaining > 0 {
							// موجودی کافی نیست
							bot.Send(tgbotapi.NewMessage(user.TelegramID, "❌ موجودی کافی برای برداشت وجود ندارد."))
							bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "موجودی کافی نیست"))
							continue
						}

						db.Save(&user)
						tx.Status = "confirmed"
						db.Save(&tx)
						bot.Send(tgbotapi.NewMessage(user.TelegramID, "✅ برداشت شما تایید و پرداخت شد."))
						bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "پرداخت شد"))
					}
					continue
				}
				if strings.HasPrefix(data, "reject_withdraw_") {
					txIDstr := strings.TrimPrefix(data, "reject_withdraw_")
					txID, _ := strconv.Atoi(txIDstr)
					var tx models.Transaction
					if err := db.First(&tx, txID).Error; err == nil && tx.Status == "pending" {
						tx.Status = "canceled"
						db.Save(&tx)
						var user models.User
						db.First(&user, tx.UserID)
						if tx.Type == "reward_withdraw" {
							user.ReferralReward += tx.Amount
							db.Save(&user)
						}
						bot.Send(tgbotapi.NewMessage(user.TelegramID, fmt.Sprintf("❌ برداشت شما به مبلغ %.2f USDT لغو شد و مبلغ به حساب شما برگشت.", tx.Amount)))
						bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "رد شد"))
					}
					continue
				}
				if state == "confirm_broadcast" {
					data := update.CallbackQuery.Data
					if data == "broadcast_send" {
						var users []models.User
						db.Find(&users)
						draft := adminBroadcastDraft[userID]
						caption := ""
						if draft.Caption != "" {
							caption = "📢 پیام از ادمین:\n\n" + draft.Caption
						}
						for _, u := range users {
							if u.TelegramID == userID {
								continue
							}
							if draft.Text != "" && draft.Photo == nil && draft.Video == nil && draft.Voice == nil && draft.Document == nil {
								m := tgbotapi.NewMessage(u.TelegramID, "📢 پیام از ادمین:\n\n"+draft.Text)
								bot.Send(m)
							} else if draft.Photo != nil {
								photo := draft.Photo[len(draft.Photo)-1]
								m := tgbotapi.NewPhoto(u.TelegramID, tgbotapi.FileID(photo.FileID))
								m.Caption = caption
								bot.Send(m)
							} else if draft.Video != nil {
								m := tgbotapi.NewVideo(u.TelegramID, tgbotapi.FileID(draft.Video.FileID))
								m.Caption = caption
								bot.Send(m)
							} else if draft.Voice != nil {
								m := tgbotapi.NewVoice(u.TelegramID, tgbotapi.FileID(draft.Voice.FileID))
								m.Caption = caption
								bot.Send(m)
							} else if draft.Document != nil {
								m := tgbotapi.NewDocument(u.TelegramID, tgbotapi.FileID(draft.Document.FileID))
								m.Caption = caption
								bot.Send(m)
							}
						}
						adminBroadcastState[userID] = ""
						adminBroadcastDraft[userID] = nil
						msg := tgbotapi.NewMessage(userID, "✅ پیام همگانی با موفقیت ارسال شد.")
						bot.Send(msg)
						showAdminMenu(bot, db, userID)
						bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "پیام ارسال شد"))
						continue
					} else if data == "broadcast_cancel" {
						adminBroadcastState[userID] = ""
						adminBroadcastDraft[userID] = nil
						msg := tgbotapi.NewMessage(userID, "❌ ارسال پیام همگانی لغو شد.")
						bot.Send(msg)
						showAdminMenu(bot, db, userID)
						bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "لغو شد"))
						continue
					}
				}
			}
		}
		// Now check for Message
		if update.Message == nil {
			continue
		}

		// Registration flow state machine - check first
		if handleRegistration(bot, db, update.Message) {
			continue // User is in registration flow, skip other handlers
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

		// Before showing main menu, check if user is fully registered
		userID := int64(update.Message.From.ID)
		user, err := getUserByTelegramID(db, userID)

		if err != nil || user == nil {
			// User doesn't exist, redirect to registration
			logInfo("User %d not found, redirecting to registration", userID)
			handleStart(bot, db, update.Message)
			continue
		}

		// Check if user is fully registered
		if !user.Registered || user.FullName == "" || user.Sheba == "" || user.CardNumber == "" {
			// User is not fully registered, redirect to registration
			logInfo("User %d not fully registered, redirecting to registration", userID)

			// Send a message explaining why they can't access menus
			redirectMsg := `😊 *یه قدم مونده تا آماده بشی!*

🚀 برای استفاده از همه امکانات فوق‌العاده ربات، فقط باید ثبت‌نامت رو تکمیل کنی.

✨ *چیزای ساده که باقی مونده:*
1️⃣ نام و نام خانوادگی
2️⃣ شماره شبا
3️⃣ شماره کارت

🎯 الان میبرمت به بخش ثبت‌نام...`

			message := tgbotapi.NewMessage(update.Message.Chat.ID, redirectMsg)
			message.ParseMode = "Markdown"
			bot.Send(message)

			handleStart(bot, db, update.Message)
			continue
		}

		// User is fully registered, show main menu
		handleMainMenu(bot, db, update.Message)

		// Handle admin broadcast states
		state := adminBroadcastState[userID]
		if state == "awaiting_broadcast" && update.Message != nil {
			// Ignore the menu button itself as broadcast content
			if update.Message.Text == "📢 پیام همگانی" {
				continue
			}
			adminBroadcastDraft[userID] = update.Message
			var previewMsg tgbotapi.Chattable
			caption := ""
			if update.Message.Caption != "" {
				caption = "📢 پیام از ادمین:\n\n" + update.Message.Caption
			}
			if update.Message.Text != "" && update.Message.Photo == nil && update.Message.Video == nil && update.Message.Voice == nil && update.Message.Document == nil {
				preview := "📢 پیام از ادمین:\n\n" + update.Message.Text
				msg := tgbotapi.NewMessage(userID, preview)
				msg.ReplyMarkup = confirmBroadcastKeyboard()
				previewMsg = msg
			} else if update.Message.Photo != nil {
				photo := update.Message.Photo[len(update.Message.Photo)-1]
				msg := tgbotapi.NewPhoto(userID, tgbotapi.FileID(photo.FileID))
				msg.Caption = caption
				msg.ReplyMarkup = confirmBroadcastKeyboard()
				previewMsg = msg
			} else if update.Message.Video != nil {
				msg := tgbotapi.NewVideo(userID, tgbotapi.FileID(update.Message.Video.FileID))
				msg.Caption = caption
				msg.ReplyMarkup = confirmBroadcastKeyboard()
				previewMsg = msg
			} else if update.Message.Voice != nil {
				msg := tgbotapi.NewVoice(userID, tgbotapi.FileID(update.Message.Voice.FileID))
				msg.Caption = caption
				msg.ReplyMarkup = confirmBroadcastKeyboard()
				previewMsg = msg
			} else if update.Message.Document != nil {
				msg := tgbotapi.NewDocument(userID, tgbotapi.FileID(update.Message.Document.FileID))
				msg.Caption = caption
				msg.ReplyMarkup = confirmBroadcastKeyboard()
				previewMsg = msg
			} else {
				msg := tgbotapi.NewMessage(userID, "❗️ فقط متن، عکس، ویدیو، ویس یا فایل قابل ارسال است.")
				msg.ReplyMarkup = confirmBroadcastKeyboard()
				previewMsg = msg
			}
			bot.Send(previewMsg)
			adminBroadcastState[userID] = "confirm_broadcast"
			continue
		}
		if state == "confirm_broadcast" && update.CallbackQuery != nil {
			data := update.CallbackQuery.Data
			if data == "broadcast_send" {
				var users []models.User
				db.Find(&users)
				draft := adminBroadcastDraft[userID]
				caption := ""
				if draft.Caption != "" {
					caption = "📢 پیام از ادمین:\n\n" + draft.Caption
				}
				for _, u := range users {
					if u.TelegramID == userID {
						continue
					}
					if draft.Text != "" && draft.Photo == nil && draft.Video == nil && draft.Voice == nil && draft.Document == nil {
						m := tgbotapi.NewMessage(u.TelegramID, "📢 پیام از ادمین:\n\n"+draft.Text)
						bot.Send(m)
					} else if draft.Photo != nil {
						photo := draft.Photo[len(draft.Photo)-1]
						m := tgbotapi.NewPhoto(u.TelegramID, tgbotapi.FileID(photo.FileID))
						m.Caption = caption
						bot.Send(m)
					} else if draft.Video != nil {
						m := tgbotapi.NewVideo(u.TelegramID, tgbotapi.FileID(draft.Video.FileID))
						m.Caption = caption
						bot.Send(m)
					} else if draft.Voice != nil {
						m := tgbotapi.NewVoice(u.TelegramID, tgbotapi.FileID(draft.Voice.FileID))
						m.Caption = caption
						bot.Send(m)
					} else if draft.Document != nil {
						m := tgbotapi.NewDocument(u.TelegramID, tgbotapi.FileID(draft.Document.FileID))
						m.Caption = caption
						bot.Send(m)
					}
				}
				adminBroadcastState[userID] = ""
				adminBroadcastDraft[userID] = nil
				msg := tgbotapi.NewMessage(userID, "✅ پیام همگانی با موفقیت ارسال شد.")
				bot.Send(msg)
				showAdminMenu(bot, db, userID)
				continue
			} else if data == "broadcast_cancel" {
				adminBroadcastState[userID] = ""
				adminBroadcastDraft[userID] = nil
				msg := tgbotapi.NewMessage(userID, "❌ ارسال پیام همگانی لغو شد.")
				bot.Send(msg)
				showAdminMenu(bot, db, userID)
				continue
			}
		}
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

	logInfo("Registration state for user %d: %s", userID, state)

	if state == "full_name" {
		// Validate Persian full name format
		if !models.ValidatePersianFullName(msg.Text) {
			errorMsg := `😅 <b> یه مشکل کوچیک داریم</b>

نام رو کمی متفاوت وارد کن !

📝 <b>مثال درست:</b> علی احمدی

💡 <b>نکته‌های مهم:</b>
• نام و نام خانوادگی به فارسی باشه 
• حداقل دو تا کلمه بنویس (نام و فامیل)
• هر کلمه حداقل ۲ حرف داشته باشه
• این نام باید با نام روی کارت بانکیت یکی باشه

🔄 حالا دوباره امتحان کن! مطمئنم این بار درست میشه 😊`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "HTML"
			bot.Send(message)
			return true
		}
		// Save full name, ask for Sheba
		logInfo("Saving full name: %s for user %d", msg.Text, userID)
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
• بدون فاصله یا کاراکتر اضافی
• بعداً شماره کارت همین حساب رو وارد کن`

		message := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(shebaMsg, msg.Text))
		message.ParseMode = "Markdown"
		bot.Send(message)
		return true
	} else if state == "sheba" {
		// Validate Sheba format
		logInfo("Validating sheba: '%s'", msg.Text)
		if !models.ValidateSheba(msg.Text) {
			logError("Sheba validation failed for: '%s'", msg.Text)

			errorMsg := `😊 <b>شماره شبا کمی اشتباه شده!</b>

نگران نباش، همه جا پیش میاد!

🏦 <b>مثال درست:</b> IR520630144905901219088011

💡 <b>نکته‌های مهم:</b>
• حتماً با IR شروع کن
• بعدش ۲۴ تا رقم بذار
• هیچ فاصله یا خط تیره نذار

🔄 یه بار دیگه امتحان کن! 😉`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "HTML"
			bot.Send(message)
			return true
		}
		// Save Sheba, ask for card number
		logInfo("Saving sheba: %s for user %d", msg.Text, userID)
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
• فقط اعداد مجاز هستند
• حتماً شماره کارت همون حسابی که شباش رو دادی`

		message := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(cardMsg, msg.Text))
		message.ParseMode = "Markdown"
		bot.Send(message)
		return true
	} else if state == "card_number" {
		// Validate card number format
		if !models.ValidateCardNumber(msg.Text) {
			errorMsg := `💳 <b>شماره کارت کمی اشتباهه!</b>

بیا دوباره درستش کنیم!

💳 <b>مثال درست:</b> 6037998215325563

💡 <b>نکته‌های مهم:</b>
• حتماً ۱۶ تا رقم باشه
• هیچ فاصله یا خط تیره نذار
• فقط عدد بنویس

🔄 الان دوباره تست کن! 🙂`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "HTML"
			bot.Send(message)
			return true
		}
		// Save card number, complete registration
		logInfo("Saving card number: %s for user %d", msg.Text, userID)
		saveRegTemp(userID, "card_number", msg.Text)
		regTemp.RLock()
		info := regTemp.m[userID]
		regTemp.RUnlock()

		logInfo("Completing registration for user %d with info: %+v", userID, info)

		err := registerUser(db, userID, info["full_name"], info["sheba"], info["card_number"])
		if err != nil {
			logError("Error registering user: %v", err)
			errorMsg := `😔 <b> یه مشکل فنی پیش اومد</b>

نگران نباش، گاهی اینطوری میشه! لطفاً:
• یه بار دیگه امتحان کن
• اگه بازم نشد، با پشتیبانی چت کن

به زودی حلش می‌کنیم! 💪`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "Markdown"
			bot.Send(message)
			return true
		}

		// --- Referral reward logic ---
		user, _ := getUserByTelegramID(db, userID)
		if user != nil && user.ReferrerID != nil {
			var inviter models.User
			if err := db.First(&inviter, *user.ReferrerID).Error; err == nil {
				// Notify inviter about registration completion (no reward)
				joinedUser := user.Username
				var notifyMsg string
				if joinedUser != "" {
					notifyMsg = fmt.Sprintf("🎉 زیرمجموعه شما ثبت‌نام خود را تکمیل کرد!\n👤 نام کاربری: @%s", joinedUser)
				} else {
					notifyMsg = fmt.Sprintf("🎉 زیرمجموعه شما ثبت‌نام خود را تکمیل کرد!\n👤 آیدی عددی: %d", user.TelegramID)
				}
				bot.Send(tgbotapi.NewMessage(inviter.TelegramID, notifyMsg))
			}
		}

		logInfo("Registration completed successfully for user %d", userID)
		clearRegState(userID)

		successMsg := fmt.Sprintf(`🎉 <b>ثبت‌نام با موفقیت انجام شد!</b>

👤 نام: %s
🏦 شبا: %s
💳 کارت: %s

✅ <b>نکته:</b> اطلاعات شما ثبت شد - شبا و کارت از یک حساب واحد

🚀 حالا می‌تونی از همه امکانات ربات استفاده کنی!`, info["full_name"], info["sheba"], info["card_number"])

		message := tgbotapi.NewMessage(msg.Chat.ID, successMsg)
		message.ParseMode = "Markdown"
		bot.Send(message)

		showMainMenu(bot, db, msg.Chat.ID, userID)
		return true
	}
	if state == "withdraw_amount" {
		if msg.Text == "لغو برداشت" {
			clearRegState(userID)
			showWalletMenu(bot, db, msg.Chat.ID, userID)
			return true
		}

		// Parse Iranian amount (مبلغ تومانی)
		tomanAmount, err := strconv.ParseFloat(msg.Text, 64)
		if err != nil || tomanAmount <= 0 {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😅  مبلغ رو درست وارد نکردی. \n\nفقط عدد بنویس، مثل: 1000000"))
			return true
		}

		// Get current USDT rate
		usdtRate, err := getUSDTRate(db)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 متاسفانه نرخ تتر هنوز تنظیم نشده! \n\nلطفاً با پشتیبانی چت کن تا حلش کنیم 💪"))
			clearRegState(userID)
			return true
		}

		// چک کردن محدودیت‌های حداقل و حداکثر برداشت
		minWithdraw := getMinWithdrawToman(db)
		maxWithdraw := getMaxWithdrawToman(db)

		if tomanAmount < minWithdraw {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("😔 مبلغ کمتر از حداقل مجاز! \n\n📊 حداقل برداشت: %s تومان \n💡 لطفاً حداقل %s تومان درخواست بده", formatToman(minWithdraw), formatToman(minWithdraw))))
			return true
		}

		if tomanAmount > maxWithdraw {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("😅 مبلغ بیشتر از حداکثر مجاز! \n\n📊 حداکثر برداشت: %s تومان \n💡 لطفاً حداکثر %s تومان درخواست بده", formatToman(maxWithdraw), formatToman(maxWithdraw))))
			return true
		}

		// Convert toman to USDT
		usdtAmount := tomanAmount / usdtRate

		user, _ := getUserByTelegramID(db, userID)
		if user == nil {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ کاربر یافت نشد."))
			clearRegState(userID)
			return true
		}

		// Calculate total USDT balance (including all sources)
		totalUSDTBalance := user.ERC20Balance + user.BEP20Balance + user.TradeBalance + user.RewardBalance

		if totalUSDTBalance < usdtAmount {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(`😔 <b>موجودی کمه !</b>

💰 <b>موجودی فعلی:</b> %.4f USDT (معادل %s تومان)
💸 <b>مقدار درخواستی:</b> %.4f USDT (معادل %s تومان)
📉 <b>کسری:</b> %.4f USDT (معادل %s تومان)

😊 یه مقدار کمتر انتخاب کن، یا اول موجودی رو شارژ کن!`,
				totalUSDTBalance, formatToman(totalUSDTBalance*usdtRate),
				usdtAmount, formatToman(tomanAmount),
				usdtAmount-totalUSDTBalance, formatToman((usdtAmount-totalUSDTBalance)*usdtRate))))
			return true
		}

		// Create pending transaction (store as USDT for internal consistency)
		tx := models.Transaction{
			UserID: user.ID,
			Type:   "withdraw",
			Amount: usdtAmount, // Store in USDT
			Status: "pending",
		}
		db.Create(&tx)

		// Notify admin with both Toman and USDT amounts
		adminMsg := fmt.Sprintf(`💸 <b>درخواست برداشت تومانی جدید</b>

👤 <b>کاربر:</b> %s (آیدی: <code>%d</code>)
💵 <b>مبلغ تومانی:</b> <b>%s تومان</b>
💰 <b>معادل USDT:</b> <b>%.4f USDT</b>
📊 <b>نرخ:</b> %s تومان

📋 <b>موجودی کاربر:</b>
• 🔵 ERC20: %.4f USDT
• 🟡 BEP20: %.4f USDT  
• 📈 ترید: %.4f USDT
• 🎁 پاداش: %.4f USDT
• 💎 مجموع: %.4f USDT

برای پرداخت <b>%s تومان</b> به کاربر، یکی از دکمه‌های زیر را انتخاب کنید.`,
			user.FullName, user.TelegramID,
			formatToman(tomanAmount), usdtAmount, formatToman(usdtRate),
			user.ERC20Balance, user.BEP20Balance, user.TradeBalance, user.RewardBalance, totalUSDTBalance,
			formatToman(tomanAmount))

		adminBtns := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("💰 پرداخت شد", fmt.Sprintf("approve_withdraw_%d", tx.ID)),
				tgbotapi.NewInlineKeyboardButtonData("❌ رد شد", fmt.Sprintf("reject_withdraw_%d", tx.ID)),
			),
		)
		msgToAdmin := tgbotapi.NewMessage(adminUserID, adminMsg)
		msgToAdmin.ParseMode = "HTML"
		msgToAdmin.ReplyMarkup = adminBtns
		bot.Send(msgToAdmin)

		// Confirm to user
		confirmMsg := fmt.Sprintf(`✅ <b>درخواست برداشت ثبت شد</b>

💵 <b>مبلغ:</b> %s تومان
💰 <b>معادل:</b> %.4f USDT
📊 <b>نرخ:</b> %s تومان

⏳ درخواست شما در انتظار تایید ادمین است.`,
			formatToman(tomanAmount), usdtAmount, formatToman(usdtRate))

		confirmMsgToUser := tgbotapi.NewMessage(msg.Chat.ID, confirmMsg)
		confirmMsgToUser.ParseMode = "HTML"
		bot.Send(confirmMsgToUser)

		clearRegState(userID)

		// بازگشت به منوی کیف پول
		showWalletMenu(bot, db, msg.Chat.ID, userID)
		return true
	}
	if state == "reward_withdraw_amount" {
		if msg.Text == "لغو برداشت" {
			clearRegState(userID)
			showRewardsMenu(bot, db, msg.Chat.ID, userID)
			return true
		}
		amount, err := strconv.ParseFloat(msg.Text, 64)
		if err != nil || amount <= 0 {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ مبلغ نامعتبر است. لطفاً فقط عدد وارد کنید."))
			return true
		}
		user, _ := getUserByTelegramID(db, userID)
		if user == nil || user.ReferralReward < amount {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ موجودی پاداش کافی نیست."))
			return true
		}
		user.ReferralReward -= amount
		db.Save(user)
		tx := models.Transaction{
			UserID: user.ID,
			Type:   "reward_withdraw",
			Amount: amount,
			Status: "pending",
		}
		db.Create(&tx)
		adminMsg := fmt.Sprintf(`🎁 <b>درخواست برداشت پاداش</b>

		👤 <b>کاربر:</b> %s (آیدی: <code>%d</code>)
		💰 <b>مبلغ:</b> <b>%.2f USDT</b>
		
		برای تایید یا رد این برداشت پاداش، یکی از دکمه‌های زیر را انتخاب کنید.`, user.FullName, user.TelegramID, amount)
		adminBtns := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("پرداخت شد", fmt.Sprintf("approve_withdraw_%d", tx.ID)),
				tgbotapi.NewInlineKeyboardButtonData("رد شد", fmt.Sprintf("reject_withdraw_%d", tx.ID)),
			),
		)
		msgToAdmin := tgbotapi.NewMessage(adminUserID, adminMsg)
		msgToAdmin.ReplyMarkup = adminBtns
		bot.Send(msgToAdmin)
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ درخواست برداشت پاداش ثبت شد و در انتظار تایید ادمین است."))
		clearRegState(userID)

		// بازگشت به منوی پاداش
		showRewardsMenu(bot, db, msg.Chat.ID, userID)
		return true
	}

	// --- Bank Info Update States ---
	if state == "update_bank_sheba" {
		if msg.Text == "❌ لغو تغییر اطلاعات" {
			clearRegState(userID)
			showMainMenu(bot, db, msg.Chat.ID, userID)
			return true
		}

		// Validate Sheba format
		if !models.ValidateSheba(msg.Text) {
			errorMsg := `😊 <b>شماره شبا کمی اشتباه شده!</b>

نگران نباش، همه جا پیش میاد!

🏦 <b>مثال درست:</b> IR520630144905901219088011

💡 <b>نکته‌های مهم:</b>
• حتماً با IR شروع کن
• بعدش ۲۴ تا رقم بذار
• هیچ فاصله یا خط تیره نذار
• بعداً شماره کارت همین حساب رو وارد کن

🔄 یه بار دیگه امتحان کن! 😉`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "HTML"
			bot.Send(message)
			return true
		}

		// Save new sheba, ask for card number
		saveRegTemp(userID, "new_sheba", msg.Text)
		setRegState(userID, "update_bank_card")

		cardMsg := `✅ <b>مرحله ۱ تکمیل شد!</b>

🏦 شماره شبا جدید: <code>%s</code>

📝 <b>مرحله ۲: شماره کارت جدید</b>

لطفاً شماره کارت بانکی جدید خود را وارد کنید:

💡 <b>مثال درست:</b> 6037998215325563

⚠️ <b>نکته‌های مهم:</b>
• حتماً ۱۶ تا رقم باشه
• هیچ فاصله یا خط تیره نذار
• فقط عدد بنویس
• حتماً به نام خودت باشه
• حتماً شماره کارت همون حسابی که شباش رو دادی`

		message := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(cardMsg, msg.Text))
		message.ParseMode = "HTML"
		cancelKeyboard := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("❌ لغو تغییر اطلاعات"),
			),
		)
		cancelKeyboard.ResizeKeyboard = true
		message.ReplyMarkup = cancelKeyboard
		bot.Send(message)
		return true
	} else if state == "update_bank_card" {
		if msg.Text == "❌ لغو تغییر اطلاعات" {
			clearRegState(userID)
			showMainMenu(bot, db, msg.Chat.ID, userID)
			return true
		}

		// Validate card number format
		if !models.ValidateCardNumber(msg.Text) {
			errorMsg := `💳 <b>شماره کارت کمی اشتباهه!</b>

بیا دوباره درستش کنیم!

💳 <b>مثال درست:</b> 6037998215325563

💡 <b>نکته‌های مهم:</b>
• حتماً ۱۶ تا رقم باشه
• هیچ فاصله یا خط تیره نذار
• فقط عدد بنویس
• حتماً شماره کارت همون حسابی که شباش رو دادی

🔄 الان دوباره تست کن! 🙂`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "HTML"
			bot.Send(message)
			return true
		}

		// Save new card number, show confirmation
		saveRegTemp(userID, "new_card", msg.Text)
		setRegState(userID, "update_bank_confirm")

		regTemp.RLock()
		info := regTemp.m[userID]
		regTemp.RUnlock()

		confirmMsg := fmt.Sprintf(`✅ <b>تایید نهایی اطلاعات جدید</b>

📋 <b>اطلاعات جدید شما:</b>
• شماره شبا: <code>%s</code>
• شماره کارت: <code>%s</code>

⚠️ <b>نکات مهم:</b>
• این اطلاعات جایگزین اطلاعات قبلی شما خواهد شد
• شماره شبا و کارت باید از یک حساب/کارت واحد باشند

✅ اگر اطلاعات درست است، دکمه تایید را بزنید.`,
			info["new_sheba"], info["new_card"])

		keyboard := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("✅ تایید و ذخیره"),
			),
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("❌ لغو تغییر اطلاعات"),
			),
		)
		keyboard.ResizeKeyboard = true

		message := tgbotapi.NewMessage(msg.Chat.ID, confirmMsg)
		message.ParseMode = "HTML"
		message.ReplyMarkup = keyboard
		bot.Send(message)
		return true
	} else if state == "update_bank_confirm" {
		if msg.Text == "❌ لغو تغییر اطلاعات" {
			clearRegState(userID)
			showMainMenu(bot, db, msg.Chat.ID, userID)
			return true
		}

		if msg.Text == "✅ تایید و ذخیره" {
			regTemp.RLock()
			info := regTemp.m[userID]
			regTemp.RUnlock()

			// Update user bank info in database
			user, err := getUserByTelegramID(db, userID)
			if err != nil || user == nil {
				bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 متاسفانه مشکلی پیش اومد! با پشتیبانی تماس بگیر."))
				clearRegState(userID)
				return true
			}

			user.Sheba = info["new_sheba"]
			user.CardNumber = info["new_card"]

			if err := db.Save(user).Error; err != nil {
				bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 متاسفانه مشکلی در ذخیره اطلاعات پیش اومد! لطفاً دوباره تلاش کن."))
				clearRegState(userID)
				return true
			}

			clearRegState(userID)

			successMsg := fmt.Sprintf(`🎉 <b>اطلاعات بانکی با موفقیت به‌روزرسانی شد!</b>

✅ <b>اطلاعات جدید شما:</b>
• شماره شبا: <code>%s</code>
• شماره کارت: <code>%s</code>

💡 <b>نکات مهم:</b>
• از این پس تمام برداشت‌ها به این حساب واریز خواهد شد
• شبا و کارت از یک حساب واحد هستند`,
				user.Sheba, user.CardNumber)

			message := tgbotapi.NewMessage(msg.Chat.ID, successMsg)
			message.ParseMode = "HTML"
			bot.Send(message)

			// بازگشت به منوی اصلی
			showMainMenu(bot, db, msg.Chat.ID, userID)
			return true
		}

		// اگر هیچ گزینه معتبری انتخاب نشد
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😅 لطفاً یکی از گزینه‌های موجود را انتخاب کن!"))
		return true
	}

	return false
}

func handleStart(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	if isAdmin(userID) {
		showAdminMenu(bot, db, msg.Chat.ID)
		return
	}
	// ... rest of handleStart as before ...
}

func showUserInfo(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, user *models.User) {
	// استفاده از موجودی ذخیره شده در دیتابیس
	erc20Balance := user.ERC20Balance
	bep20Balance := user.BEP20Balance
	totalBalance := erc20Balance + bep20Balance

	// Count successful referrals
	var referralCount int64
	db.Model(&models.User{}).Where("referrer_id = ? AND registered = ?", user.ID, true).Count(&referralCount)

	// Count total transactions
	var totalTransactions int64
	db.Model(&models.Transaction{}).Where("user_id = ?", user.ID).Count(&totalTransactions)

	info := fmt.Sprintf(`👤 *اطلاعات کاربر*

📝 *اطلاعات شخصی:*
• نام و نام خانوادگی: %s
• نام کاربری: @%s
• شماره کارت: %s
• شماره شبا: %s
• وضعیت: ✅ ثبت‌نام شده

💰 *موجودی کیف پول:*
• موجودی کل: %.2f USDT
• 🔵 ERC20 (اتریوم): %.2f USDT
• 🟡 BEP20 (بایننس): %.2f USDT

🎁 *آمار رفرال:*
• موجودی پاداش: %.2f USDT
• تعداد زیرمجموعه: %d کاربر

📊 *آمار تراکنش:*
• کل تراکنش‌ها: %d مورد

🎉 *خوش آمدید!* حالا می‌توانید از تمام خدمات ربات استفاده کنید.`,
		user.FullName, user.Username, user.CardNumber, user.Sheba,
		totalBalance, erc20Balance, bep20Balance,
		user.ReferralReward, referralCount, totalTransactions)

	message := tgbotapi.NewMessage(chatID, info)
	message.ParseMode = "Markdown"
	bot.Send(message)
}

func handleMainMenu(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	if isAdmin(userID) {
		handleAdminMenu(bot, db, msg)
		return
	}
	// Check if user is fully registered before allowing menu access
	user, err := getUserByTelegramID(db, userID)

	if err != nil || user == nil {
		logInfo("User %d not found in main menu, redirecting to registration", userID)
		handleStart(bot, db, msg)
		return
	}

	if !user.Registered || user.FullName == "" || user.Sheba == "" || user.CardNumber == "" {
		logInfo("User %d not fully registered in main menu, redirecting to registration", userID)

		// Send a message explaining why they can't access menus
		redirectMsg := `😊 *یه قدم مونده تا آماده بشی!*

🚀 برای استفاده از همه امکانات فوق‌العاده ربات، فقط باید ثبت‌نامت رو تکمیل کنی.

✨ *چیزای ساده که باقی مونده:*
1️⃣ نام و نام خانوادگی
2️⃣ شماره شبا
3️⃣ شماره کارت

🎯 الان میبرمت به بخش ثبت‌نام...`

		message := tgbotapi.NewMessage(msg.Chat.ID, redirectMsg)
		message.ParseMode = "Markdown"
		bot.Send(message)

		handleStart(bot, db, msg)
		return
	}

	switch msg.Text {
	case "💰 کیف پول":
		showWalletMenu(bot, db, msg.Chat.ID, userID)
	case "🎁 پاداش":
		showRewardsMenu(bot, db, msg.Chat.ID, userID)
	case "📊 آمار":
		showStatsMenu(bot, db, msg.Chat.ID, userID)
	case "💱 نرخ لحظه‌ای":
		showCurrentRates(bot, db, msg.Chat.ID)
	case "🆘 پشتیبانی و آموزش":
		msg := tgbotapi.NewMessage(msg.Chat.ID, "💫 <b>کمک و راهنمایی</b>\n\n😊 سوال یا مشکلی داری؟ اینجاییم تا کمکت کنیم!\n\n💬 <b>پشتیبانی آنلاین:</b>\n👨‍💻 برای چت با تیم پشتیبانی به آیدی زیر پیام بده:\n👉 @SupportUsername\n\n📚 <b>آموزش و اطلاع‌رسانی:</b>\n🔔 برای اطلاع از آخرین اخبار و آموزش‌ها عضو کانال ما شو:\n👉 @ChannelUsername\n\n🤝 همیشه خوشحالیم که در کنارتیم!")
		msg.ParseMode = "HTML"
		bot.Send(msg)
	case "🔗 لینک رفرال":
		handleReferralLink(bot, db, msg)
	case "ترید با 🤖":
		showUserDepositsForTrade(bot, db, msg)
		return
	case "⬅️ بازگشت":
		showMainMenu(bot, db, msg.Chat.ID, userID)
	default:
		// Check if it's a submenu action
		handleSubmenuActions(bot, db, msg)
	}
}

func handleSubmenuActions(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	if isAdmin(userID) {
		handleAdminMenu(bot, db, msg)
		return
	}
	// Check if user is fully registered before allowing submenu access
	user, err := getUserByTelegramID(db, userID)

	if err != nil || user == nil {
		logInfo("User %d not found in submenu, redirecting to registration", userID)
		handleStart(bot, db, msg)
		return
	}

	if !user.Registered || user.FullName == "" || user.Sheba == "" || user.CardNumber == "" {
		logInfo("User %d not fully registered in submenu, redirecting to registration", userID)

		// Send a message explaining why they can't access menus
		redirectMsg := `🔒 *دسترسی محدود*

⚠️ برای استفاده از خدمات ربات، ابتدا باید ثبت‌نام خود را تکمیل کنید.

📝 *مراحل ثبت‌نام:*
1️⃣ نام و نام خانوادگی
2️⃣ شماره شبا
3️⃣ شماره کارت

�� در حال انتقال به صفحه ثبت‌نام...`

		message := tgbotapi.NewMessage(msg.Chat.ID, redirectMsg)
		message.ParseMode = "Markdown"
		bot.Send(message)

		handleStart(bot, db, msg)
		return
	}

	switch msg.Text {
	case "💵 برداشت":
		// Get current USDT rate
		usdtRate, err := getUSDTRate(db)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 متاسفانه نرخ تتر هنوز تنظیم نشده! \n\nلطفاً با پشتیبانی چت کن تا حلش کنیم 💪"))
			return
		}

		setRegState(userID, "withdraw_amount")
		cancelKeyboard := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("لغو برداشت"),
			),
		)

		withdrawMsg := fmt.Sprintf(`💰 <b>برداشت تومانی</b>

🎯 <b>نرخ امروز USDT:</b> %s تومان

😊 چه مقدار می‌خوای برداشت کنی؟ مبلغ رو به <b>تومان</b> بنویس:

💡 <i>مثال: 1000000 (یک میلیون تومان)</i>`, formatToman(usdtRate))

		msgSend := tgbotapi.NewMessage(msg.Chat.ID, withdrawMsg)
		msgSend.ParseMode = "HTML"
		msgSend.ReplyMarkup = cancelKeyboard
		bot.Send(msgSend)
		return
	case "💰 دریافت پاداش":
		setRegState(userID, "reward_withdraw_amount")
		cancelKeyboard := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("لغو برداشت"),
			),
		)
		msgSend := tgbotapi.NewMessage(msg.Chat.ID, "🎁 لطفاً مبلغ برداشت پاداش را به عدد وارد کنید (USDT):")
		msgSend.ReplyMarkup = cancelKeyboard
		bot.Send(msgSend)
		return
	case "📋 تاریخچه":
		showTransactionHistory(bot, db, msg)
		return
	case "💳 واریز USDT":
		handleWalletDeposit(bot, db, msg)
		return
	case "🔗 لینک رفرال":
		handleReferralLink(bot, db, msg)
		return
	case "📈 آمار شخصی":
		showPersonalStats(bot, db, msg)
		return
	case "👥 زیرمجموعه‌ها":
		showReferralList(bot, db, msg)
		return
	case "🏦 تغییر اطلاعات بانکی":
		showBankInfoChangeMenu(bot, db, msg.Chat.ID, userID)
		return
	case "✏️ شروع تغییر اطلاعات":
		startBankInfoUpdate(bot, db, msg.Chat.ID, userID)
		return
	default:
		showMainMenu(bot, db, msg.Chat.ID, userID)
	}
}

func showMainMenu(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	if isAdmin(userID) {
		showAdminMenu(bot, db, chatID)
		return
	}
	// Get user to display summary
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// استفاده از موجودی ذخیره شده در دیتابیس
	erc20Balance := user.ERC20Balance
	bep20Balance := user.BEP20Balance
	blockchainBalance := erc20Balance + bep20Balance
	tradeBalance := user.TradeBalance
	rewardBalance := user.RewardBalance
	totalBalance := blockchainBalance + tradeBalance + rewardBalance

	// Count successful referrals
	var referralCount int64
	db.Model(&models.User{}).Where("referrer_id = ? AND registered = ?", user.ID, true).Count(&referralCount)

	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💰 کیف پول"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🎁 پاداش"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 آمار"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💱 نرخ لحظه‌ای"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🆘 پشتیبانی و آموزش"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("ترید با 🤖"),
		),
	)
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	// Create main menu message with summary
	mainMsg := fmt.Sprintf(`💠 <b>خوش اومدی %s!</b>

👋 به ربات صرافی ما خوش اومدی. اینجا می‌تونی به راحتی واریز، برداشت و ترید انجام بدی.

💰 <b>موجودی فعلی شما:</b>
• کل دارایی: <b>%.2f USDT</b>
• بلاکچین: %.2f USDT
• پاداش: %.2f USDT
• 👥 زیرمجموعه‌ها: %d نفر

🔻 از منوی زیر یکی از گزینه‌ها رو انتخاب کن یا دستور مورد نظرت رو بنویس.`, user.FullName, totalBalance, blockchainBalance, rewardBalance, referralCount)

	msg := tgbotapi.NewMessage(chatID, mainMsg)
	msg.ReplyMarkup = menu
	msg.ParseMode = "HTML"
	bot.Send(msg)
}

func showWalletMenu(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	if isAdmin(userID) {
		showAdminMenu(bot, db, chatID)
		return
	}
	// Get user to calculate balances
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// استفاده از موجودی ذخیره شده در دیتابیس
	erc20Balance := user.ERC20Balance
	bep20Balance := user.BEP20Balance
	blockchainBalance := erc20Balance + bep20Balance
	tradeBalance := user.TradeBalance
	rewardBalance := user.RewardBalance
	totalBalance := blockchainBalance + tradeBalance + rewardBalance

	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💵 برداشت"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📋 تاریخچه"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💳 واریز USDT"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🏦 تغییر اطلاعات بانکی"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
		),
	)
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	// Get USDT rate for display
	usdtRate, err := getUSDTRate(db)
	var balanceMsg string

	if err == nil {
		totalToman := totalBalance * usdtRate
		blockchainToman := blockchainBalance * usdtRate
		rewardToman := rewardBalance * usdtRate
		tradeToman := tradeBalance * usdtRate
		erc20Toman := erc20Balance * usdtRate
		bep20Toman := bep20Balance * usdtRate

		// Create balance display message with Toman
		balanceMsg = fmt.Sprintf(`💰 <b>کیف پول شما</b>

💎 <b>موجودی کل:</b> 
• <b>%.4f USDT</b>
• <b>%s تومان</b>

📊 <b>جزئیات:</b>
• بلاکچین: %.4f USDT (%s تومان)
• پاداش: %.4f USDT (%s تومان)
• ترید: %.4f USDT (%s تومان)
• 🔵 ERC20: %.4f USDT (%s تومان)
• 🟡 BEP20: %.4f USDT (%s تومان)

💡 از منوی زیر برای برداشت، واریز یا مشاهده تاریخچه استفاده کن.`,
			totalBalance, formatToman(totalToman),
			blockchainBalance, formatToman(blockchainToman),
			rewardBalance, formatToman(rewardToman),
			tradeBalance, formatToman(tradeToman),
			erc20Balance, formatToman(erc20Toman),
			bep20Balance, formatToman(bep20Toman))
	} else {
		// Fallback without Toman rates
		balanceMsg = fmt.Sprintf(`💰 <b>کیف پول شما</b>

💎 <b>موجودی کل:</b> <b>%.4f USDT</b>
⚠️ <i>نرخ تنظیم نشده - با ادمین تماس بگیرید</i>

📊 <b>جزئیات:</b>
• بلاکچین: %.4f USDT
• پاداش: %.4f USDT
• ترید: %.4f USDT
• 🔵 ERC20 (اتریوم): %.4f USDT
• 🟡 BEP20 (بایننس): %.4f USDT

💡 از منوی زیر برای برداشت، واریز یا مشاهده تاریخچه استفاده کن.`,
			totalBalance, blockchainBalance, rewardBalance, tradeBalance, erc20Balance, bep20Balance)
	}

	msg := tgbotapi.NewMessage(chatID, balanceMsg)
	msg.ReplyMarkup = menu
	msg.ParseMode = "HTML"
	bot.Send(msg)
}

func showRewardsMenu(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	if isAdmin(userID) {
		showAdminMenu(bot, db, chatID)
		return
	}
	// Get user to display reward balance
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// Count successful referrals
	var referralCount int64
	db.Model(&models.User{}).Where("referrer_id = ? AND registered = ?", user.ID, true).Count(&referralCount)

	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔗 لینک رفرال"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💰 دریافت پاداش"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🏦 تغییر اطلاعات بانکی"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
		),
	)
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	// Create reward display message
	rewardMsg := fmt.Sprintf(`🎁 *منوی پاداش*

💰 *موجودی پاداش:* %.2f USDT
👥 *تعداد زیرمجموعه:* %d کاربر

💡 *گزینه‌های موجود:*
🔗 *لینک رفرال* - دریافت لینک معرفی
💰 *دریافت پاداش* - انتقال پاداش به کیف پول
⬅️ *بازگشت* - بازگشت به منوی اصلی`,
		user.ReferralReward, referralCount)

	msg := tgbotapi.NewMessage(chatID, rewardMsg)
	msg.ReplyMarkup = menu
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func showStatsMenu(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	if isAdmin(userID) {
		showAdminMenu(bot, db, chatID)
		return
	}
	// Get user to display comprehensive stats
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// Calculate USDT balances for each network
	var erc20Balance, bep20Balance float64

	// Calculate ERC20 balance (deposits - withdrawals)
	var erc20Deposits, erc20Withdrawals float64
	db.Model(&models.Transaction{}).
		Where("user_id = ? AND network = ? AND type = ? AND status = ?", user.ID, "ERC20", "deposit", "confirmed").
		Select("COALESCE(SUM(amount), 0)").
		Scan(&erc20Deposits)

	db.Model(&models.Transaction{}).
		Where("user_id = ? AND network = ? AND type = ? AND status = ?", user.ID, "ERC20", "withdraw", "confirmed").
		Select("COALESCE(SUM(amount), 0)").
		Scan(&erc20Withdrawals)

	erc20Balance = erc20Deposits - erc20Withdrawals

	// Calculate BEP20 balance (deposits - withdrawals)
	var bep20Deposits, bep20Withdrawals float64
	db.Model(&models.Transaction{}).
		Where("user_id = ? AND network = ? AND type = ? AND status = ?", user.ID, "BEP20", "deposit", "confirmed").
		Select("COALESCE(SUM(amount), 0)").
		Scan(&bep20Deposits)

	db.Model(&models.Transaction{}).
		Where("user_id = ? AND network = ? AND type = ? AND status = ?", user.ID, "BEP20", "withdraw", "confirmed").
		Select("COALESCE(SUM(amount), 0)").
		Scan(&bep20Withdrawals)

	bep20Balance = bep20Deposits - bep20Withdrawals

	// Calculate total balance
	totalBalance := erc20Balance + bep20Balance

	// Count successful referrals
	var referralCount int64
	db.Model(&models.User{}).Where("referrer_id = ? AND registered = ?", user.ID, true).Count(&referralCount)

	// Count total transactions
	var totalTransactions int64
	db.Model(&models.Transaction{}).Where("user_id = ?", user.ID).Count(&totalTransactions)

	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📈 آمار شخصی"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("👥 زیرمجموعه‌ها"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
		),
	)
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	// Create comprehensive stats display message
	statsMsg := fmt.Sprintf(`📊 *منوی آمار*

💎 *موجودی کل:* %.2f USDT
💰 *موجودی پاداش:* %.2f USDT

📈 *جزئیات موجودی:*
• 🔵 *ERC20 (اتریوم):* %.2f USDT
• 🟡 *BEP20 (بایننس):* %.2f USDT

👥 *آمار رفرال:*
• تعداد زیرمجموعه: %d کاربر
• پاداش کل: %.2f USDT

📋 *آمار تراکنش:*
• کل تراکنش‌ها: %d مورد

💡 *گزینه‌های موجود:*
📈 *آمار شخصی* - آمار تراکنش‌ها و موجودی
👥 *زیرمجموعه‌ها* - لیست کاربران معرفی شده
⬅️ *بازگشت* - بازگشت به منوی اصلی`,
		totalBalance, user.ReferralReward, erc20Balance, bep20Balance, referralCount, user.ReferralReward, totalTransactions)

	msg := tgbotapi.NewMessage(chatID, statsMsg)
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
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 اول باید با /start شروع کنی! \n\nروی /start بزن تا ثبت‌نامت کنیم 😊"))
		return
	}

	if user.Registered && user.FullName != "" && user.Sheba != "" && user.CardNumber != "" {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "🎉 عالی! تو قبلاً ثبت‌نام کردی و همه چیز کامله! \n\nمی‌تونی از همه امکانات استفاده کنی 💪"))
		return
	}

	// Start registration process for incomplete user
	setRegState(userID, "full_name")
	regTemp.Lock()
	regTemp.m[userID] = make(map[string]string)
	regTemp.Unlock()
	bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😊 بیا ثبت‌نامت رو تکمیل کنیم! \n\nاول نام و نام خانوادگیت رو بنویس:"))
}

// Handler for 'لینک رفرال'
func handleReferralLink(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "کاربر یافت نشد. لطفاً ابتدا ثبت‌نام کنید."))
		return
	}

	// Get bot username
	botUser := bot.Self.UserName
	refLink := "https://t.me/" + botUser + "?start=" + fmt.Sprintf("%d", user.TelegramID)

	// Count successful referrals
	var count int64
	db.Model(&models.User{}).Where("referrer_id = ? AND registered = ?", user.ID, true).Count(&count)

	msgText := fmt.Sprintf(`🔗 *لینک رفرال اختصاصی شما*

%s

📊 *آمار رفرال:*
• تعداد زیرمجموعه: %d کاربر
• موجودی پاداش: %.2f USDT

💡 *نحوه استفاده:*
توضیحات نحوه استفاده به زودی اضافه میشود.`,
		refLink, count, user.ReferralReward)

	message := tgbotapi.NewMessage(msg.Chat.ID, msgText)
	message.ParseMode = "Markdown"
	bot.Send(message)
}

// Handler for 'دریافت پاداش'
func handleReward(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "کاربر یافت نشد. لطفاً ابتدا ثبت‌نام کنید."))
		return
	}

	// Count successful referrals
	var referralCount int64
	db.Model(&models.User{}).Where("referrer_id = ? AND registered = ?", user.ID, true).Count(&referralCount)

	msgText := fmt.Sprintf(`💰 *موجودی پاداش شما*

💎 *پاداش کل:* %.2f USDT
👥 *تعداد زیرمجموعه:* %d کاربر

⚠️ *توجه:* برداشت پاداش به زودی به ربات اضافه خواهد شد.`,
		user.ReferralReward, referralCount)

	message := tgbotapi.NewMessage(msg.Chat.ID, msgText)
	message.ParseMode = "Markdown"
	bot.Send(message)
}

func handleWalletDeposit(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "کاربر یافت نشد. لطفاً ابتدا ثبت‌نام کنید."))
		return
	}

	// استفاده از موجودی ذخیره شده در دیتابیس
	erc20Balance := user.ERC20Balance
	bep20Balance := user.BEP20Balance

	// For old users: if missing wallet, generate and save
	if user.ERC20Address == "" || user.BEP20Address == "" {
		ethMnemonic, ethPriv, ethAddr, err := models.GenerateEthWallet()
		if err == nil && user.ERC20Address == "" {
			user.ERC20Address = ethAddr
			user.ERC20Mnemonic = ethMnemonic
			user.ERC20PrivKey = ethPriv
		}
		bepMnemonic, bepPriv, bepAddr, err := models.GenerateEthWallet()
		if err == nil && user.BEP20Address == "" {
			user.BEP20Address = bepAddr
			user.BEP20Mnemonic = bepMnemonic
			user.BEP20PrivKey = bepPriv
		}
		db.Save(user)
		if user.ERC20Address == "" || user.BEP20Address == "" {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 متاسفانه مشکل فنی در ساخت کیف پول! \n\nلطفاً با پشتیبانی چت کن تا سریع حلش کنیم 🛠️"))
			return
		}
	}

	// دریافت حداقل واریز
	minDeposit := getMinDepositUSDT(db)

	msgText := fmt.Sprintf(`💳 *آدرس‌های واریز USDT شما*

💰 *موجودی فعلی:*
• 🔵 ERC20 (اتریوم): %.2f USDT
• 🟡 BEP20 (بایننس): %.2f USDT

📥 *آدرس‌های واریز:*

🔵 *ERC20 (اتریوم):*
`+"`%s`"+`

🟡 *BEP20 (بایننس اسمارت چین):*
`+"`%s`"+`

⚠️ *نکات مهم:*
• حتماً USDT رو به شبکه درست بفرست
• اگه اشتباه بفرستی، پولت گم میشه 💔
• حداقل واریز: %.0f USDT`,
		erc20Balance, bep20Balance, user.ERC20Address, user.BEP20Address, minDeposit)

	message := tgbotapi.NewMessage(msg.Chat.ID, msgText)
	message.ParseMode = "Markdown"
	bot.Send(message)
}

func showReferralList(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "کاربر یافت نشد."))
		return
	}

	var referrals []models.User
	db.Where("referrer_id = ?", user.ID).Order("created_at desc").Find(&referrals)

	if len(referrals) == 0 {
		emptyMsg := tgbotapi.NewMessage(msg.Chat.ID, "👥 <b>لیست زیرمجموعه‌ها</b>\n\n😊 هنوز کسی با لینک تو عضو نشده!\n\n🚀 برای معرفی دوستات، لینک رفرالت رو باهاشون به اشتراک بذار و پاداش بگیر! 💰")
		emptyMsg.ParseMode = "HTML"
		bot.Send(emptyMsg)
		return
	}

	// Count registered vs unregistered
	var registeredCount, unregisteredCount int64
	for _, ref := range referrals {
		if ref.Registered {
			registeredCount++
		} else {
			unregisteredCount++
		}
	}

	msgText := fmt.Sprintf(`👥 <b>لیست زیرمجموعه‌های شما</b>

📊 <b>آمار کلی:</b>
• کل زیرمجموعه: %d کاربر
• ثبت‌نام شده: %d کاربر
• ناتمام: %d کاربر

📋 <b>جزئیات زیرمجموعه‌ها:</b>`, len(referrals), registeredCount, unregisteredCount)

	for i, ref := range referrals {
		var name string
		if ref.Username != "" {
			name = "@" + ref.Username
		} else {
			name = fmt.Sprintf("ID: %d", ref.TelegramID)
		}

		status := "❌ ناتمام"
		if ref.Registered {
			status = "✅ ثبت‌نام شده"
		}

		// Format registration date
		dateStr := ref.CreatedAt.Format("02/01/2006")

		msgText += fmt.Sprintf("\n%d. %s - %s (%s)", i+1, name, status, dateStr)
	}

	msgText += "\n\n💡 نکته: فقط کاربران ثبت‌نام شده پاداش محاسبه می‌شوند."

	message := tgbotapi.NewMessage(msg.Chat.ID, msgText)
	message.ParseMode = "HTML"
	bot.Send(message)
}

func showTransactionHistory(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "کاربر یافت نشد."))
		return
	}

	var txs []models.Transaction
	db.Where("user_id = ?", user.ID).Order("created_at desc").Limit(10).Find(&txs)

	if len(txs) == 0 {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "📋 *تاریخچه تراکنش‌ها*\n\n😊 هنوز هیچ تراکنشی نداری!\n\n🚀 اولین واریز یا برداشتت رو انجام بده تا اینجا نمایش داده بشه."))
		return
	}

	// Calculate summary statistics
	var totalDeposits, totalWithdrawals, totalRewardWithdrawals float64
	var depositCount, withdrawCount, rewardWithdrawCount int64

	for _, tx := range txs {
		if tx.Status != "confirmed" {
			continue
		}
		if tx.Type == "deposit" {
			totalDeposits += tx.Amount
			depositCount++
		} else if tx.Type == "withdraw" {
			totalWithdrawals += tx.Amount
			withdrawCount++
		} else if tx.Type == "reward_withdraw" {
			totalRewardWithdrawals += tx.Amount
			rewardWithdrawCount++
		}
	}

	history := fmt.Sprintf(`📋 <b>تاریخچه تراکنش‌ها</b>

📊 <b>خلاصه (آخرین ۱۰ تراکنش):</b>
• کل واریز: <b>%.2f USDT</b> (%d تراکنش)
• کل برداشت: <b>%.2f USDT</b> (%d تراکنش)
• کل برداشت پاداش: <b>%.2f USDT</b> (%d تراکنش)

📋 <b>جزئیات تراکنش‌ها:</b>`, totalDeposits, depositCount, totalWithdrawals, withdrawCount, totalRewardWithdrawals, rewardWithdrawCount)

	for i, tx := range txs {
		typeFa := "💳 واریز"
		if tx.Type == "withdraw" {
			typeFa = "💵 برداشت"
		} else if tx.Type == "reward_withdraw" {
			typeFa = "🎁 برداشت پاداش"
		}

		networkFa := ""
		if tx.Network == "ERC20" {
			networkFa = "🔵 ERC20"
		} else if tx.Network == "BEP20" {
			networkFa = "🟡 BEP20"
		}

		statusFa := "⏳ در انتظار"
		if tx.Status == "confirmed" {
			statusFa = "✅ تایید شده"
		} else if tx.Status == "failed" {
			statusFa = "❌ ناموفق"
		} else if tx.Status == "canceled" {
			statusFa = "❌ لغو شده"
		}

		// Format transaction date
		dateStr := tx.CreatedAt.Format("02/01 15:04")

		history += fmt.Sprintf("\n%d. %s %s - %.2f USDT - %s (%s)",
			i+1, typeFa, networkFa, tx.Amount, statusFa, dateStr)
	}

	history += "\n\n💡 *نکته:* فقط تراکنش‌های تایید شده در موجودی محاسبه می‌شوند."

	message := tgbotapi.NewMessage(msg.Chat.ID, history)
	message.ParseMode = "HTML"
	bot.Send(message)
}

func showPersonalStats(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "کاربر یافت نشد. لطفاً ابتدا ثبت‌نام کنید."))
		return
	}

	// استفاده از موجودی ذخیره شده در دیتابیس
	erc20Balance := user.ERC20Balance
	bep20Balance := user.BEP20Balance
	tradeBalance := user.TradeBalance
	rewardBalance := user.RewardBalance
	totalBalance := erc20Balance + bep20Balance + tradeBalance + rewardBalance

	// Count successful referrals
	var referralCount int64
	db.Model(&models.User{}).Where("referrer_id = ? AND registered = ?", user.ID, true).Count(&referralCount)

	// Count transactions by type and network
	var erc20DepositCount, erc20WithdrawCount, bep20DepositCount, bep20WithdrawCount int64
	db.Model(&models.Transaction{}).Where("user_id = ? AND network = ? AND type = ?", user.ID, "ERC20", "deposit").Count(&erc20DepositCount)
	db.Model(&models.Transaction{}).Where("user_id = ? AND network = ? AND type = ?", user.ID, "ERC20", "withdraw").Count(&erc20WithdrawCount)
	db.Model(&models.Transaction{}).Where("user_id = ? AND network = ? AND type = ?", user.ID, "BEP20", "deposit").Count(&bep20DepositCount)
	db.Model(&models.Transaction{}).Where("user_id = ? AND network = ? AND type = ?", user.ID, "BEP20", "withdraw").Count(&bep20WithdrawCount)

	totalTransactions := erc20DepositCount + erc20WithdrawCount + bep20DepositCount + bep20WithdrawCount

	statsMsg := fmt.Sprintf(`📈 *آمار شخصی*

👤 *اطلاعات کاربر:*
• نام: %s
• نام کاربری: @%s
• تاریخ عضویت: %s

💰 *موجودی کیف پول:*
• موجودی کل: %.2f USDT
• 🔵 ERC20 (اتریوم): %.2f USDT
• 🟡 BEP20 (بایننس): %.2f USDT
• سود/ضرر ترید: %.2f USDT
• پاداش: %.2f USDT

🎁 *آمار رفرال:*
• تعداد زیرمجموعه: %d کاربر

📊 *آمار تراکنش‌ها:*
• کل تراکنش‌ها: %d مورد
• 🔵 ERC20 واریز: %d مورد
• 🔵 ERC20 برداشت: %d مورد
• 🟡 BEP20 واریز: %d مورد
• 🟡 BEP20 برداشت: %d مورد`,
		user.FullName, user.Username, user.CreatedAt.Format("02/01/2006"),
		totalBalance, erc20Balance, bep20Balance, tradeBalance, rewardBalance,
		referralCount, totalTransactions,
		erc20DepositCount, erc20WithdrawCount, bep20DepositCount, bep20WithdrawCount)

	message := tgbotapi.NewMessage(msg.Chat.ID, statsMsg)
	message.ParseMode = "Markdown"
	bot.Send(message)
}

// Helper for confirm keyboard
func confirmBroadcastKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ ارسال", "broadcast_send"),
			tgbotapi.NewInlineKeyboardButtonData("لغو ارسال", "broadcast_cancel"),
		),
	)
}

func showUsersPageEdit(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, adminID int64, page int, messageID int) {
	const usersPerPage = 10

	// Get total count first
	var totalUsers int64
	db.Model(&models.User{}).Count(&totalUsers)

	if totalUsers == 0 {
		editMsg := tgbotapi.NewEditMessageText(chatID, messageID, "👥 هیچ کاربری در دیتابیس وجود ندارد.")
		editMsg.ParseMode = "HTML"
		bot.Send(editMsg)
		return
	}

	totalPages := int((totalUsers + usersPerPage - 1) / usersPerPage)

	// Validate page number
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	// Update admin's current page
	adminUsersPage[adminID] = page

	// Get users for current page with optimized query
	var users []struct {
		models.User
		ReferralCount int64 `gorm:"column:referral_count"`
	}

	offset := page * usersPerPage

	// Single optimized query with LEFT JOIN for referral count
	db.Table("users").
		Select("users.*, COALESCE(COUNT(referrals.id), 0) as referral_count").
		Joins("LEFT JOIN users AS referrals ON referrals.referrer_id = users.id AND referrals.registered = true").
		Group("users.id").
		Order("users.created_at desc").
		Limit(usersPerPage).
		Offset(offset).
		Find(&users)

	var usersList string
	usersList = fmt.Sprintf("👥 <b>لیست کاربران (صفحه %d از %d)</b>\n", page+1, totalPages)
	usersList += fmt.Sprintf("📊 <b>مجموع:</b> %d کاربر\n\n", totalUsers)

	for _, userData := range users {
		user := userData.User
		referralCount := userData.ReferralCount

		status := "❌ ناقص"
		if user.Registered {
			status = "✅ تکمیل"
		}

		// محاسبه موجودی کل
		totalBalance := user.ERC20Balance + user.BEP20Balance + user.TradeBalance + user.RewardBalance

		usersList += fmt.Sprintf(`🆔 <b>%d</b> | %s
👤 <b>نام:</b> %s
📱 <b>یوزرنیم:</b> @%s
🔑 <b>User ID:</b> <code>%d</code>
💰 <b>موجودی:</b> %.2f USDT
🎁 <b>پاداش:</b> %.2f USDT
👥 <b>زیرمجموعه:</b> %d نفر
📅 <b>تاریخ عضویت:</b> %s
📋 <b>وضعیت:</b> %s

━━━━━━━━━━━━━━━━━━━━━━

`, user.TelegramID, user.FullName, user.FullName, user.Username, user.ID, totalBalance, user.ReferralReward, referralCount, user.CreatedAt.Format("02/01/2006"), status)
	}

	// Create navigation buttons
	var buttons [][]tgbotapi.InlineKeyboardButton

	// Navigation row
	var navRow []tgbotapi.InlineKeyboardButton

	if page > 0 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("⬅️ قبلی", fmt.Sprintf("users_page_%d", page-1)))
	}

	navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("📄 %d/%d", page+1, totalPages), "users_current_page"))

	if page < totalPages-1 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("➡️ بعدی", fmt.Sprintf("users_page_%d", page+1)))
	}

	if len(navRow) > 0 {
		buttons = append(buttons, navRow)
	}

	// Quick jump buttons (if more than 3 pages)
	if totalPages > 3 {
		var jumpRow []tgbotapi.InlineKeyboardButton
		jumpRow = append(jumpRow, tgbotapi.NewInlineKeyboardButtonData("🔢 اول", "users_page_0"))
		if totalPages > 1 {
			jumpRow = append(jumpRow, tgbotapi.NewInlineKeyboardButtonData("🔢 آخر", fmt.Sprintf("users_page_%d", totalPages-1)))
		}
		buttons = append(buttons, jumpRow)
	}

	// Refresh and close buttons
	actionRow := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("🔄 بروزرسانی", fmt.Sprintf("users_page_%d", page)),
		tgbotapi.NewInlineKeyboardButtonData("❌ بستن", "users_close"),
	}
	buttons = append(buttons, actionRow)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)

	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, usersList)
	editMsg.ParseMode = "HTML"
	editMsg.ReplyMarkup = &keyboard
	bot.Send(editMsg)
}

func showUsersPage(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, adminID int64, page int) {
	const usersPerPage = 10

	// Get total count first
	var totalUsers int64
	db.Model(&models.User{}).Count(&totalUsers)

	if totalUsers == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "👥 هیچ کاربری در دیتابیس وجود ندارد."))
		return
	}

	totalPages := int((totalUsers + usersPerPage - 1) / usersPerPage)

	// Validate page number
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	// Update admin's current page
	adminUsersPage[adminID] = page

	// Get users for current page with optimized query
	var users []struct {
		models.User
		ReferralCount int64 `gorm:"column:referral_count"`
	}

	offset := page * usersPerPage

	// Single optimized query with LEFT JOIN for referral count
	db.Table("users").
		Select("users.*, COALESCE(COUNT(referrals.id), 0) as referral_count").
		Joins("LEFT JOIN users AS referrals ON referrals.referrer_id = users.id AND referrals.registered = true").
		Group("users.id").
		Order("users.created_at desc").
		Limit(usersPerPage).
		Offset(offset).
		Find(&users)

	var usersList string
	usersList = fmt.Sprintf("👥 <b>لیست کاربران (صفحه %d از %d)</b>\n", page+1, totalPages)
	usersList += fmt.Sprintf("📊 <b>مجموع:</b> %d کاربر\n\n", totalUsers)

	for _, userData := range users {
		user := userData.User
		referralCount := userData.ReferralCount

		status := "❌ ناقص"
		if user.Registered {
			status = "✅ تکمیل"
		}

		// محاسبه موجودی کل
		totalBalance := user.ERC20Balance + user.BEP20Balance + user.TradeBalance + user.RewardBalance

		usersList += fmt.Sprintf(`🆔 <b>%d</b> | %s
👤 <b>نام:</b> %s
📱 <b>یوزرنیم:</b> @%s
🔑 <b>User ID:</b> <code>%d</code>
💰 <b>موجودی:</b> %.2f USDT
🎁 <b>پاداش:</b> %.2f USDT
👥 <b>زیرمجموعه:</b> %d نفر
📅 <b>تاریخ عضویت:</b> %s
📋 <b>وضعیت:</b> %s

━━━━━━━━━━━━━━━━━━━━━━

`, user.TelegramID, user.FullName, user.FullName, user.Username, user.ID, totalBalance, user.ReferralReward, referralCount, user.CreatedAt.Format("02/01/2006"), status)
	}

	// Create navigation buttons
	var buttons [][]tgbotapi.InlineKeyboardButton

	// Navigation row
	var navRow []tgbotapi.InlineKeyboardButton

	if page > 0 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("⬅️ قبلی", fmt.Sprintf("users_page_%d", page-1)))
	}

	navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("📄 %d/%d", page+1, totalPages), "users_current_page"))

	if page < totalPages-1 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("➡️ بعدی", fmt.Sprintf("users_page_%d", page+1)))
	}

	if len(navRow) > 0 {
		buttons = append(buttons, navRow)
	}

	// Quick jump buttons (if more than 3 pages)
	if totalPages > 3 {
		var jumpRow []tgbotapi.InlineKeyboardButton
		jumpRow = append(jumpRow, tgbotapi.NewInlineKeyboardButtonData("🔢 اول", "users_page_0"))
		if totalPages > 1 {
			jumpRow = append(jumpRow, tgbotapi.NewInlineKeyboardButtonData("🔢 آخر", fmt.Sprintf("users_page_%d", totalPages-1)))
		}
		buttons = append(buttons, jumpRow)
	}

	// Refresh and close buttons
	actionRow := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("🔄 بروزرسانی", fmt.Sprintf("users_page_%d", page)),
		tgbotapi.NewInlineKeyboardButtonData("❌ بستن", "users_close"),
	}
	buttons = append(buttons, actionRow)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)

	msg := tgbotapi.NewMessage(chatID, usersList)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func showAllPendingWithdrawals(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64) {
	var txs []models.Transaction
	db.Where("status = ?", "pending").Order("created_at desc").Find(&txs)
	if len(txs) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "⏳ هیچ برداشت در انتظاری وجود ندارد."))
		return
	}
	for _, tx := range txs {
		var user models.User
		db.First(&user, tx.UserID)
		typeFa := "💵 برداشت"
		if tx.Type == "reward_withdraw" {
			typeFa = "🎁 برداشت پاداش"
		}
		msgText := fmt.Sprintf("%s - %.2f USDT\nکاربر: %s (%d)\nتاریخ: %s", typeFa, tx.Amount, user.FullName, user.TelegramID, tx.CreatedAt.Format("02/01 15:04"))
		adminBtns := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("پرداخت شد", fmt.Sprintf("approve_withdraw_%d", tx.ID)),
				tgbotapi.NewInlineKeyboardButtonData("رد شد", fmt.Sprintf("reject_withdraw_%d", tx.ID)),
			),
		)
		m := tgbotapi.NewMessage(chatID, msgText)
		m.ReplyMarkup = adminBtns
		bot.Send(m)
	}
}

func showUserDepositsForTrade(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	telegramID := int64(msg.From.ID)
	var user models.User
	if err := db.Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "کاربر یافت نشد."))
		return
	}
	var deposits []models.Transaction
	db.Where("user_id = ? AND type = ? AND status = ?", user.ID, "deposit", "confirmed").Find(&deposits)
	if len(deposits) == 0 {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "هیچ واریزی قابل ترید ندارید."))
		return
	}
	found := false
	for _, tx := range deposits {
		if tx.TradeCount >= 3 {
			continue
		}
		found = true
		tradeBtn := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("درخواست ترید (%d/3)", tx.TradeCount), fmt.Sprintf("request_trade_%d", tx.ID)),
			),
		)
		msgText := fmt.Sprintf("واریز: %.2f USDT\nتاریخ: %s", tx.Amount, tx.CreatedAt.Format("02/01 15:04"))
		m := tgbotapi.NewMessage(msg.Chat.ID, msgText)
		m.ReplyMarkup = tradeBtn
		bot.Send(m)
	}
	if !found {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "همه واریزهای شما قبلاً ۳ بار ترید شده‌اند."))
	}
}

// تابع min را اضافه کن:
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// --- نرخ USDT ---
func getUSDTRate(db *gorm.DB) (float64, error) {
	var rate models.Rate
	if err := db.Where("asset = ?", "USDT").First(&rate).Error; err != nil {
		return 0, err
	}
	return rate.Value, nil
}

// --- مبلغ با جداکننده هزارگان ---
func formatToman(val float64) string {
	v := int64(val + 0.5) // گرد کردن
	s := fmt.Sprintf("%d", v)
	n := len(s)
	if n <= 3 {
		return s
	}
	res := ""
	for i, c := range s {
		if i > 0 && (n-i)%3 == 0 {
			res += ","
		}
		res += string(c)
	}
	return res
}

// --- Settings Management ---
func getSetting(db *gorm.DB, key string, defaultValue string) string {
	var setting models.Settings
	if err := db.Where("key = ?", key).First(&setting).Error; err != nil {
		return defaultValue
	}
	return setting.Value
}

func setSetting(db *gorm.DB, key, value, description string) error {
	var setting models.Settings
	if err := db.Where("key = ?", key).First(&setting).Error; err != nil {
		// ایجاد تنظیم جدید
		setting = models.Settings{
			Key:         key,
			Value:       value,
			Description: description,
		}
		return db.Create(&setting).Error
	} else {
		// به‌روزرسانی تنظیم موجود
		setting.Value = value
		if description != "" {
			setting.Description = description
		}
		return db.Save(&setting).Error
	}
}

func getMinDepositUSDT(db *gorm.DB) float64 {
	val := getSetting(db, models.SETTING_MIN_DEPOSIT_USDT, "100")
	result, _ := strconv.ParseFloat(val, 64)
	return result
}

func getMinWithdrawToman(db *gorm.DB) float64 {
	val := getSetting(db, models.SETTING_MIN_WITHDRAW_TOMAN, "5000000")
	result, _ := strconv.ParseFloat(val, 64)
	return result
}

func getMaxWithdrawToman(db *gorm.DB) float64 {
	val := getSetting(db, models.SETTING_MAX_WITHDRAW_TOMAN, "100000000")
	result, _ := strconv.ParseFloat(val, 64)
	return result
}

// --- Initialize Default Settings ---
func InitializeDefaultSettings(db *gorm.DB) {
	setSetting(db, models.SETTING_MIN_DEPOSIT_USDT, "100", "حداقل مبلغ واریز (USDT)")
	setSetting(db, models.SETTING_MIN_WITHDRAW_TOMAN, "5000000", "حداقل مبلغ برداشت (تومان)")
	setSetting(db, models.SETTING_MAX_WITHDRAW_TOMAN, "100000000", "حداکثر مبلغ برداشت (تومان)")
}

func showCurrentRates(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64) {
	// دریافت نرخ USDT
	usdtRate, err := getUSDTRate(db)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 متاسفانه نرخ‌ها هنوز تنظیم نشده! \n\nبا پشتیبانی تماس بگیر تا حلش کنیم 💪"))
		return
	}

	// دریافت محدودیت‌ها
	minDeposit := getMinDepositUSDT(db)
	minWithdraw := getMinWithdrawToman(db)
	maxWithdraw := getMaxWithdrawToman(db)

	rateMsg := fmt.Sprintf(`💱 <b>نرخ‌های فعلی</b>

🎯 <b>نرخ خرید USDT:</b> %s تومان

📋 <b>محدودیت‌ها:</b>
• حداقل واریز: %.0f USDT
• حداقل برداشت: %s تومان  
• حداکثر برداشت: %s تومان

💡 <b>مثال محاسبه:</b>
• ۱ USDT = %s تومان
• ۱۰ USDT = %s تومان
• ۱۰۰ USDT = %s تومان

⏰ آخرین بروزرسانی: همین الان`,
		formatToman(usdtRate),
		minDeposit,
		formatToman(minWithdraw),
		formatToman(maxWithdraw),
		formatToman(usdtRate),
		formatToman(usdtRate*10),
		formatToman(usdtRate*100))

	message := tgbotapi.NewMessage(chatID, rateMsg)
	message.ParseMode = "HTML"
	bot.Send(message)
}

func showBankInfoChangeMenu(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// نمایش اطلاعات فعلی
	currentInfoMsg := fmt.Sprintf(`🏦 <b>تغییر اطلاعات بانکی</b>

📋 <b>اطلاعات فعلی شما:</b>
• شماره شبا: <code>%s</code>
• شماره کارت: <code>%s</code>

💡 <b>دلایل تغییر اطلاعات:</b>
• کارت قبلی از دسترس خارج شده
• کارت جدید دریافت کرده‌اید
• تغییر بانک

⚠️ <b>نکات مهم:</b>
• شماره کارت و شبا حتماً باید به نام خودتان باشد: <b>%s</b>
• شماره شبا و شماره کارت حتماً باید از یک کارت/حساب باشند

🚀 برای شروع تغییر اطلاعات، دکمه زیر را بزنید.`,
		user.Sheba, user.CardNumber, user.FullName)

	// کیبورد برای شروع تغییر
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("✏️ شروع تغییر اطلاعات"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
		),
	)
	keyboard.ResizeKeyboard = true
	keyboard.OneTimeKeyboard = false

	message := tgbotapi.NewMessage(chatID, currentInfoMsg)
	message.ParseMode = "HTML"
	message.ReplyMarkup = keyboard
	bot.Send(message)
}

func startBankInfoUpdate(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	// شروع فرآیند به‌روزرسانی اطلاعات بانکی
	setRegState(userID, "update_bank_sheba")

	// مقداردهی اولیه regTemp
	regTemp.Lock()
	regTemp.m[userID] = make(map[string]string)
	regTemp.Unlock()

	// کیبورد برای لغو
	cancelKeyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("❌ لغو تغییر اطلاعات"),
		),
	)
	cancelKeyboard.ResizeKeyboard = true
	cancelKeyboard.OneTimeKeyboard = false

	shebaMsg := `📝 <b>مرحله ۱: شماره شبا جدید</b>

لطفاً شماره شبا جدید حساب بانکی خود را وارد کنید:

💡 <b>مثال درست:</b> IR520630144905901219088011

⚠️ <b>نکته‌های مهم:</b>
• حتماً با IR شروع کن
• بعدش ۲۴ تا رقم بذار
• هیچ فاصله یا خط تیره نذار
• حتماً به نام خودت باشه
• بعداً شماره کارت همین حساب رو وارد کن`

	message := tgbotapi.NewMessage(chatID, shebaMsg)
	message.ParseMode = "HTML"
	message.ReplyMarkup = cancelKeyboard
	bot.Send(message)
}

func showLimitsSettings(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64) {
	// دریافت تنظیمات فعلی
	minDeposit := getMinDepositUSDT(db)
	minWithdraw := getMinWithdrawToman(db)
	maxWithdraw := getMaxWithdrawToman(db)

	settingsMsg := fmt.Sprintf(`⚙️ <b>تنظیمات محدودیت‌ها</b>

📋 <b>وضعیت فعلی:</b>
• حداقل واریز: %.0f USDT
• حداقل برداشت: %s تومان
• حداکثر برداشت: %s تومان

🔧 <b>دستورات تغییر:</b>
• <code>/setmindeposit AMOUNT</code> - تنظیم حداقل واریز (USDT)
• <code>/setminwithdraw AMOUNT</code> - تنظیم حداقل برداشت (تومان)  
• <code>/setmaxwithdraw AMOUNT</code> - تنظیم حداکثر برداشت (تومان)

💡 <b>مثال:</b>
<code>/setmindeposit 50</code>
<code>/setminwithdraw 3000000</code>
<code>/setmaxwithdraw 200000000</code>`,
		minDeposit,
		formatToman(minWithdraw),
		formatToman(maxWithdraw))

	message := tgbotapi.NewMessage(chatID, settingsMsg)
	message.ParseMode = "HTML"
	bot.Send(message)
}

func showRatesManagement(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64) {
	// دریافت نرخ‌های فعلی
	var rates []models.Rate
	db.Find(&rates)

	rateMsg := "💱 <b>مدیریت نرخ‌ها</b>\n\n"

	if len(rates) == 0 {
		rateMsg += "😔 هنوز هیچ نرخی تنظیم نشده!\n\n"
	} else {
		rateMsg += "📊 <b>نرخ‌های فعلی:</b>\n"
		for _, r := range rates {
			rateMsg += fmt.Sprintf("• %s: %s تومان\n", r.Asset, formatToman(r.Value))
		}
		rateMsg += "\n"
	}

	rateMsg += `🔧 <b>دستورات:</b>
• <code>/setrate ASSET RATE</code> - تنظیم نرخ ارز
• <code>/rates</code> - نمایش همه نرخ‌ها

💡 <b>مثال:</b>
<code>/setrate USDT 58500</code>
<code>/setrate BTC 3500000000</code>`

	message := tgbotapi.NewMessage(chatID, rateMsg)
	message.ParseMode = "HTML"
	bot.Send(message)
}
