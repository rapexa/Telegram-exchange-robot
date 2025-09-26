package handlers

import (
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
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
var adminUserIDs = []int64{
	7403868937, // Original admin
	7947533993, // New admin
}

// Track admin state for broadcast
var adminState = make(map[int64]string)

var adminBroadcastState = make(map[int64]string) // "awaiting_broadcast", "confirm_broadcast", ""
var adminBroadcastDraft = make(map[int64]*tgbotapi.Message)

// Track admin users list pagination
var adminUsersPage = make(map[int64]int) // userID -> current page number

// Track admin search state
var adminSearchState = make(map[int64]string)                   // userID -> search state
var adminSearchFilters = make(map[int64]map[string]interface{}) // userID -> search filters

func isAdmin(userID int64) bool {
	for _, adminID := range adminUserIDs {
		if userID == adminID {
			return true
		}
	}
	return false
}

// sendToAllAdmins sends a message to all admin users
func sendToAllAdmins(bot *tgbotapi.BotAPI, message string) {
	for _, adminID := range adminUserIDs {
		msg := tgbotapi.NewMessage(adminID, message)
		bot.Send(msg)
	}
}

// sendToAllAdminsWithMarkup sends a message with markup to all admin users
func sendToAllAdminsWithMarkup(bot *tgbotapi.BotAPI, message string, markup interface{}) {
	for _, adminID := range adminUserIDs {
		msg := tgbotapi.NewMessage(adminID, message)
		msg.ParseMode = "HTML"
		msg.ReplyMarkup = markup
		bot.Send(msg)
	}
}

func showAdminMenu(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64) {
	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 آمار کلی"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("👥 مشاهده همه کاربران"),
			tgbotapi.NewKeyboardButton("🔍 جستجوی کاربران"),
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
		"*💰 مدیریت موجودی USDT:*\n" +
		"• `/addusdt USER_ID AMOUNT` — افزایش موجودی USDT کاربر\n" +
		"• `/subusdt USER_ID AMOUNT` — کاهش موجودی USDT کاربر\n" +
		"• `/setusdt USER_ID AMOUNT` — تنظیم موجودی USDT کاربر\n\n" +
		"*💵 مدیریت موجودی تومانی:*\n" +
		"• `/addtoman USER_ID AMOUNT` — افزایش موجودی تومانی کاربر\n" +
		"• `/subtoman USER_ID AMOUNT` — کاهش موجودی تومانی کاربر\n" +
		"• `/settoman USER_ID AMOUNT` — تنظیم موجودی تومانی کاربر\n\n" +
		"*👤 اطلاعات کاربران:*\n" +
		"• `/userinfo USER_ID` — مشاهده اطلاعات کامل کاربر و کیف پول\n" +
		"• `/backup` — دریافت فایل پشتیبان دیتابیس (mysqldump)\n" +
		"• `/simplebackup` — دریافت فایل پشتیبان ساده (Go-based)\n\n" +
		"*📈 مدیریت ترید:*\n" +
		"• `/settrade [شماره معامله] [حداقل درصد] [حداکثر درصد]`\n" +
		"  └ تنظیم بازه سود/ضرر برای هر ترید\n\n" +
		"• `/trades`\n" +
		"  └ نمایش رنج‌های فعلی ترید\n\n" +
		"*💱 مدیریت نرخ‌ها:*\n" +
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

	// Check if admin is in search mode
	if adminSearchState[msg.From.ID] != "" && adminSearchState[msg.From.ID] != "search_menu" {
		handleSearchInput(bot, db, msg)
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
		adminUsersPage[msg.From.ID] = 0       // Reset to first page
		adminSearchState[msg.From.ID] = ""    // Clear search state
		adminSearchFilters[msg.From.ID] = nil // Clear filters
		showUsersPage(bot, db, msg.Chat.ID, msg.From.ID, 0)
		return
	case "🔍 جستجوی کاربران":
		showUserSearchMenu(bot, db, msg.Chat.ID, msg.From.ID)
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
	case "❌ لغو و بازگشت":
		clearRegState(msg.From.ID)
		showBankAccountsManagement(bot, db, msg.Chat.ID, msg.From.ID)
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

func logInfo(format string, v ...interface{}) {
	log.Printf("[INFO] "+format, v...)
}

func logError(format string, v ...interface{}) {
	log.Printf("[ERROR] "+format, v...)
}

func logDebug(format string, v ...interface{}) {
	log.Printf("[DEBUG] "+format, v...)
}

// calculateReferralRewards calculates and distributes referral rewards for a transaction
// IMPORTANT: This function ONLY processes rewards for TRADES, not for deposits or withdrawals
// Referral rewards are only given when users perform trading operations in the bot
func calculateReferralRewards(bot *tgbotapi.BotAPI, db *gorm.DB, userID uint, amount float64, transactionType string) {
	// Get the user who made the transaction
	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		logError("Failed to get user for referral rewards: %v", err)
		return
	}

	// Only process if user has a referrer
	if user.ReferrerID == nil {
		return
	}

	// CRITICAL: Only process referral rewards for TRADES
	// Deposits and withdrawals do NOT generate referral rewards
	if transactionType != "trade" {
		logDebug("Skipping referral rewards for %s - only trades generate rewards", transactionType)
		return
	}

	// Level 1 Referrer (Direct referrer)
	var referrer1 models.User
	if err := db.First(&referrer1, *user.ReferrerID).Error; err == nil {
		// Check if referrer has 20+ direct referrals for special plan
		var count int64
		db.Model(&models.User{}).Where("referrer_id = ? AND registered = ?", referrer1.ID, true).Count(&count)

		// Set commission percentage based on plan
		percent := 0.5 // Default 0.5%
		if count >= 20 {
			percent = 0.6 // Special plan: 0.6%
			if !referrer1.PlanUpgradedNotified {
				bot.Send(tgbotapi.NewMessage(referrer1.TelegramID, "🏆 تبریک! شما به خاطر داشتن ۲۰ زیرمجموعه فعال، درصد پاداش Level 1 شما به ۰.۶٪ افزایش یافت."))
				referrer1.PlanUpgradedNotified = true
			}
		}

		// Calculate and add reward
		reward1 := amount * percent / 100
		referrer1.ReferralReward += reward1
		db.Save(&referrer1)

		// Send notification to referrer
		// Note: At this point we know it's a trade because we checked above
		actionText := "معامله"

		bot.Send(tgbotapi.NewMessage(referrer1.TelegramID,
			fmt.Sprintf("🎉 شما به خاطر %s زیرمجموعه‌تان %s مبلغ %.4f USDT پاداش گرفتید!",
				actionText, user.FullName, reward1)))

		// Level 2 Referrer (Indirect referrer)
		if referrer1.ReferrerID != nil {
			var referrer2 models.User
			if err := db.First(&referrer2, *referrer1.ReferrerID).Error; err == nil {
				reward2 := amount * 0.25 / 100 // 0.25% for level 2
				referrer2.ReferralReward += reward2
				db.Save(&referrer2)

				// Send notification to level 2 referrer
				bot.Send(tgbotapi.NewMessage(referrer2.TelegramID,
					fmt.Sprintf("🎉 شما به خاطر %s زیرمجموعه غیرمستقیم %s مبلغ %.4f USDT پاداش گرفتید!",
						actionText, user.FullName, reward2)))
			}
		}
	}
}

// ProcessReferralRewardsForDeposits processes referral rewards for all existing deposits
// NOTE: This function is currently DISABLED because referral rewards are only given for TRADES
// Deposits and withdrawals do NOT generate referral rewards
func ProcessReferralRewardsForDeposits(bot *tgbotapi.BotAPI, db *gorm.DB) {
	logInfo("Processing referral rewards for existing deposits...")

	// Get all confirmed deposits that haven't had referral rewards processed
	var deposits []models.Transaction
	err := db.Where("type = ? AND status = ? AND network IN (?)", "deposit", "confirmed", []string{"ERC20", "BEP20"}).Find(&deposits).Error
	if err != nil {
		logError("Failed to get deposits for referral processing: %v", err)
		return
	}

	processedCount := 0
	for _, deposit := range deposits {
		// DISABLED: پاداش رفرال فقط برای تریدها پرداخت می‌شود، نه برای واریز
		// calculateReferralRewards(bot, db, deposit.UserID, deposit.Amount, "deposit")
		processedCount++
	}

	logInfo("Processed referral rewards for %d deposits (DISABLED - only trades generate rewards)", processedCount)
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

					// بررسی اعتبار درصدها
					if minPercent > maxPercent {
						bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ حداقل درصد نمی‌تواند از حداکثر درصد بیشتر باشد!"))
						continue
					}

					if minPercent < -50 || maxPercent > 100 {
						bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "⚠️ درصدها باید بین -50% تا +100% باشند!"))
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
					// پیام بهتر برای تنظیم رنج‌های ترید
					var riskLevel string
					var riskEmoji string

					if minPercent >= 0 {
						riskLevel = "کم‌ریسک"
						riskEmoji = "🟢"
					} else if minPercent >= -10 {
						riskLevel = "متوسط"
						riskEmoji = "🟡"
					} else {
						riskLevel = "پرریسک"
						riskEmoji = "🔴"
					}

					msg := fmt.Sprintf("%s *رنج معامله %d تنظیم شد*\n\n"+
						"📊 *بازه درصد:* %.1f%% تا %.1f%%\n"+
						"⚠️ *سطح ریسک:* %s\n"+
						"💡 *توضیحات:*\n"+
						"• حداقل سود: %.1f%%\n"+
						"• حداکثر ضرر: %.1f%%\n\n"+
						"✅ تنظیمات با موفقیت ذخیره شد!",
						riskEmoji, tradeIndex, minPercent, maxPercent, riskLevel,
						maxPercent, -minPercent)

					message := tgbotapi.NewMessage(update.Message.Chat.ID, msg)
					message.ParseMode = "Markdown"
					bot.Send(message)
				} else {
					helpMsg := "❌ *فرمت دستور اشتباه!*\n\n" +
						"📝 *فرمت صحیح:*\n" +
						"`/settrade [شماره معامله] [حداقل درصد] [حداکثر درصد]`\n\n" +
						"💡 *مثال‌ها:*\n" +
						"• `/settrade 1 -5 15` - معامله ۱: -۵٪ تا +۱۵٪\n" +
						"• `/settrade 2 -8 20` - معامله ۲: -۸٪ تا +۲۰٪\n" +
						"• `/settrade 3 -10 25` - معامله ۳: -۱۰٪ تا +۲۵٪\n\n" +
						"⚠️ *نکات مهم:*\n" +
						"• حداقل درصد باید از حداکثر کمتر باشد\n" +
						"• درصدها بین -۵۰٪ تا +۱۰۰٪ باشند\n" +
						"• برای مشاهده رنج‌های فعلی: `/trades`"

					message := tgbotapi.NewMessage(update.Message.Chat.ID, helpMsg)
					message.ParseMode = "Markdown"
					bot.Send(message)
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
					helpMsg := "❌ *فرمت دستور اشتباه!*\n\n" +
						"📝 *فرمت صحیح:*\n" +
						"`/setrate [ارز] [نرخ به تومان]`\n\n" +
						"💡 *مثال‌ها:*\n" +
						"• `/setrate USDT 58500` - نرخ تتر: ۵۸,۵۰۰ تومان\n" +
						"• `/setrate BTC 2500000000` - نرخ بیت‌کوین: ۲,۵۰۰,۰۰۰,۰۰۰ تومان\n" +
						"• `/setrate ETH 150000000` - نرخ اتریوم: ۱۵۰,۰۰۰,۰۰۰ تومان\n\n" +
						"⚠️ *نکات مهم:*\n" +
						"• نرخ باید عدد مثبت باشد\n" +
						"• برای مشاهده نرخ‌های فعلی: `/rates`"

					message := tgbotapi.NewMessage(update.Message.Chat.ID, helpMsg)
					message.ParseMode = "Markdown"
					bot.Send(message)
				}
				continue
			}
			if update.Message.Command() == "trades" {
				var tradeRanges []models.TradeRange
				db.Order("trade_index").Find(&tradeRanges)
				if len(tradeRanges) == 0 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😔 هنوز هیچ رنج تریدی تنظیم نشده! \n\nبرای تنظیم رنج از دستور `/settrade` استفاده کن 👆"))
					continue
				}
				tradeMsg := "📊 *رنج‌های فعلی ترید*\n\n"
				tradeMsg += "معامله    حداقل    حداکثر    ریسک\n"
				tradeMsg += "----------------------------------------\n"
				for _, tr := range tradeRanges {
					var riskEmoji string
					if tr.MinPercent >= 0 {
						riskEmoji = "🟢"
					} else if tr.MinPercent >= -10 {
						riskEmoji = "🟡"
					} else {
						riskEmoji = "🔴"
					}
					tradeMsg += fmt.Sprintf("%-8s %+6.1f%%   %+6.1f%%   %s\n",
						fmt.Sprintf("#%d", tr.TradeIndex), tr.MinPercent, tr.MaxPercent, riskEmoji)
				}
				tradeMsg += "\n💡 *توضیحات:*\n"
				tradeMsg += "🟢 کم‌ریسک (فقط سود)\n"
				tradeMsg += "🟡 متوسط (سود و ضرر محدود)\n"
				tradeMsg += "🔴 پرریسک (ضرر احتمالی بالا)\n\n"
				tradeMsg += "✏️ برای تغییر رنج هر معامله، از دستور `/settrade [شماره] [حداقل] [حداکثر]` استفاده کن."
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, tradeMsg)
				msg.ParseMode = "Markdown"
				bot.Send(msg)
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
			if update.Message.Command() == "addusdt" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 2 {
					helpMsg := "❌ *فرمت دستور اشتباه!*\n\n" +
						"📝 *فرمت صحیح:*\n" +
						"`/addusdt [USER_ID] [AMOUNT]`\n\n" +
						"💡 *مثال:*\n" +
						"• `/addusdt 123456789 100` - افزایش موجودی USDT کاربر ۱۲۳۴۵۶۷۸۹ به میزان ۱۰۰ USDT\n\n" +
						"⚠️ *نکات مهم:*\n" +
						"• USER_ID باید عدد باشد\n" +
						"• AMOUNT باید عدد مثبت باشد"

					message := tgbotapi.NewMessage(update.Message.Chat.ID, helpMsg)
					message.ParseMode = "Markdown"
					bot.Send(message)
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
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("✅ *انجام شد!* \n\n🎉 موجودی USDT کاربر *%s* (آیدی: `%d`) به میزان *%.2f USDT* افزایش یافت.", user.FullName, user.TelegramID, amount)))
				continue
			}
			if update.Message.Command() == "subusdt" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 2 {
					helpMsg := "❌ *فرمت دستور اشتباه!*\n\n" +
						"📝 *فرمت صحیح:*\n" +
						"`/subusdt [USER_ID] [AMOUNT]`\n\n" +
						"💡 *مثال:*\n" +
						"• `/subusdt 123456789 50` - کاهش موجودی USDT کاربر ۱۲۳۴۵۶۷۸۹ به میزان ۵۰ USDT\n\n" +
						"⚠️ *نکات مهم:*\n" +
						"• USER_ID باید عدد باشد\n" +
						"• AMOUNT باید عدد مثبت باشد"

					message := tgbotapi.NewMessage(update.Message.Chat.ID, helpMsg)
					message.ParseMode = "Markdown"
					bot.Send(message)
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
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("✅ *انجام شد!* \n\n📉 موجودی USDT کاربر *%s* (آیدی: `%d`) به میزان *%.2f USDT* کاهش یافت.", user.FullName, user.TelegramID, amount)))
				continue
			}
			if update.Message.Command() == "setusdt" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 2 {
					helpMsg := "❌ *فرمت دستور اشتباه!*\n\n" +
						"📝 *فرمت صحیح:*\n" +
						"`/setusdt [USER_ID] [AMOUNT]`\n\n" +
						"💡 *مثال:*\n" +
						"• `/setusdt 123456789 200` - تنظیم موجودی USDT کاربر ۱۲۳۴۵۶۷۸۹ به میزان ۲۰۰ USDT\n\n" +
						"⚠️ *نکات مهم:*\n" +
						"• USER_ID باید عدد باشد\n" +
						"• AMOUNT باید عدد مثبت باشد"

					message := tgbotapi.NewMessage(update.Message.Chat.ID, helpMsg)
					message.ParseMode = "Markdown"
					bot.Send(message)
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
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("✅ *تمام!* \n\n🎯 موجودی USDT کاربر *%s* (آیدی: `%d`) روی *%.2f USDT* تنظیم شد.", user.FullName, user.TelegramID, amount)))
				continue
			}
			if update.Message.Command() == "addtoman" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 2 {
					helpMsg := "❌ *فرمت دستور اشتباه!*\n\n" +
						"📝 *فرمت صحیح:*\n" +
						"`/addtoman [USER_ID] [AMOUNT]`\n\n" +
						"💡 *مثال:*\n" +
						"• `/addtoman 123456789 1000000` - افزایش موجودی تومانی کاربر ۱۲۳۴۵۶۷۸۹ به میزان ۱,۰۰۰,۰۰۰ تومان\n\n" +
						"⚠️ *نکات مهم:*\n" +
						"• USER_ID باید عدد باشد\n" +
						"• AMOUNT باید عدد مثبت باشد (تومان)"

					message := tgbotapi.NewMessage(update.Message.Chat.ID, helpMsg)
					message.ParseMode = "Markdown"
					bot.Send(message)
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
				user.TomanBalance += amount
				db.Save(user)
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("✅ *انجام شد!* \n\n🎉 موجودی تومانی کاربر *%s* (آیدی: `%d`) به میزان *%s تومان* افزایش یافت.", user.FullName, user.TelegramID, formatToman(amount))))
				continue
			}
			if update.Message.Command() == "subtoman" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 2 {
					helpMsg := "❌ *فرمت دستور اشتباه!*\n\n" +
						"📝 *فرمت صحیح:*\n" +
						"`/subtoman [USER_ID] [AMOUNT]`\n\n" +
						"💡 *مثال:*\n" +
						"• `/subtoman 123456789 500000` - کاهش موجودی تومانی کاربر ۱۲۳۴۵۶۷۸۹ به میزان ۵۰۰,۰۰۰ تومان\n\n" +
						"⚠️ *نکات مهم:*\n" +
						"• USER_ID باید عدد باشد\n" +
						"• AMOUNT باید عدد مثبت باشد (تومان)"

					message := tgbotapi.NewMessage(update.Message.Chat.ID, helpMsg)
					message.ParseMode = "Markdown"
					bot.Send(message)
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
				if user.TomanBalance < amount {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "😬  موجودی تومانی کافی نیست."))
					continue
				}
				user.TomanBalance -= amount
				db.Save(user)
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("✅ *انجام شد!* \n\n📉 موجودی تومانی کاربر *%s* (آیدی: `%d`) به میزان *%s تومان* کاهش یافت.", user.FullName, user.TelegramID, formatToman(amount))))
				continue
			}
			if update.Message.Command() == "settoman" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 2 {
					helpMsg := "❌ *فرمت دستور اشتباه!*\n\n" +
						"📝 *فرمت صحیح:*\n" +
						"`/settoman [USER_ID] [AMOUNT]`\n\n" +
						"💡 *مثال:*\n" +
						"• `/settoman 123456789 2000000` - تنظیم موجودی تومانی کاربر ۱۲۳۴۵۶۷۸۹ به میزان ۲,۰۰۰,۰۰۰ تومان\n\n" +
						"⚠️ *نکات مهم:*\n" +
						"• USER_ID باید عدد باشد\n" +
						"• AMOUNT باید عدد مثبت باشد (تومان)"

					message := tgbotapi.NewMessage(update.Message.Chat.ID, helpMsg)
					message.ParseMode = "Markdown"
					bot.Send(message)
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
				user.TomanBalance = amount
				db.Save(user)
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("✅ *تمام!* \n\n🎯 موجودی تومانی کاربر *%s* (آیدی: `%d`) روی *%s تومان* تنظیم شد.", user.FullName, user.TelegramID, formatToman(amount))))
				continue
			}
			if update.Message.Command() == "userinfo" {
				args := strings.Fields(update.Message.CommandArguments())
				if len(args) != 1 {
					helpMsg := "❌ *فرمت دستور اشتباه!*\n\n" +
						"📝 *فرمت صحیح:*\n" +
						"`/userinfo [USER_ID]`\n\n" +
						"💡 *مثال:*\n" +
						"• `/userinfo 123456789` - نمایش اطلاعات کاربر با شناسه ۱۲۳۴۵۶۷۸۹\n\n" +
						"⚠️ *نکات مهم:*\n" +
						"• USER_ID باید عدد باشد"

					message := tgbotapi.NewMessage(update.Message.Chat.ID, helpMsg)
					message.ParseMode = "Markdown"
					bot.Send(message)
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
			if update.Message.Command() == "backup" || update.Message.Command() == "simplebackup" {
				// اجرای بکاپ دیتابیس و ارسال فایل به ادمین
				go func(chatID int64) {
					bot.Send(tgbotapi.NewMessage(chatID, "⏳ صبر کن، دارم فایل بکاپ رو آماده می‌کنم..."))

					user := cfg.MySQL.User
					pass := cfg.MySQL.Password
					dbName := cfg.MySQL.DBName
					host := cfg.MySQL.Host
					port := cfg.MySQL.Port

					// اگر host خالی باشه، default رو بذار
					if host == "" {
						host = "localhost"
					}
					if port == 0 {
						port = 3306
					}

					backupFile := filepath.Join(os.TempDir(), fmt.Sprintf("backup_%d.sql", time.Now().Unix()))
					var output []byte
					var err error

					// اگه simplebackup باشه، مستقیماً Go backup استفاده کن
					if update.Message.Command() == "simplebackup" {
						logInfo("Using Go-based backup (simplebackup command)")
						bot.Send(tgbotapi.NewMessage(chatID, "🔄 استفاده از backup داخلی Go..."))
						output, err = createGoBackup(db, dbName)
					} else {
						// ساخت کامند بدون password در command line
						var cmd *exec.Cmd
						if pass != "" {
							// استفاده از environment variable برای password
							cmd = exec.Command("mysqldump",
								"--user="+user,
								"--host="+host,
								"--port="+fmt.Sprintf("%d", port),
								"--single-transaction",
								"--routines",
								"--triggers",
								dbName)
							cmd.Env = append(os.Environ(), "MYSQL_PWD="+pass)
						} else {
							// اگه password نداره
							cmd = exec.Command("mysqldump",
								"--user="+user,
								"--host="+host,
								"--port="+fmt.Sprintf("%d", port),
								"--single-transaction",
								"--routines",
								"--triggers",
								dbName)
						}

						// گرفتن output
						output, err = cmd.Output()
						if err != nil {
							// اگه mysqldump کار نکرد، از Go backup استفاده کن
							logInfo("mysqldump failed, trying Go-based backup...")
							bot.Send(tgbotapi.NewMessage(chatID, "⚠️ mysqldump مشکل داره! از روش جایگزین استفاده می‌کنم..."))

							output, err = createGoBackup(db, dbName)
							if err != nil {
								bot.Send(tgbotapi.NewMessage(chatID, "😞 هر دو روش بک‌اپ شکست خورد: "+err.Error()))
								return
							}
						}
					}

					if err != nil {
						bot.Send(tgbotapi.NewMessage(chatID, "😞 متاسفانه مشکلی پیش اومد: "+err.Error()))
						return
					}

					// نوشتن فایل
					err = os.WriteFile(backupFile, output, 0644)
					if err != nil {
						bot.Send(tgbotapi.NewMessage(chatID, "😞 مشکل در نوشتن فایل: "+err.Error()))
						return
					}

					// چک کردن اندازه فایل
					fileInfo, err := os.Stat(backupFile)
					if err != nil {
						bot.Send(tgbotapi.NewMessage(chatID, "😞 مشکل در خواندن اطلاعات فایل: "+err.Error()))
						return
					}

					if fileInfo.Size() == 0 {
						bot.Send(tgbotapi.NewMessage(chatID, "😞 فایل بکاپ خالی است! احتمالاً مشکلی در دیتابیس وجود دارد."))
						os.Remove(backupFile)
						return
					}

					// ارسال فایل
					file := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(backupFile))
					file.Caption = fmt.Sprintf("📦 فایل بکاپ آماده!\n📊 اندازه: %.2f KB", float64(fileInfo.Size())/1024)
					_, err = bot.Send(file)
					if err != nil {
						bot.Send(tgbotapi.NewMessage(chatID, "😞 مشکل در ارسال فایل: "+err.Error()))
					}

					// پاک کردن فایل بعد از ارسال
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
						// اگر رنج ترید تنظیم نشده، از مقادیر پیش‌فرض استفاده کن
						bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⚠️ رنج درصد تنظیم نشده! از مقادیر پیش‌فرض استفاده می‌شود."))

						// ایجاد رنج پیش‌فرض برای این معامله
						switch tradeIndex {
						case 1:
							tr = models.TradeRange{TradeIndex: 1, MinPercent: -5.0, MaxPercent: 15.0}
						case 2:
							tr = models.TradeRange{TradeIndex: 2, MinPercent: -8.0, MaxPercent: 20.0}
						case 3:
							tr = models.TradeRange{TradeIndex: 3, MinPercent: -10.0, MaxPercent: 25.0}
						}

						// ذخیره رنج پیش‌فرض در دیتابیس
						if err := db.Create(&tr).Error; err != nil {
							log.Printf("❌ Failed to create default trade range %d: %v", tradeIndex, err)
						} else {
							log.Printf("✅ Created default trade range %d for user %d: %.1f%% to %.1f%%",
								tradeIndex, tx.UserID, tr.MinPercent, tr.MaxPercent)
						}
					}

					// تولید درصد رندوم در بازه
					percent := tr.MinPercent + rand.Float64()*(tr.MaxPercent-tr.MinPercent)

					// لاگ کردن اطلاعات ترید برای دیباگ
					log.Printf("🎯 Trade %d for user %d: range %.1f%% to %.1f%%, generated: %.2f%%",
						tradeIndex, tx.UserID, tr.MinPercent, tr.MaxPercent, percent)
					// محاسبه مبلغ جدید
					var lastAmount float64 = tx.Amount
					var lastTrade models.TradeResult
					db.Where("transaction_id = ? AND user_id = ?", tx.ID, tx.UserID).Order("trade_index desc").First(&lastTrade)
					if lastTrade.ID != 0 {
						lastAmount = lastTrade.ResultAmount
					}
					resultAmount := lastAmount * (1 + percent/100)

					// لاگ کردن محاسبات برای دیباگ
					log.Printf("💰 Trade calculation: lastAmount=%.2f, percent=%.2f%%, resultAmount=%.2f",
						lastAmount, percent, resultAmount)

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
							adminMsg := fmt.Sprintf("⚠️ کاربر %s (ID: %d) در معامله %d %s به مقدار %.2f USDT ضرر کرد.\n\n"+
								"📊 جزئیات معامله:\n"+
								"• درصد: %.2f%%\n"+
								"• مبلغ اولیه: %.2f USDT\n"+
								"• مبلغ نهایی: %.2f USDT\n"+
								"• ضرر: %.2f USDT\n\n"+
								"💳 عملیات مورد نیاز:\n"+
								"لطفاً %.2f USDT را از ولت %s کاربر (%s) کسر و به ولت صرافی منتقل کن.",
								user.FullName, user.TelegramID, tradeIndex, network, loss,
								percent, lastAmount, resultAmount, loss, deducted, network, walletAddr)
							sendToAllAdmins(bot, adminMsg)
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
							adminMsg := fmt.Sprintf("🎉 کاربر %s (ID: %d) در معامله %d %s %.2f USDT سود کرد!\n\n"+
								"📊 جزئیات معامله:\n"+
								"• درصد: %.2f%%\n"+
								"• مبلغ اولیه: %.2f USDT\n"+
								"• مبلغ نهایی: %.2f USDT\n"+
								"• سود: %.2f USDT\n\n"+
								"💳 آدرس ولت کاربر: %s",
								user.FullName, user.TelegramID, tradeIndex, network, profit,
								percent, lastAmount, resultAmount, profit, walletAddr)
							sendToAllAdmins(bot, adminMsg)
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

					// محاسبه پاداش رفرال برای معامله
					// IMPORTANT: Referral rewards are ONLY given for TRADES
					// Deposits and withdrawals do NOT generate referral rewards
					calculateReferralRewards(bot, db, tx.UserID, lastAmount, "trade")
					// پیام به کاربر: بعد از ۳۰ دقیقه نتیجه را ارسال کن
					go func(chatID int64, amount float64, percent float64, resultAmount float64, tradeIndex int) {
						time.Sleep(30 * time.Minute)

						// پیام بهتر با جزئیات بیشتر
						var resultEmoji string
						var resultText string
						if percent > 0 {
							resultEmoji = "🟢"
							resultText = "سود"
						} else if percent < 0 {
							resultEmoji = "🔴"
							resultText = "ضرر"
						} else {
							resultEmoji = "🟡"
							resultText = "بدون تغییر"
						}

						msg := fmt.Sprintf("%s *نتیجه معامله %d شما*\n\n"+
							"💰 مبلغ اولیه: %.2f USDT\n"+
							"📊 درصد تغییر: %+.2f%% (%s)\n"+
							"💵 مبلغ جدید: %.2f USDT\n\n"+
							"⏰ زمان: %s",
							resultEmoji, tradeIndex, amount, percent, resultText, resultAmount,
							time.Now().Format("15:04"))

						message := tgbotapi.NewMessage(chatID, msg)
						message.ParseMode = "Markdown"
						bot.Send(message)
					}(update.CallbackQuery.From.ID, lastAmount, percent, resultAmount, tradeIndex)
					// پیام بهتر برای کاربر
					var tradeEmoji string
					if percent > 0 {
						tradeEmoji = "🚀"
					} else if percent < 0 {
						tradeEmoji = "📉"
					} else {
						tradeEmoji = "➡️"
					}

					callbackMsg := fmt.Sprintf("%s *درخواست معامله %d ثبت شد!*\n\n"+
						"⏰ نتیجه تا ۳۰ دقیقه دیگر اعلام می‌شود\n"+
						"📊 رنج درصد: %.1f%% تا %.1f%%\n"+
						"💡 برای مشاهده نتایج: `/trades %d`",
						tradeEmoji, tradeIndex, tr.MinPercent, tr.MaxPercent, tx.ID)

					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, callbackMsg))
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
					msg := "📊 *نتایج معاملات این واریز:*\n\n"
					var totalProfit float64
					var initialAmount float64

					for i, t := range trades {
						if i == 0 {
							// برای معامله اول، مبلغ اولیه را از تراکنش اصلی بگیر
							var tx models.Transaction
							if err := db.First(&tx, t.TransactionID).Error; err == nil {
								initialAmount = tx.Amount
							}
						}

						var emoji string
						if t.Percent > 0 {
							emoji = "🟢"
						} else if t.Percent < 0 {
							emoji = "🔴"
						} else {
							emoji = "🟡"
						}

						msg += fmt.Sprintf("%s *معامله %d:* %+.2f%% → %.2f USDT\n",
							emoji, t.TradeIndex, t.Percent, t.ResultAmount)

						// محاسبه سود/ضرر کل
						if i == 0 {
							totalProfit = t.ResultAmount - initialAmount
						} else {
							var prevTrade models.TradeResult
							if err := db.Where("transaction_id = ? AND trade_index = ?", t.TransactionID, t.TradeIndex-1).First(&prevTrade).Error; err == nil {
								totalProfit += t.ResultAmount - prevTrade.ResultAmount
							}
						}
					}

					msg += "\n📈 *خلاصه کلی:*\n"
					msg += fmt.Sprintf("💰 مبلغ اولیه: %.2f USDT\n", initialAmount)
					msg += fmt.Sprintf("💵 مبلغ نهایی: %.2f USDT\n", trades[len(trades)-1].ResultAmount)

					var totalEmoji string
					if totalProfit > 0 {
						totalEmoji = "🟢"
						msg += fmt.Sprintf("%s سود کل: +%.2f USDT", totalEmoji, totalProfit)
					} else if totalProfit < 0 {
						totalEmoji = "🔴"
						msg += fmt.Sprintf("%s ضرر کل: %.2f USDT", totalEmoji, totalProfit)
					} else {
						totalEmoji = "🟡"
						msg += fmt.Sprintf("%s بدون تغییر", totalEmoji)
					}

					message := tgbotapi.NewMessage(update.Message.Chat.ID, msg)
					message.ParseMode = "Markdown"
					bot.Send(message)
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

				// Handle user details callbacks
				if strings.HasPrefix(data, "user_details_") {
					userIDstr := strings.TrimPrefix(data, "user_details_")
					userIDint, err := strconv.Atoi(userIDstr)
					if err == nil {
						// Show user details
						handleUserDetails(bot, db, update.CallbackQuery.Message.Chat.ID, int64(userIDint))
						bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "جزئیات کاربر نمایش داده شد"))
						continue
					}
				}

				// Handle search callbacks
				if data == "search_by_name" {
					adminSearchState[userID] = "awaiting_name"
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "نام کاربر را وارد کنید"))
					bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "🔍 لطفاً نام کامل کاربر را وارد کنید:"))
					continue
				}
				if data == "search_by_username" {
					adminSearchState[userID] = "awaiting_username"
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "یوزرنیم را وارد کنید"))
					bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "📱 لطفاً یوزرنیم کاربر را وارد کنید (بدون @):"))
					continue
				}
				if data == "search_by_telegram_id" {
					adminSearchState[userID] = "awaiting_telegram_id"
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "تلگرام ID را وارد کنید"))
					bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "🆔 لطفاً تلگرام ID کاربر را وارد کنید:"))
					continue
				}
				if data == "search_by_user_id" {
					adminSearchState[userID] = "awaiting_user_id"
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "User ID را وارد کنید"))
					bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "🔑 لطفاً User ID کاربر را وارد کنید:"))
					continue
				}
				if data == "filter_by_balance" {
					showBalanceFilterMenu(bot, db, update.CallbackQuery.Message.Chat.ID, userID)
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "فیلتر موجودی"))
					continue
				}
				if data == "filter_by_date" {
					showDateFilterMenu(bot, db, update.CallbackQuery.Message.Chat.ID, userID)
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "فیلتر تاریخ"))
					continue
				}
				if data == "filter_registered" {
					// Initialize filters map if it doesn't exist
					if adminSearchFilters[userID] == nil {
						adminSearchFilters[userID] = make(map[string]interface{})
					}
					adminSearchFilters[userID]["registered"] = true
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "فیلتر کاربران ثبت‌نام شده اعمال شد"))
					continue
				}
				if data == "filter_unregistered" {
					// Initialize filters map if it doesn't exist
					if adminSearchFilters[userID] == nil {
						adminSearchFilters[userID] = make(map[string]interface{})
					}
					adminSearchFilters[userID]["registered"] = false
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "فیلتر کاربران ناتمام اعمال شد"))
					continue
				}
				if data == "clear_filters" {
					adminSearchFilters[userID] = make(map[string]interface{})
					adminSearchState[userID] = "search_menu"
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "فیلترها پاک شدند"))
					showUserSearchMenu(bot, db, update.CallbackQuery.Message.Chat.ID, userID)
					continue
				}
				if data == "show_search_results" {
					showSearchResults(bot, db, update.CallbackQuery.Message.Chat.ID, userID, 0)
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "نتایج جستجو"))
					continue
				}
				if data == "back_to_admin" {
					adminSearchState[userID] = ""
					adminSearchFilters[userID] = nil
					showAdminMenu(bot, db, update.CallbackQuery.Message.Chat.ID)
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "بازگشت به پنل ادمین"))
					continue
				}
				if data == "balance_above" {
					adminSearchState[userID] = "awaiting_balance_min"
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "حداقل موجودی را وارد کنید"))
					bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "💰 لطفاً حداقل موجودی را وارد کنید (USDT):"))
					continue
				}
				if data == "balance_below" {
					adminSearchState[userID] = "awaiting_balance_max"
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "حداکثر موجودی را وارد کنید"))
					bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "💸 لطفاً حداکثر موجودی را وارد کنید (USDT):"))
					continue
				}
				if data == "balance_between" {
					adminSearchState[userID] = "awaiting_balance_min"
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "حداقل موجودی را وارد کنید"))
					bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "💰 لطفاً ابتدا حداقل موجودی را وارد کنید (USDT):"))
					continue
				}
				if data == "date_from" {
					adminSearchState[userID] = "awaiting_date_from"
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "تاریخ شروع را وارد کنید"))
					bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "📅 لطفاً تاریخ شروع را وارد کنید (YYYY-MM-DD):"))
					continue
				}
				if data == "date_to" {
					adminSearchState[userID] = "awaiting_date_to"
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "تاریخ پایان را وارد کنید"))
					bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "📅 لطفاً تاریخ پایان را وارد کنید (YYYY-MM-DD):"))
					continue
				}
				if data == "date_between" {
					adminSearchState[userID] = "awaiting_date_from"
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "تاریخ شروع را وارد کنید"))
					bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "📅 لطفاً ابتدا تاریخ شروع را وارد کنید (YYYY-MM-DD):"))
					continue
				}
				if data == "back_to_search" {
					showUserSearchMenu(bot, db, update.CallbackQuery.Message.Chat.ID, userID)
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "بازگشت به منوی جستجو"))
					continue
				}
				if strings.HasPrefix(data, "search_page_") {
					pageStr := strings.TrimPrefix(data, "search_page_")
					page, err := strconv.Atoi(pageStr)
					if err == nil {
						showSearchResults(bot, db, update.CallbackQuery.Message.Chat.ID, userID, page)
						bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, fmt.Sprintf("صفحه %d", page+1)))
						continue
					}
				}
				if data == "search_current_page" {
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "شما در این صفحه هستید"))
					continue
				}
				if data == "search_new" {
					adminSearchState[userID] = ""
					adminSearchFilters[userID] = nil
					showUserSearchMenu(bot, db, update.CallbackQuery.Message.Chat.ID, userID)
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "جستجوی جدید"))
					continue
				}
				if data == "search_close" {
					adminSearchState[userID] = ""
					adminSearchFilters[userID] = nil
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "جستجو بسته شد"))
					continue
				}
				if data == "cancel_search" {
					adminSearchState[userID] = ""
					adminSearchFilters[userID] = nil
					showAdminMenu(bot, db, update.CallbackQuery.Message.Chat.ID)
					bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "جستجو لغو شد"))
					continue
				}

				state := adminBroadcastState[userID]
				// مرحله 2: تایید اولیه درخواست (بدون کسر موجودی)
				if strings.HasPrefix(data, "approve_withdraw_") {
					txIDstr := strings.TrimPrefix(data, "approve_withdraw_")
					txID, _ := strconv.Atoi(txIDstr)
					var tx models.Transaction
					if err := db.First(&tx, txID).Error; err == nil && tx.Status == "pending" {
						var user models.User
						db.First(&user, tx.UserID)

						// مرحله 2: فقط تایید درخواست (بدون کسر موجودی)
						tx.Status = "approved"
						db.Save(&tx)

						// Get bank account info for toman withdrawals
						var bankMsg string
						if tx.Network == "TOMAN" && tx.BankAccountID != nil {
							var bankAccount models.BankAccount
							if err := db.First(&bankAccount, *tx.BankAccountID).Error; err == nil {
								bankName := bankAccount.BankName
								if bankName == "" {
									bankName = "نامشخص"
								}
								bankMsg = fmt.Sprintf("\n🏦 حساب انتخابی: %s\n📄 شبا: %s\n💳 کارت: %s",
									bankName, bankAccount.Sheba, bankAccount.CardNumber)
							}
						}

						// DISABLED: پاداش رفرال فقط برای تریدها پرداخت می‌شود، نه برای برداشت
						// Referral rewards are ONLY given for TRADES, not for withdrawals

						// پیام مرحله 2 به کاربر: "درخواست بررسی شد"
						var userMsg string
						if tx.Network == "TOMAN" {
							usdtRate, _ := getUSDTRate(db)
							tomanAmount := tx.Amount * usdtRate
							userMsg = fmt.Sprintf(`✅ <b>درخواست شما بررسی شد</b>

💵 <b>مبلغ:</b> %s تومان
💰 <b>معادل:</b> %.4f USDT

%s

📢 <b>درخواست برداشت تایید شد و به زودی به حساب بانکی شما واریز میشود</b>

⏳ منتظر اطلاع رسانی پرداخت باشید.`, formatToman(tomanAmount), tx.Amount, bankMsg)
						} else {
							userMsg = fmt.Sprintf("✅ درخواست برداشت %.4f USDT بررسی و تایید شد.", tx.Amount)
						}

						userMessage := tgbotapi.NewMessage(user.TelegramID, userMsg)
						userMessage.ParseMode = "HTML"
						bot.Send(userMessage)

						// آپدیت دکمه‌های ادمین - حالا فقط "پرداخت شد" نمایش می‌دهد
						adminBtns := tgbotapi.NewInlineKeyboardMarkup(
							tgbotapi.NewInlineKeyboardRow(
								tgbotapi.NewInlineKeyboardButtonData("💰 پرداخت شد", fmt.Sprintf("complete_withdraw_%d", tx.ID)),
							),
						)

						editMsg := tgbotapi.NewEditMessageReplyMarkup(
							update.CallbackQuery.Message.Chat.ID,
							update.CallbackQuery.Message.MessageID,
							adminBtns,
						)
						bot.Send(editMsg)

						bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "✅ تایید شد"))
					}
					continue
				}

				// مرحله 3: کسر موجودی و تکمیل پرداخت
				if strings.HasPrefix(data, "complete_withdraw_") {
					txIDstr := strings.TrimPrefix(data, "complete_withdraw_")
					txID, _ := strconv.Atoi(txIDstr)
					var tx models.Transaction
					if err := db.First(&tx, txID).Error; err == nil && tx.Status == "approved" {
						var user models.User
						db.First(&user, tx.UserID)
						amount := tx.Amount
						remaining := amount

						// کسر موجودی (همان منطق قبلی)
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

						// 4. کم کردن از موجودی تومانی (تبدیل به USDT)
						if remaining > 0 {
							// Get current rate for conversion
							currentRate, rateErr := getUSDTRate(db)
							if rateErr == nil {
								remainingToman := remaining * currentRate
								if user.TomanBalance >= remainingToman {
									user.TomanBalance -= remainingToman
									remaining = 0
								} else {
									remaining -= user.TomanBalance / currentRate
									user.TomanBalance = 0
								}
							}
						}

						if remaining > 0 {
							// موجودی کافی نیست
							bot.Send(tgbotapi.NewMessage(user.TelegramID, "❌ موجودی کافی برای برداشت وجود ندارد."))
							bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "موجودی کافی نیست"))
							continue
						}

						// تکمیل پرداخت
						db.Save(&user)
						tx.Status = "completed"
						db.Save(&tx)

						// پیام مرحله 3 به کاربر: "درخواست پرداخت شد"
						var userMsg string
						if tx.Network == "TOMAN" {
							usdtRate, _ := getUSDTRate(db)
							tomanAmount := tx.Amount * usdtRate
							userMsg = fmt.Sprintf(`🎉 <b>درخواست شما پرداخت شد</b>

💵 <b>مبلغ:</b> %s تومان
💰 <b>معادل:</b> %.4f USDT

✅ <b>درخواست برداشت شما کامل شد و به حساب شما پرداخت شد</b>

💡 مبلغ به حساب بانکی انتخابی شما واریز شده است.`, formatToman(tomanAmount), tx.Amount)
						} else {
							userMsg = fmt.Sprintf("🎉 درخواست برداشت %.4f USDT کامل شد و پرداخت شد.", tx.Amount)
						}

						userMessage := tgbotapi.NewMessage(user.TelegramID, userMsg)
						userMessage.ParseMode = "HTML"
						bot.Send(userMessage)

						// ویرایش پیام ادمین و اضافه کردن وضعیت "پرداخت شد"
						originalMsg := update.CallbackQuery.Message.Text
						updatedMsg := originalMsg + "\n\n✅ <b>وضعیت:</b> پرداخت کامل شد"

						editMsg := tgbotapi.NewEditMessageText(
							update.CallbackQuery.Message.Chat.ID,
							update.CallbackQuery.Message.MessageID,
							updatedMsg,
						)
						editMsg.ParseMode = "HTML"
						bot.Send(editMsg)

						bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "💰 پرداخت کامل شد"))
					}
					continue
				}

				// رد درخواست
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

						// پیام رد به کاربر
						var userMsg string
						if tx.Network == "TOMAN" {
							usdtRate, _ := getUSDTRate(db)
							tomanAmount := tx.Amount * usdtRate
							userMsg = fmt.Sprintf("❌ درخواست برداشت %s تومان (%.4f USDT) رد شد.", formatToman(tomanAmount), tx.Amount)
						} else {
							userMsg = fmt.Sprintf("❌ درخواست برداشت %.4f USDT رد شد.", tx.Amount)
						}

						userMessage := tgbotapi.NewMessage(user.TelegramID, userMsg)
						bot.Send(userMessage)

						// ویرایش پیام ادمین و اضافه کردن وضعیت "رد شد"
						originalMsg := update.CallbackQuery.Message.Text
						updatedMsg := originalMsg + "\n\n❌ <b>وضعیت:</b> درخواست رد شد"

						editMsg := tgbotapi.NewEditMessageText(
							update.CallbackQuery.Message.Chat.ID,
							update.CallbackQuery.Message.MessageID,
							updatedMsg,
						)
						editMsg.ParseMode = "HTML"
						bot.Send(editMsg)

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

		// Check if user is admin first - admins don't need to be registered
		if isAdmin(userID) {
			handleAdminMenu(bot, db, update.Message)
			continue
		}

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

		// Calculate total USDT balance (including all sources) + Toman equivalent
		totalUSDTBalance := user.ERC20Balance + user.BEP20Balance + user.TradeBalance + user.RewardBalance
		tomanEquivalentUSDT := user.TomanBalance / usdtRate
		totalAvailableUSDT := totalUSDTBalance + tomanEquivalentUSDT

		if totalAvailableUSDT < usdtAmount {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(`😔 <b>موجودی کمه !</b>

💰 <b>موجودی کل:</b> %.4f USDT (معادل %s تومان)
  • USDT: %.4f
  • تومان: %s (معادل %.4f USDT)

💸 <b>مقدار درخواستی:</b> %.4f USDT (معادل %s تومان)
📉 <b>کسری:</b> %.4f USDT (معادل %s تومان)

😊 یه مقدار کمتر انتخاب کن، یا اول موجودی رو شارژ کن!`,
				totalAvailableUSDT, formatToman(totalAvailableUSDT*usdtRate),
				totalUSDTBalance, formatToman(user.TomanBalance), tomanEquivalentUSDT,
				usdtAmount, formatToman(tomanAmount),
				usdtAmount-totalAvailableUSDT, formatToman((usdtAmount-totalAvailableUSDT)*usdtRate))))
			return true
		}

		// دریافت حساب‌های بانکی کاربر
		accounts, err := user.GetBankAccounts(db)
		if err != nil || len(accounts) == 0 {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, `😔 <b>هیچ حساب بانکی ندارید!</b>

برای برداشت، ابتدا باید یک حساب بانکی اضافه کنید.

🏦 از منو کیف پول > مدیریت حساب‌های بانکی > اضافه کردن حساب جدید استفاده کنید.`))
			clearRegState(userID)
			showWalletMenu(bot, db, msg.Chat.ID, userID)
			return true
		}

		// ذخیره اطلاعات برداشت برای مرحله بعد
		saveRegTemp(userID, "withdraw_toman_amount", fmt.Sprintf("%.2f", tomanAmount))
		saveRegTemp(userID, "withdraw_usdt_amount", fmt.Sprintf("%.6f", usdtAmount))
		saveRegTemp(userID, "withdraw_rate", fmt.Sprintf("%.2f", usdtRate))

		// تغییر state برای انتخاب حساب بانکی
		setRegState(userID, "withdraw_select_account")

		// نمایش لیست حساب‌های بانکی
		showBankAccountSelection(bot, db, msg.Chat.ID, userID, tomanAmount, usdtAmount, usdtRate)
		return true
	}

	// --- USDT to Toman Conversion State ---
	if state == "convert_usdt_amount" {
		if msg.Text == "❌ لغو تبدیل" {
			clearRegState(userID)
			showConversionMenu(bot, db, msg.Chat.ID, userID)
			return true
		}

		// Parse USDT amount
		usdtAmount, err := strconv.ParseFloat(msg.Text, 64)
		if err != nil || usdtAmount <= 0 {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😅 مبلغ رو درست وارد نکردی. \n\nفقط عدد بنویس، مثل: 10.5"))
			return true
		}

		logInfo("User %d wants to convert %.4f USDT", userID, usdtAmount)

		// Get user and check balance
		user, _ := getUserByTelegramID(db, userID)
		if user == nil {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ کاربر یافت نشد."))
			clearRegState(userID)
			return true
		}

		// Calculate total USDT balance
		totalUSDT := user.ERC20Balance + user.BEP20Balance + user.TradeBalance + user.RewardBalance

		logInfo("User %d has total %.4f USDT, wants to convert %.4f USDT", userID, totalUSDT, usdtAmount)

		if totalUSDT < usdtAmount {
			logInfo("User %d has insufficient balance: %.4f < %.4f", userID, totalUSDT, usdtAmount)
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(`😔 <b>موجودی کمه!</b>

💰 <b>موجودی فعلی:</b> %.4f USDT
💸 <b>مقدار درخواستی:</b> %.4f USDT
📉 <b>کسری:</b> %.4f USDT

😊 یه مقدار کمتر انتخاب کن!`,
				totalUSDT, usdtAmount, usdtAmount-totalUSDT)))
			return true
		}

		// Get USDT rate
		usdtRate, err := getUSDTRate(db)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 نرخ در دسترس نیست!"))
			clearRegState(userID)
			return true
		}

		// Convert USDT to Toman
		tomanAmount := usdtAmount * usdtRate

		// Deduct USDT from balances (priority: RewardBalance -> TradeBalance -> ERC20Balance -> BEP20Balance)
		remaining := usdtAmount
		logInfo("Starting deduction for user %d: %.4f USDT remaining", userID, remaining)

		// Step 1: RewardBalance
		if user.RewardBalance >= remaining {
			logInfo("Deducting %.4f from RewardBalance (%.4f available)", remaining, user.RewardBalance)
			user.RewardBalance -= remaining
			remaining = 0
		} else if user.RewardBalance > 0 {
			logInfo("Deducting all RewardBalance: %.4f", user.RewardBalance)
			remaining -= user.RewardBalance
			user.RewardBalance = 0
		}

		// Step 2: TradeBalance
		if remaining > 0 && user.TradeBalance >= remaining {
			logInfo("Deducting %.4f from TradeBalance (%.4f available)", remaining, user.TradeBalance)
			user.TradeBalance -= remaining
			remaining = 0
		} else if remaining > 0 && user.TradeBalance > 0 {
			logInfo("Deducting all TradeBalance: %.4f", user.TradeBalance)
			remaining -= user.TradeBalance
			user.TradeBalance = 0
		}

		// Step 3: ERC20Balance
		if remaining > 0 && user.ERC20Balance >= remaining {
			logInfo("Deducting %.4f from ERC20Balance (%.4f available)", remaining, user.ERC20Balance)
			user.ERC20Balance -= remaining
			remaining = 0
		} else if remaining > 0 && user.ERC20Balance > 0 {
			logInfo("Deducting all ERC20Balance: %.4f", user.ERC20Balance)
			remaining -= user.ERC20Balance
			user.ERC20Balance = 0
		}

		// Step 4: BEP20Balance
		if remaining > 0 && user.BEP20Balance >= remaining {
			logInfo("Deducting %.4f from BEP20Balance (%.4f available)", remaining, user.BEP20Balance)
			user.BEP20Balance -= remaining
			remaining = 0
		} else if remaining > 0 && user.BEP20Balance > 0 {
			logInfo("Deducting all BEP20Balance: %.4f", user.BEP20Balance)
			remaining -= user.BEP20Balance
			user.BEP20Balance = 0
		}

		logInfo("Deduction completed for user %d: %.4f USDT remaining (should be 0)", userID, remaining)

		// Add Toman amount to TomanBalance
		user.TomanBalance += tomanAmount

		// Log before save for debugging
		logInfo("Before save - User %d: ERC20=%.4f, BEP20=%.4f, Trade=%.4f, Reward=%.4f, Toman=%.2f",
			userID, user.ERC20Balance, user.BEP20Balance, user.TradeBalance, user.RewardBalance, user.TomanBalance)

		// Save user changes
		result := db.Save(user)
		if result.Error != nil {
			logError("Failed to save user %d after conversion: %v", userID, result.Error)
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 خطا در تبدیل ارز. لطفاً دوباره تلاش کنید."))
			clearRegState(userID)
			return true
		}

		logInfo("Successfully saved user %d after conversion", userID)

		// Reload user to verify save worked
		var savedUser models.User
		if err := db.First(&savedUser, user.ID).Error; err == nil {
			logInfo("After save verification - User %d: ERC20=%.4f, BEP20=%.4f, Trade=%.4f, Reward=%.4f, Toman=%.2f",
				userID, savedUser.ERC20Balance, savedUser.BEP20Balance, savedUser.TradeBalance, savedUser.RewardBalance, savedUser.TomanBalance)
		}

		// Create transaction record
		tx := models.Transaction{
			UserID:    user.ID,
			Type:      "conversion",
			Amount:    usdtAmount,
			Status:    "confirmed",
			Network:   "USDT_TO_TOMAN",
			CreatedAt: time.Now(),
		}
		db.Create(&tx)

		// Send success message
		successMsg := fmt.Sprintf(`🎉 <b>تبدیل موفقیت‌آمیز!</b>

✅ <b>تبدیل انجام شده:</b>
• USDT: <b>%.2f</b>
• تومان: <b>%s</b>
• نرخ: <b>%s تومان</b>

💰 <b>موجودی جدید تومانی:</b> <b>%s تومان</b>

💡 حالا می‌تونید از منوی کیف پول برداشت کنید!`,
			usdtAmount,
			formatToman(tomanAmount),
			formatToman(usdtRate),
			formatToman(user.TomanBalance))

		message := tgbotapi.NewMessage(msg.Chat.ID, successMsg)
		message.ParseMode = "HTML"
		bot.Send(message)

		clearRegState(userID)
		showConversionMenu(bot, db, msg.Chat.ID, userID)
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
				bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 متاسفانه مشکلی در ذخیره اطلاعات پیش اومد! لطفاً دوباره تلاش کن."))
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

	// --- Add New Bank Account States ---
	if state == "add_new_bank_sheba" {
		if msg.Text == "❌ لغو و بازگشت" {
			clearRegState(userID)
			showBankAccountsManagement(bot, db, msg.Chat.ID, userID)
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

🔄 یه بار دیگه امتحان کن! 😉`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "HTML"
			bot.Send(message)
			return true
		}

		// بررسی تکراری نبودن شبا
		user, err := getUserByTelegramID(db, userID)
		if err != nil || user == nil {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 متاسفانه مشکلی پیش اومد!"))
			clearRegState(userID)
			return true
		}

		if models.IsBankAccountExists(db, user.ID, msg.Text, "") {
			errorMsg := `⚠️ <b>شماره شبا تکراری!</b>

این شماره شبا قبلاً برای شما ثبت شده است.

🔍 لطفاً شماره شبا متفاوتی وارد کنید یا از منوی "📋 مشاهده همه حساب‌ها" اطلاعات موجود را بررسی کنید.

🔄 یه شماره شبا دیگه امتحان کن! 😊`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "HTML"
			bot.Send(message)
			return true
		}

		// Save new sheba, ask for card number
		saveRegTemp(userID, "new_sheba", msg.Text)
		setRegState(userID, "add_new_bank_card")

		cardMsg := `✅ <b>مرحله ۱ تکمیل شد!</b>

🏦 شماره شبا جدید: <code>%s</code>

📝 <b>مرحله ۲: شماره کارت</b>

لطفاً شماره کارت بانکی این حساب را وارد کنید:

💡 <b>مثال درست:</b> 6037998215325563

⚠️ <b>نکته‌های مهم:</b>
• حتماً ۱۶ تا رقم باشه
• هیچ فاصله یا خط تیره نذار
• فقط عدد بنویس
• حتماً شماره کارت همون حسابی که شباش رو دادی`

		message := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(cardMsg, msg.Text))
		message.ParseMode = "HTML"
		cancelKeyboard := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("❌ لغو و بازگشت"),
			),
		)
		cancelKeyboard.ResizeKeyboard = true
		message.ReplyMarkup = cancelKeyboard
		bot.Send(message)
		return true
	} else if state == "add_new_bank_card" {
		if msg.Text == "❌ لغو و بازگشت" {
			clearRegState(userID)
			showBankAccountsManagement(bot, db, msg.Chat.ID, userID)
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

🔄 الان دوباره تست کن! 🙂`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "HTML"
			bot.Send(message)
			return true
		}

		// بررسی تکراری نبودن کارت
		user, err := getUserByTelegramID(db, userID)
		if err != nil || user == nil {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 متاسفانه مشکلی پیش اومد!"))
			clearRegState(userID)
			return true
		}

		if models.IsBankAccountExists(db, user.ID, "", msg.Text) {
			errorMsg := `⚠️ <b>شماره کارت تکراری!</b>

این شماره کارت قبلاً برای شما ثبت شده است.

🔍 لطفاً شماره کارت متفاوتی وارد کنید یا از منوی "📋 مشاهده همه حساب‌ها" اطلاعات موجود را بررسی کنید.

🔄 یه شماره کارت دیگه امتحان کن! 😊`

			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "HTML"
			bot.Send(message)
			return true
		}

		// Save new card number, ask for bank name (optional)
		saveRegTemp(userID, "new_card", msg.Text)
		setRegState(userID, "add_new_bank_name")

		regTemp.RLock()
		info := regTemp.m[userID]
		regTemp.RUnlock()

		bankNameMsg := fmt.Sprintf(`✅ <b>مرحله ۲ تکمیل شد!</b>

💳 شماره کارت: <code>%s</code>

📝 <b>مرحله ۳: نام بانک (اختیاری)</b>

لطفاً نام بانک این حساب را وارد کنید یا دکمه "رد کردن" را بزنید:

💡 <b>مثال‌ها:</b> ملی، صادرات، پارسیان، پاسارگاد

⚠️ این فیلد اختیاری است و فقط برای شناسایی آسان‌تر حساب‌ها استفاده می‌شود.`,
			info["new_card"])

		keyboard := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("⏭️ رد کردن و ادامه"),
			),
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("❌ لغو و بازگشت"),
			),
		)
		keyboard.ResizeKeyboard = true

		message := tgbotapi.NewMessage(msg.Chat.ID, bankNameMsg)
		message.ParseMode = "HTML"
		message.ReplyMarkup = keyboard
		bot.Send(message)
		return true
	} else if state == "add_new_bank_name" {
		if msg.Text == "❌ لغو و بازگشت" {
			clearRegState(userID)
			showBankAccountsManagement(bot, db, msg.Chat.ID, userID)
			return true
		}

		bankName := ""
		if msg.Text != "⏭️ رد کردن و ادامه" {
			bankName = strings.TrimSpace(msg.Text)
			// Validate bank name length
			if len(bankName) > 100 {
				bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😅 نام بانک خیلی طولانیه! حداکثر ۱۰۰ کاراکتر مجاز است."))
				return true
			}
		}

		// Save bank name and show confirmation
		saveRegTemp(userID, "new_bank_name", bankName)
		setRegState(userID, "add_new_bank_confirm")

		regTemp.RLock()
		info := regTemp.m[userID]
		regTemp.RUnlock()

		bankNameDisplay := info["new_bank_name"]
		if bankNameDisplay == "" {
			bankNameDisplay = "نامشخص"
		}

		confirmMsg := fmt.Sprintf(`✅ <b>تایید نهایی حساب جدید</b>

📋 <b>اطلاعات حساب جدید:</b>
• شماره شبا: <code>%s</code>
• شماره کارت: <code>%s</code>
• نام بانک: %s

⚠️ <b>نکات مهم:</b>
• این حساب به لیست حساب‌های شما اضافه خواهد شد
• می‌توانید بعداً آن را به عنوان پیش‌فرض تنظیم کنید
• شماره شبا و کارت باید از یک حساب/کارت واحد باشند

✅ اگر اطلاعات درست است، دکمه تایید را بزنید.`,
			info["new_sheba"], info["new_card"], bankNameDisplay)

		keyboard := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("✅ تایید و ذخیره حساب"),
			),
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("❌ لغو و بازگشت"),
			),
		)
		keyboard.ResizeKeyboard = true

		message := tgbotapi.NewMessage(msg.Chat.ID, confirmMsg)
		message.ParseMode = "HTML"
		message.ReplyMarkup = keyboard
		bot.Send(message)
		return true
	} else if state == "add_new_bank_confirm" {
		if msg.Text == "❌ لغو و بازگشت" {
			clearRegState(userID)
			showBankAccountsManagement(bot, db, msg.Chat.ID, userID)
			return true
		}

		if msg.Text == "✅ تایید و ذخیره حساب" {
			regTemp.RLock()
			info := regTemp.m[userID]
			regTemp.RUnlock()

			// Get user
			user, err := getUserByTelegramID(db, userID)
			if err != nil || user == nil {
				bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 متاسفانه مشکلی پیش اومد! با پشتیبانی تماس بگیر."))
				clearRegState(userID)
				return true
			}

			// دریافت حساب‌های موجود
			existingAccounts, err := user.GetBankAccounts(db)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 مشکلی در دریافت اطلاعات حساب‌ها پیش اومد!"))
				clearRegState(userID)
				return true
			}

			// تعیین اینکه آیا این اولین حساب است (پیش‌فرض شود)
			isDefault := len(existingAccounts) == 0

			// اضافه کردن حساب جدید
			newAccount, err := models.AddBankAccount(db, user.ID,
				info["new_sheba"],
				info["new_card"],
				info["new_bank_name"],
				isDefault)

			if err != nil {
				bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 متاسفانه مشکلی در ذخیره حساب پیش اومد! لطفاً دوباره تلاش کن."))
				clearRegState(userID)
				return true
			}

			clearRegState(userID)

			// پیام موفقیت
			bankNameDisplay := newAccount.BankName
			if bankNameDisplay == "" {
				bankNameDisplay = "نامشخص"
			}

			var successMsg string
			if isDefault {
				successMsg = fmt.Sprintf(`🎉 <b>اولین حساب بانکی با موفقیت اضافه شد!</b>

✅ <b>حساب پیش‌فرض شما:</b>
• شماره شبا: <code>%s</code>
• شماره کارت: <code>%s</code>
• نام بانک: %s

🚀 <b>تبریک!</b> حالا می‌توانید:
• برداشت کنید
• پاداش‌ها را دریافت کنید
• از تمام امکانات ربات استفاده کنید

💡 این حساب به عنوان پیش‌فرض تنظیم شد.`,
					newAccount.Sheba, newAccount.CardNumber, bankNameDisplay)
			} else {
				successMsg = fmt.Sprintf(`🎉 <b>حساب جدید با موفقیت اضافه شد!</b>

✅ <b>حساب جدید شما:</b>
• شماره شبا: <code>%s</code>
• شماره کارت: <code>%s</code>
• نام بانک: %s

💡 <b>نکات مهم:</b>
• حساب به لیست حساب‌های شما اضافه شد
• برای تنظیم به عنوان پیش‌فرض از منوی "🎯 تغییر حساب پیش‌فرض" استفاده کنید
• تعداد کل حساب‌های شما: %d`,
					newAccount.Sheba, newAccount.CardNumber, bankNameDisplay, len(existingAccounts)+1)
			}

			message := tgbotapi.NewMessage(msg.Chat.ID, successMsg)
			message.ParseMode = "HTML"
			bot.Send(message)

			// بازگشت به منوی مدیریت حساب‌ها
			showBankAccountsManagement(bot, db, msg.Chat.ID, userID)
			return true
		}

		// اگر هیچ گزینه معتبری انتخاب نشد
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😅 لطفاً یکی از گزینه‌های موجود را انتخاب کن!"))
		return true
	}

	// --- Withdraw Bank Account Selection State ---
	if state == "withdraw_select_account" {
		if msg.Text == "❌ لغو برداشت" {
			clearRegState(userID)
			showWalletMenu(bot, db, msg.Chat.ID, userID)
			return true
		}

		// بررسی انتخاب حساب
		if !strings.HasPrefix(msg.Text, "🏦 برداشت به حساب ") {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😅 لطفاً یکی از حساب‌های موجود را انتخاب کنید!"))
			return true
		}

		// استخراج شماره حساب
		accountNumStr := strings.TrimPrefix(msg.Text, "🏦 برداشت به حساب ")
		accountNum, err := strconv.Atoi(accountNumStr)
		if err != nil || accountNum <= 0 {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 شماره حساب نامعتبر است!"))
			return true
		}

		// دریافت اطلاعات ذخیره شده
		regTemp.RLock()
		info := regTemp.m[userID]
		regTemp.RUnlock()

		tomanAmount, _ := strconv.ParseFloat(info["withdraw_toman_amount"], 64)
		usdtAmount, _ := strconv.ParseFloat(info["withdraw_usdt_amount"], 64)

		// Get user and accounts
		user, err := getUserByTelegramID(db, userID)
		if err != nil || user == nil {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 متاسفانه مشکلی پیش اومد!"))
			clearRegState(userID)
			return true
		}

		// دریافت حساب‌های بانکی
		accounts, err := user.GetBankAccounts(db)
		if err != nil || len(accounts) < accountNum {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "😔 حساب مورد نظر یافت نشد!"))
			clearRegState(userID)
			return true
		}

		// انتخاب حساب (منطق 0-based)
		selectedAccount := accounts[accountNum-1]

		// Create pending transaction با BankAccountID
		tx := models.Transaction{
			UserID:        user.ID,
			Type:          "withdraw",
			Amount:        usdtAmount, // Store in USDT for internal consistency
			Status:        "pending",
			Network:       "TOMAN", // برای تشخیص برداشت تومانی
			BankAccountID: &selectedAccount.ID,
		}
		db.Create(&tx)

		// محاسبه موجودی کل
		totalUSDTBalance := user.ERC20Balance + user.BEP20Balance + user.TradeBalance + user.RewardBalance
		usdtRate, _ := getUSDTRate(db)
		tomanEquivalentUSDT := user.TomanBalance / usdtRate
		totalAvailableUSDT := totalUSDTBalance + tomanEquivalentUSDT

		// نمایش اطلاعات بانک انتخابی
		bankName := selectedAccount.BankName
		if bankName == "" {
			bankName = "نامشخص"
		}

		// پیام به ادمین
		adminMsg := fmt.Sprintf(`💸 <b>درخواست برداشت تومانی جدید</b>

👤 <b>کاربر:</b> %s (آیدی: <code>%d</code>)
💵 <b>مبلغ تومانی:</b> <b>%s تومان</b>
💰 <b>معادل USDT:</b> <b>%.4f USDT</b>
📊 <b>نرخ:</b> %s تومان

🏦 <b>حساب انتخابی کاربر:</b>
• بانک: %s
• شماره شبا: <code>%s</code>
• شماره کارت: <code>%s</code>
• وضعیت: %s

📋 <b>موجودی کاربر:</b>
• 🔵 ERC20: %.4f USDT
• 🟡 BEP20: %.4f USDT  
• 📈 ترید: %.4f USDT
• 🎁 پاداش: %.4f USDT
• 💰 تومان: %s (معادل %.4f USDT)
• 💎 مجموع: %.4f USDT

برای پرداخت <b>%s تومان</b> به حساب کاربر، یکی از دکمه‌های زیر را انتخاب کنید.`,
			user.FullName, user.TelegramID,
			formatToman(tomanAmount), usdtAmount, formatToman(usdtRate),
			bankName, selectedAccount.Sheba, selectedAccount.CardNumber,
			func() string {
				if selectedAccount.IsDefault {
					return "✅ پیش‌فرض"
				}
				return "🔘 معمولی"
			}(),
			user.ERC20Balance, user.BEP20Balance, user.TradeBalance, user.RewardBalance,
			formatToman(user.TomanBalance), tomanEquivalentUSDT, totalAvailableUSDT,
			formatToman(tomanAmount))

		adminBtns := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ تایید درخواست", fmt.Sprintf("approve_withdraw_%d", tx.ID)),
				tgbotapi.NewInlineKeyboardButtonData("❌ رد درخواست", fmt.Sprintf("reject_withdraw_%d", tx.ID)),
			),
		)
		sendToAllAdminsWithMarkup(bot, adminMsg, adminBtns)

		// پیام تایید به کاربر
		confirmMsg := fmt.Sprintf(`✅ <b>درخواست برداشت ثبت شد</b>

💵 <b>مبلغ:</b> %s تومان
💰 <b>معادل:</b> %.4f USDT
📊 <b>نرخ:</b> %s تومان

🏦 <b>حساب انتخابی:</b>
• بانک: %s
• شبا: %s***%s
• کارت: %s***%s

⏳ <b>وضعیت:</b> در انتظار تایید ادمین

💡 بعد از تایید ادمین، مبلغ به حساب انتخابی شما واریز خواهد شد.`,
			formatToman(tomanAmount), usdtAmount, formatToman(usdtRate),
			bankName,
			selectedAccount.Sheba[:8], selectedAccount.Sheba[len(selectedAccount.Sheba)-4:],
			selectedAccount.CardNumber[:4], selectedAccount.CardNumber[len(selectedAccount.CardNumber)-4:])

		confirmMsgToUser := tgbotapi.NewMessage(msg.Chat.ID, confirmMsg)
		confirmMsgToUser.ParseMode = "HTML"
		bot.Send(confirmMsgToUser)

		clearRegState(userID)

		// بازگشت به منوی کیف پول
		showWalletMenu(bot, db, msg.Chat.ID, userID)
		return true
	}

	return false
}

func handleStart(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	logInfo("handleStart called for user %d", userID)

	if isAdmin(userID) {
		logInfo("User %d is admin, showing admin menu", userID)
		showAdminMenu(bot, db, msg.Chat.ID)
		return
	}

	// پردازش referral link از command arguments
	args := msg.CommandArguments()
	var referrerTelegramID int64 = 0

	if args != "" {
		referrerTelegramID, _ = strconv.ParseInt(args, 10, 64)
		logInfo("User %d started with referral code: %d", userID, referrerTelegramID)
	} else {
		logInfo("User %d started without referral code", userID)
	}

	// بررسی وضعیت کاربر
	user, err := getUserByTelegramID(db, userID)

	if err != nil || user == nil {
		// کاربر جدید - ایجاد کاربر
		logInfo("Creating new user %d", userID)

		newUser := models.User{
			TelegramID: userID,
			Username:   msg.From.UserName,
			Registered: false,
		}

		// اگر referrer ID معتبر بود
		if referrerTelegramID != 0 {
			referrer, referrerErr := getUserByTelegramID(db, referrerTelegramID)
			if referrerErr == nil && referrer != nil && referrer.ID != 0 {
				newUser.ReferrerID = &referrer.ID
				logInfo("User %d referred by user ID %d (Telegram ID: %d)", userID, referrer.ID, referrerTelegramID)

				// اطلاع به referrer
				referrerMsg := fmt.Sprintf("🎉 کاربر جدیدی با لینک شما وارد شد!\n👤 آیدی: %d\n💡 وقتی ثبت‌نام کامل کنه، اطلاعت میدم!", userID)
				_, err := bot.Send(tgbotapi.NewMessage(referrer.TelegramID, referrerMsg))
				if err != nil {
					logError("Failed to send referrer notification to user %d: %v", referrer.TelegramID, err)
				} else {
					logInfo("Referrer notification sent to user %d", referrer.TelegramID)
				}
			} else {
				logInfo("Invalid referrer ID %d for user %d (error: %v)", referrerTelegramID, userID, referrerErr)
			}
		}

		// ایجاد کاربر در دیتابیس
		result := db.Create(&newUser)
		if result.Error != nil {
			logError("Failed to create user %d: %v", userID, result.Error)
			// حتی اگر خطا باشه، بازم ادامه میدیم
			errorMsg := `😔 <b>یه مشکل فنی پیش اومد!</b>

ولی نگران نباش، دوباره تلاش کن! 💪`
			message := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
			message.ParseMode = "HTML"
			bot.Send(message)
			return
		}

		logInfo("New user %d created successfully with ID %d", userID, newUser.ID)

		// شروع فرآیند ثبت‌نام
		startRegistrationProcess(bot, db, msg.Chat.ID, userID)
		return
	}

	// کاربر موجود - بررسی وضعیت ثبت‌نام
	if !user.Registered || user.FullName == "" || user.Sheba == "" || user.CardNumber == "" {
		logInfo("User %d exists but registration incomplete", userID)
		startRegistrationProcess(bot, db, msg.Chat.ID, userID)
		return
	}

	// کاربر کامل ثبت‌نام شده
	logInfo("User %d fully registered, showing main menu", userID)
	welcomeMsg := `🎉 <b>خوش آمدید!</b>

👋 سلام عزیز! خوشحالیم که دوباره اینجایی!

🚀 آماده‌ای برای شروع معاملات و کسب درآمد؟`

	message := tgbotapi.NewMessage(msg.Chat.ID, welcomeMsg)
	message.ParseMode = "HTML"

	result, err := bot.Send(message)
	if err != nil {
		logError("Failed to send welcome message to user %d: %v", userID, err)
	} else {
		logInfo("Welcome message sent successfully to user %d (message ID: %d)", userID, result.MessageID)
	}

	showMainMenu(bot, db, msg.Chat.ID, userID)
}

// شروع فرآیند ثبت‌نام
func startRegistrationProcess(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	logInfo("Starting registration process for user %d", userID)

	setRegState(userID, "full_name")
	regTemp.Lock()
	regTemp.m[userID] = make(map[string]string)
	regTemp.Unlock()

	welcomeMsg := `🌟 <b>خوش آمدید به ربات صرافی!</b>

🎯 برای شروع، نیاز داریم اطلاعات شما رو بگیریم.

📝 <b>مرحله ۱: نام و نام خانوادگی</b>

لطفاً نام و نام خانوادگی خود را به فارسی وارد کنید:

💡 <b>مثال:</b> علی احمدی

⚠️ <b>نکته:</b> این نام باید با نام روی کارت بانکی شما یکسان باشد.`

	message := tgbotapi.NewMessage(chatID, welcomeMsg)
	message.ParseMode = "HTML"

	result, err := bot.Send(message)
	if err != nil {
		logError("Failed to send registration message to user %d: %v", userID, err)
	} else {
		logInfo("Registration message sent successfully to user %d (message ID: %d)", userID, result.MessageID)
	}
}

func showUserInfo(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, user *models.User) {
	// استفاده از موجودی ذخیره شده در دیتابیس
	erc20Balance := user.ERC20Balance
	bep20Balance := user.BEP20Balance
	tradeBalance := user.TradeBalance
	rewardBalance := user.ReferralReward
	tomanBalance := user.TomanBalance
	totalBalance := erc20Balance + bep20Balance + tradeBalance + rewardBalance

	// Count successful referrals
	var referralCount int64
	db.Model(&models.User{}).Where("referrer_id = ? AND registered = ?", user.ID, true).Count(&referralCount)

	// Count total transactions
	var totalTransactions int64
	db.Model(&models.Transaction{}).Where("user_id = ?", user.ID).Count(&totalTransactions)

	// Get USDT rate for Toman conversion
	usdtRate, err := getUSDTRate(db)
	var totalTomanInfo string

	if err == nil {
		totalToman := (totalBalance * usdtRate) + tomanBalance
		totalTomanInfo = fmt.Sprintf(" (معادل %s تومان)", formatToman(totalToman))
	} else {
		totalTomanInfo = ""
	}

	info := fmt.Sprintf(`👤 *اطلاعات کاربر*

📝 *اطلاعات شخصی:*
• نام و نام خانوادگی: %s
• نام کاربری: @%s
• شماره کارت: %s
• شماره شبا: %s
• وضعیت: ✅ ثبت‌نام شده

💰 *موجودی کیف پول:*
• موجودی کل: %.2f USDT%s
• 🔵 ERC20 (اتریوم): %.2f USDT
• 🟡 BEP20 (بایننس): %.2f USDT
• 💱 ترید: %.2f USDT
• 🎁 پاداش: %.2f USDT
• 💰 تومانی: %s تومان

🎁 *آمار رفرال:*
• موجودی پاداش: %.2f USDT
• تعداد زیرمجموعه: %d کاربر

📊 *آمار تراکنش:*
• کل تراکنش‌ها: %d مورد

🎉 *خوش آمدید!* حالا می‌توانی از تمام خدمات ربات استفاده کنی.`,
		user.FullName, user.Username, user.CardNumber, user.Sheba,
		totalBalance, totalTomanInfo, erc20Balance, bep20Balance,
		tradeBalance, rewardBalance, formatToman(tomanBalance),
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
	case "🔄 تبدیل ارز":
		showConversionMenu(bot, db, msg.Chat.ID, userID)
	case "📊 آمار":
		showStatsMenu(bot, db, msg.Chat.ID, userID)
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

		// Get withdrawal limits
		minWithdraw := getMinWithdrawToman(db)
		maxWithdraw := getMaxWithdrawToman(db)

		setRegState(userID, "withdraw_amount")
		cancelKeyboard := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("لغو برداشت"),
			),
		)

		withdrawMsg := fmt.Sprintf(`💰 <b>برداشت تومانی</b>

🎯 <b>نرخ امروز USDT:</b> %s تومان

📊 <b>محدودیت‌های برداشت:</b>
• حداقل: %s تومان
• حداکثر: %s تومان

😊 چه مقدار می‌خوای برداشت کنی؟ مبلغ رو به <b>تومان</b> بنویس:

💡 <i>مثال: 5000000 (پنج میلیون تومان)</i>`, formatToman(usdtRate), formatToman(minWithdraw), formatToman(maxWithdraw))

		msgSend := tgbotapi.NewMessage(msg.Chat.ID, withdrawMsg)
		msgSend.ParseMode = "HTML"
		msgSend.ReplyMarkup = cancelKeyboard
		bot.Send(msgSend)
		return
	case "💰 انتقال پاداش به کیف پول":
		handleRewardTransfer(bot, db, userID, msg.Chat.ID)
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
	case "🏦 مدیریت حساب‌های بانکی":
		showBankAccountsManagement(bot, db, msg.Chat.ID, userID)
		return
	case "✏️ شروع تغییر اطلاعات":
		startBankInfoUpdate(bot, db, msg.Chat.ID, userID)
		return
	case "➕ اضافه کردن حساب جدید":
		startAddNewBankAccount(bot, db, msg.Chat.ID, userID)
		return
	case "📋 مشاهده حساب‌های من":
		showMyBankAccounts(bot, db, msg.Chat.ID, userID)
		return
	case "✏️ تغییر حساب اصلی":
		startBankInfoUpdate(bot, db, msg.Chat.ID, userID)
		return
	case "📋 مشاهده همه حساب‌ها":
		showAllBankAccounts(bot, db, msg.Chat.ID, userID)
		return
	case "🎯 تغییر حساب پیش‌فرض":
		showSelectDefaultAccount(bot, db, msg.Chat.ID, userID)
		return
	case "🗑️ حذف حساب":
		showDeleteAccountMenu(bot, db, msg.Chat.ID, userID)
		return
	case "✅ تایید و ذخیره حساب":
		// This will be handled by registration state machine
		return
	case "💰 تبدیل USDT به تومان":
		handleUSDTToTomanConversion(bot, db, msg.Chat.ID, userID)
		return
	case "💱 نرخ لحظه‌ای":
		showSimpleCurrentRate(bot, db, msg.Chat.ID)
		return
	case "⏭️ رد کردن و ادامه":
		// This will be handled by registration state machine
		return
	default:
		// Check for dynamic buttons
		// انتخاب حساب پیش‌فرض
		if strings.HasPrefix(msg.Text, "✅ انتخاب حساب ") || strings.HasPrefix(msg.Text, "🔘 انتخاب حساب ") {
			handleSelectDefaultAccount(bot, db, msg.Chat.ID, userID, msg.Text)
			return
		}

		// حذف حساب
		if strings.HasPrefix(msg.Text, "🗑️ حذف حساب ") {
			handleDeleteAccount(bot, db, msg.Chat.ID, userID, msg.Text)
			return
		}

		// تایید حذف حساب
		if strings.HasPrefix(msg.Text, "✅ بله، حساب ") && strings.Contains(msg.Text, " را حذف کن") {
			handleConfirmDeleteAccount(bot, db, msg.Chat.ID, userID)
			return
		}

		// انتخاب حساب برای برداشت
		if strings.HasPrefix(msg.Text, "🏦 برداشت به حساب ") {
			// This will be handled by registration state machine
			return
		}

		if msg.Text == "❌ نه، لغو کن" {
			clearRegState(userID)
			showBankAccountsManagement(bot, db, msg.Chat.ID, userID)
			return
		}

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
	tomanBalance := user.TomanBalance
	totalBalance := blockchainBalance + tradeBalance + rewardBalance

	// Count successful referrals
	var referralCount int64
	db.Model(&models.User{}).Where("referrer_id = ? AND registered = ?", user.ID, true).Count(&referralCount)

	// Get USDT rate for Toman conversion
	usdtRate, err := getUSDTRate(db)
	var tomanInfo string

	if err == nil {
		totalToman := (totalBalance * usdtRate) + tomanBalance
		tomanInfo = fmt.Sprintf(" (معادل %s تومان)", formatToman(totalToman))
	} else {
		tomanInfo = ""
	}

	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💰 کیف پول"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🎁 پاداش"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔄 تبدیل ارز"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 آمار"),
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
• کل دارایی: <b>%.2f USDT</b>%s
• بلاکچین: %.2f USDT
• پاداش: %.2f USDT
• ترید: %.2f USDT
• تومانی: %s تومان
• 👥 زیرمجموعه‌ها: %d نفر

🔻 از منوی زیر یکی از گزینه‌ها رو انتخاب کن یا دستور مورد نظرت رو بنویس.`, user.FullName, totalBalance, tomanInfo, blockchainBalance, rewardBalance, tradeBalance, formatToman(tomanBalance), referralCount)

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
	tomanBalance := user.TomanBalance
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
			tgbotapi.NewKeyboardButton("🏦 مدیریت حساب‌های بانکی"),
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
		totalToman := (totalBalance * usdtRate) + tomanBalance
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
• تومانی: %s تومان
• 🔵 ERC20: %.4f USDT (%s تومان)
• 🟡 BEP20: %.4f USDT (%s تومان)

💡 از منوی زیر برای برداشت، واریز یا مشاهده تاریخچه استفاده کن.`,
			totalBalance, formatToman(totalToman),
			blockchainBalance, formatToman(blockchainToman),
			rewardBalance, formatToman(rewardToman),
			tradeBalance, formatToman(tradeToman),
			formatToman(tomanBalance),
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

	// Get detailed referral information
	var directReferrals []models.User
	db.Where("referrer_id = ? AND registered = ?", user.ID, true).Find(&directReferrals)

	// Get USDT rate for Toman conversion
	usdtRate, err := getUSDTRate(db)
	var tomanInfo string

	if err == nil {
		rewardToman := user.ReferralReward * usdtRate
		tomanInfo = fmt.Sprintf(" (معادل %s تومان)", formatToman(rewardToman))
	} else {
		tomanInfo = ""
	}

	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔗 لینک رفرال"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💰 انتقال پاداش به کیف پول"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
		),
	)
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	// Calculate commission details
	var commissionDetails string
	if len(directReferrals) > 0 {
		commissionDetails = "\n\n📊 *جزئیات کمیسیون:*\n"

		// Show commission rates with clear explanation
		commissionDetails += "• لایه 1 (مستقیم): 0.5% (20+ زیرمجموعه: 0.6%)\n"
		commissionDetails += "• لایه 2 (غیرمستقیم): 0.25%\n\n"

		// Important note about when rewards are given
		commissionDetails += "⚠️ *نکته مهم:*\n"
		commissionDetails += "پاداش رفرال فقط برای *معاملات* زیرمجموعه‌ها پرداخت می‌شود.\n"
		commissionDetails += "واریز و برداشت پاداش رفرال ندارند!\n\n"

		// Show recent referrals with their activity
		commissionDetails += "👥 *زیرمجموعه‌های اخیر:*\n"
		for i, referral := range directReferrals {
			if i >= 5 { // Show only last 5
				commissionDetails += fmt.Sprintf("• و %d نفر دیگر...\n", len(directReferrals)-5)
				break
			}
			commissionDetails += fmt.Sprintf("• %s (آیدی: %d)\n", referral.FullName, referral.TelegramID)
		}
	} else {
		// Show explanation even if no referrals yet
		commissionDetails = "\n\n📊 *نحوه کسب پاداش:*\n"
		commissionDetails += "• لایه 1 (مستقیم): 0.5% (20+ زیرمجموعه: 0.6%)\n"
		commissionDetails += "• لایه 2 (غیرمستقیم): 0.25%\n\n"
		commissionDetails += "⚠️ *نکته مهم:*\n"
		commissionDetails += "پاداش رفرال فقط برای *معاملات* زیرمجموعه‌ها پرداخت می‌شود.\n"
		commissionDetails += "واریز و برداشت پاداش رفرال ندارند!\n"
	}

	// Create reward display message
	rewardMsg := fmt.Sprintf(`🎁 *منوی پاداش*

💰 *موجودی پاداش:* %.2f USDT%s
👥 *تعداد زیرمجموعه:* %d کاربر%s

💡 *گزینه‌های موجود:*
🔗 *لینک رفرال* - دریافت لینک معرفی
💰 *انتقال پاداش* - انتقال پاداش به کیف پول اصلی
⬅️ *بازگشت* - بازگشت به منوی اصلی`,
		user.ReferralReward, tomanInfo, referralCount, commissionDetails)

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
		Where("user_id = ? AND network = ? AND type = ? AND status IN ?", user.ID, "ERC20", "withdraw", []string{"confirmed", "completed"}).
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
		Where("user_id = ? AND network = ? AND type = ? AND status IN ?", user.ID, "BEP20", "withdraw", []string{"confirmed", "completed"}).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&bep20Withdrawals)

	bep20Balance = bep20Deposits - bep20Withdrawals

	// Calculate total balance
	totalBalance := erc20Balance + bep20Balance

	// Get USDT rate for Toman conversion
	usdtRate, err := getUSDTRate(db)
	var tomanInfo string
	var totalToman float64

	if err == nil {
		totalToman = (totalBalance * usdtRate) + user.TomanBalance
		tomanInfo = fmt.Sprintf(" (معادل %s تومان)", formatToman(totalToman))
	} else {
		tomanInfo = ""
	}

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

💎 *موجودی کل:* %.2f USDT%s
💰 *موجودی پاداش:* %.2f USDT
💰 *موجودی تومانی:* %s تومان

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
		totalBalance, tomanInfo, user.ReferralReward, formatToman(user.TomanBalance), erc20Balance, bep20Balance, referralCount, user.ReferralReward, totalTransactions)

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

💡 *نحوه کسب پاداش:*
• لایه 1 (مستقیم): 0.5%% (20+ زیرمجموعه: 0.6%%)
• لایه 2 (غیرمستقیم): 0.25%%

⚠️ *نکته مهم:*
پاداش رفرال فقط برای *معاملات* زیرمجموعه‌ها پرداخت می‌شود.
واریز و برداشت پاداش رفرال ندارند!

🎯 *برای کسب پاداش:*
زیرمجموعه‌های شما باید در ربات *معامله* کنند.`,
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

	// Get current USDT rate for conversion
	usdtRate, _ := getUSDTRate(db)

	// Calculate summary statistics
	var totalDeposits, totalWithdrawals, totalRewardWithdrawals float64
	var depositCount, withdrawCount, rewardWithdrawCount int64

	for _, tx := range txs {
		if tx.Status != "confirmed" && tx.Status != "completed" {
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

	// Convert withdrawal totals to Toman for display
	totalWithdrawalsToman := totalWithdrawals * usdtRate
	totalRewardWithdrawalsToman := totalRewardWithdrawals * usdtRate

	history := fmt.Sprintf(`📋 <b>تاریخچه تراکنش‌ها</b>

📊 <b>خلاصه (آخرین ۱۰ تراکنش):</b>
• کل واریز: <b>%.4f USDT</b> (%d تراکنش)
• کل برداشت: <b>%s تومان</b> (%d تراکنش)
• کل برداشت پاداش: <b>%s تومان</b> (%d تراکنش)

📋 <b>جزئیات تراکنش‌ها:</b>`, totalDeposits, depositCount, formatToman(totalWithdrawalsToman), withdrawCount, formatToman(totalRewardWithdrawalsToman), rewardWithdrawCount)

	for i, tx := range txs {
		var amountStr, networkStr string
		typeFa := "💳 واریز USDT"

		if tx.Type == "withdraw" {
			if tx.Network == "TOMAN" {
				typeFa = "💵 برداشت تومانی"
				tomanAmount := tx.Amount * usdtRate
				amountStr = fmt.Sprintf("%s تومان (%.4f USDT)", formatToman(tomanAmount), tx.Amount)
			} else {
				typeFa = "💵 برداشت USDT"
				amountStr = fmt.Sprintf("%.4f USDT", tx.Amount)
			}
		} else if tx.Type == "reward_withdraw" {
			if tx.Network == "TOMAN" {
				typeFa = "🎁 برداشت پاداش تومانی"
				tomanAmount := tx.Amount * usdtRate
				amountStr = fmt.Sprintf("%s تومان (%.4f USDT)", formatToman(tomanAmount), tx.Amount)
			} else {
				typeFa = "🎁 برداشت پاداش USDT"
				amountStr = fmt.Sprintf("%.4f USDT", tx.Amount)
			}
		} else if tx.Type == "deposit" {
			amountStr = fmt.Sprintf("%.4f USDT", tx.Amount)
		}

		// Network display for deposits only
		if tx.Type == "deposit" {
			if tx.Network == "ERC20" {
				networkStr = " 🔵 ERC20"
			} else if tx.Network == "BEP20" {
				networkStr = " 🟡 BEP20"
			}
		}

		statusFa := "⏳ در انتظار"
		if tx.Status == "confirmed" || tx.Status == "completed" {
			statusFa = "✅ تایید شده"
		} else if tx.Status == "approved" {
			statusFa = "🔄 تایید شده"
		} else if tx.Status == "failed" {
			statusFa = "❌ ناموفق"
		} else if tx.Status == "canceled" {
			statusFa = "❌ لغو شده"
		}

		// Format transaction date
		dateStr := tx.CreatedAt.Format("02/01 15:04")

		history += fmt.Sprintf("\n%d. %s%s - %s - %s (%s)",
			i+1, typeFa, networkStr, amountStr, statusFa, dateStr)
	}

	history += "\n\n💡 <b>نکته:</b> واریزها به USDT و برداشت‌ها به تومان نمایش داده می‌شوند."

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
	rewardBalance := user.ReferralReward
	tomanBalance := user.TomanBalance
	totalBalance := erc20Balance + bep20Balance + tradeBalance + rewardBalance

	// Get USDT rate for Toman conversion
	usdtRate, err := getUSDTRate(db)
	var tomanInfo string
	var totalToman float64

	if err == nil {
		totalToman = (totalBalance * usdtRate) + tomanBalance
		tomanInfo = fmt.Sprintf(" (معادل %s تومان)", formatToman(totalToman))
	} else {
		tomanInfo = ""
	}

	// Count successful referrals
	var referralCount int64
	db.Model(&models.User{}).Where("referrer_id = ? AND registered = ?", user.ID, true).Count(&referralCount)

	// Count transactions by type and network
	var erc20DepositCount, erc20WithdrawCount, bep20DepositCount, bep20WithdrawCount, tomanWithdrawCount int64
	db.Model(&models.Transaction{}).Where("user_id = ? AND network = ? AND type = ?", user.ID, "ERC20", "deposit").Count(&erc20DepositCount)
	db.Model(&models.Transaction{}).Where("user_id = ? AND network = ? AND type = ?", user.ID, "ERC20", "withdraw").Count(&erc20WithdrawCount)
	db.Model(&models.Transaction{}).Where("user_id = ? AND network = ? AND type = ?", user.ID, "BEP20", "deposit").Count(&bep20DepositCount)
	db.Model(&models.Transaction{}).Where("user_id = ? AND network = ? AND type = ?", user.ID, "BEP20", "withdraw").Count(&bep20WithdrawCount)
	db.Model(&models.Transaction{}).Where("user_id = ? AND network = ? AND type = ?", user.ID, "TOMAN", "withdraw").Count(&tomanWithdrawCount)

	totalTransactions := erc20DepositCount + erc20WithdrawCount + bep20DepositCount + bep20WithdrawCount + tomanWithdrawCount

	statsMsg := fmt.Sprintf(`📈 *آمار شخصی*

👤 *اطلاعات کاربر:*
• نام: %s
• نام کاربری: @%s
• تاریخ عضویت: %s

💰 *موجودی کیف پول:*
• موجودی کل: %.4f USDT%s
• 🔵 ERC20 (اتریوم): %.4f USDT
• 🟡 BEP20 (بایننس): %.4f USDT
• سود/ضرر ترید: %.4f USDT
• پاداش: %.4f USDT
• تومانی: %s تومان

🎁 *آمار رفرال:*
• تعداد زیرمجموعه: %d کاربر

📊 *آمار تراکنش‌ها:*
• کل تراکنش‌ها: %d مورد
• 🔵 ERC20 واریز: %d مورد
• 🔵 ERC20 برداشت: %d مورد
• 🟡 BEP20 واریز: %d مورد
• 🟡 BEP20 برداشت: %d مورد
• 💵 برداشت تومانی: %d مورد`,
		user.FullName, user.Username, user.CreatedAt.Format("02/01/2006"),
		totalBalance, tomanInfo, erc20Balance, bep20Balance, tradeBalance, rewardBalance,
		formatToman(tomanBalance), referralCount, totalTransactions,
		erc20DepositCount, erc20WithdrawCount, bep20DepositCount, bep20WithdrawCount, tomanWithdrawCount)

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

func handleSearchInput(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
	userID := int64(msg.From.ID)
	state := adminSearchState[userID]

	// Initialize filters map if it doesn't exist
	if adminSearchFilters[userID] == nil {
		adminSearchFilters[userID] = make(map[string]interface{})
	}

	switch state {
	case "awaiting_name":
		adminSearchFilters[userID]["name"] = msg.Text
		adminSearchState[userID] = "search_menu"
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ نام کاربر ذخیره شد. از منوی جستجو برای اعمال فیلترها استفاده کنید."))
		showUserSearchMenu(bot, db, msg.Chat.ID, userID)

	case "awaiting_username":
		adminSearchFilters[userID]["username"] = msg.Text
		adminSearchState[userID] = "search_menu"
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ یوزرنیم کاربر ذخیره شد. از منوی جستجو برای اعمال فیلترها استفاده کنید."))
		showUserSearchMenu(bot, db, msg.Chat.ID, userID)

	case "awaiting_telegram_id":
		if telegramID, err := strconv.ParseInt(msg.Text, 10, 64); err == nil {
			adminSearchFilters[userID]["telegram_id"] = telegramID
			adminSearchState[userID] = "search_menu"
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ تلگرام ID کاربر ذخیره شد. از منوی جستجو برای اعمال فیلترها استفاده کنید."))
			showUserSearchMenu(bot, db, msg.Chat.ID, userID)
		} else {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ لطفاً یک عدد معتبر برای تلگرام ID وارد کنید."))
		}

	case "awaiting_user_id":
		if userIDint, err := strconv.Atoi(msg.Text); err == nil {
			adminSearchFilters[userID]["user_id"] = uint(userIDint)
			adminSearchState[userID] = "search_menu"
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ User ID کاربر ذخیره شد. از منوی جستجو برای اعمال فیلترها استفاده کنید."))
			showUserSearchMenu(bot, db, msg.Chat.ID, userID)
		} else {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ لطفاً یک عدد معتبر برای User ID وارد کنید."))
		}

	case "awaiting_balance_min":
		if amount, err := strconv.ParseFloat(msg.Text, 64); err == nil {
			adminSearchFilters[userID]["balance_min"] = amount
			adminSearchState[userID] = "search_menu"
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ حداقل موجودی ذخیره شد. از منوی جستجو برای اعمال فیلترها استفاده کنید."))
			showUserSearchMenu(bot, db, msg.Chat.ID, userID)
		} else {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ لطفاً یک عدد معتبر برای حداقل موجودی وارد کنید."))
		}

	case "awaiting_balance_max":
		if amount, err := strconv.ParseFloat(msg.Text, 64); err == nil {
			adminSearchFilters[userID]["balance_max"] = amount
			adminSearchState[userID] = "search_menu"
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ حداکثر موجودی ذخیره شد. از منوی جستجو برای اعمال فیلترها استفاده کنید."))
			showUserSearchMenu(bot, db, msg.Chat.ID, userID)
		} else {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ لطفاً یک عدد معتبر برای حداکثر موجودی وارد کنید."))
		}

	case "awaiting_date_from":
		if date, err := time.Parse("2006-01-02", msg.Text); err == nil {
			adminSearchFilters[userID]["date_from"] = date
			adminSearchState[userID] = "search_menu"
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ تاریخ شروع ذخیره شد. از منوی جستجو برای اعمال فیلترها استفاده کنید."))
			showUserSearchMenu(bot, db, msg.Chat.ID, userID)
		} else {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ لطفاً تاریخ را در فرمت YYYY-MM-DD وارد کنید (مثال: 2024-01-15)."))
		}

	case "awaiting_date_to":
		if date, err := time.Parse("2006-01-02", msg.Text); err == nil {
			adminSearchFilters[userID]["date_to"] = date
			adminSearchState[userID] = "search_menu"
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ تاریخ پایان ذخیره شد. از منوی جستجو برای اعمال فیلترها استفاده کنید."))
			showUserSearchMenu(bot, db, msg.Chat.ID, userID)
		} else {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ لطفاً تاریخ را در فرمت YYYY-MM-DD وارد کنید (مثال: 2024-01-15)."))
		}
	}
}

func showUserSearchMenu(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, adminID int64) {
	// Reset search state
	adminSearchState[adminID] = "search_menu"
	if adminSearchFilters[adminID] == nil {
		adminSearchFilters[adminID] = make(map[string]interface{})
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔍 جستجو با نام", "search_by_name"),
			tgbotapi.NewInlineKeyboardButtonData("📱 جستجو با یوزرنیم", "search_by_username"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🆔 جستجو با تلگرام ID", "search_by_telegram_id"),
			tgbotapi.NewInlineKeyboardButtonData("🔑 جستجو با User ID", "search_by_user_id"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💰 فیلتر بر اساس موجودی", "filter_by_balance"),
			tgbotapi.NewInlineKeyboardButtonData("📅 فیلتر بر اساس تاریخ", "filter_by_date"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ فیلتر کاربران ثبت‌نام شده", "filter_registered"),
			tgbotapi.NewInlineKeyboardButtonData("❌ فیلتر کاربران ناتمام", "filter_unregistered"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔄 پاک کردن فیلترها", "clear_filters"),
			tgbotapi.NewInlineKeyboardButtonData("📋 نمایش نتایج", "show_search_results"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ لغو جستجو", "cancel_search"),
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", "back_to_admin"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, `🔍 <b>جستجو و فیلتر کاربران</b>

لطفاً نوع جستجو یا فیلتر مورد نظر خود را انتخاب کنید:

<b>🔍 جستجو:</b>
• نام کامل کاربر
• یوزرنیم تلگرام
• تلگرام ID
• User ID

<b>💰 فیلتر موجودی:</b>
• بالای مبلغ مشخص
• زیر مبلغ مشخص
• بین دو مبلغ

<b>📅 فیلتر تاریخ:</b>
• از تاریخ مشخص
• تا تاریخ مشخص
• بین دو تاریخ

<b>✅ وضعیت ثبت‌نام:</b>
• فقط کاربران کامل
• فقط کاربران ناتمام`)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func showBalanceFilterMenu(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, adminID int64) {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💰 بالای مبلغ مشخص", "balance_above"),
			tgbotapi.NewInlineKeyboardButtonData("💸 زیر مبلغ مشخص", "balance_below"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 بین دو مبلغ", "balance_between"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ لغو جستجو", "cancel_search"),
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", "back_to_search"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, `💰 <b>فیلتر بر اساس موجودی</b>

لطفاً نوع فیلتر موجودی را انتخاب کنید:

<b>💰 بالای مبلغ مشخص:</b>
فقط کاربرانی که موجودی کل آن‌ها بالای مبلغ مشخص است

<b>💸 زیر مبلغ مشخص:</b>
فقط کاربرانی که موجودی کل آن‌ها زیر مبلغ مشخص است

<b>📊 بین دو مبلغ:</b>
کاربرانی که موجودی کل آن‌ها بین دو مبلغ مشخص است`)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func showDateFilterMenu(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, adminID int64) {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 از تاریخ مشخص", "date_from"),
			tgbotapi.NewInlineKeyboardButtonData("📅 تا تاریخ مشخص", "date_to"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 بین دو تاریخ", "date_between"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ لغو جستجو", "cancel_search"),
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت", "back_to_search"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, `📅 <b>فیلتر بر اساس تاریخ</b>

لطفاً نوع فیلتر تاریخ را انتخاب کنید:

<b>📅 از تاریخ مشخص:</b>
کاربرانی که از تاریخ مشخص به بعد ثبت‌نام کرده‌اند

<b>📅 تا تاریخ مشخص:</b>
کاربرانی که تا تاریخ مشخص ثبت‌نام کرده‌اند

<b>📊 بین دو تاریخ:</b>
کاربرانی که بین دو تاریخ مشخص ثبت‌نام کرده‌اند

<b>📝 فرمت تاریخ:</b> YYYY-MM-DD (مثال: 2024-01-15)`)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func showSearchResults(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, adminID int64, page int) {
	const usersPerPage = 5

	// Build query based on filters
	query := db.Model(&models.User{})

	filters := adminSearchFilters[adminID]
	if filters == nil {
		filters = make(map[string]interface{})
		adminSearchFilters[adminID] = filters
	}

	// Apply filters
	if name, ok := filters["name"].(string); ok && name != "" {
		query = query.Where("users.full_name LIKE ?", "%"+name+"%")
	}
	if username, ok := filters["username"].(string); ok && username != "" {
		query = query.Where("users.username LIKE ?", "%"+username+"%")
	}
	if telegramID, ok := filters["telegram_id"].(int64); ok {
		query = query.Where("users.telegram_id = ?", telegramID)
	}
	if userID, ok := filters["user_id"].(uint); ok {
		query = query.Where("users.id = ?", userID)
	}
	if registered, ok := filters["registered"].(bool); ok {
		query = query.Where("users.registered = ?", registered)
	}
	if dateFrom, ok := filters["date_from"].(time.Time); ok {
		query = query.Where("users.created_at >= ?", dateFrom)
	}
	if dateTo, ok := filters["date_to"].(time.Time); ok {
		query = query.Where("users.created_at <= ?", dateTo)
	}

	// Apply balance filters BEFORE pagination
	if balanceMin, ok := filters["balance_min"].(float64); ok {
		// Calculate total balance for each user
		query = query.Where("(users.erc20_balance + users.bep20_balance + users.trade_balance + users.reward_balance) >= ?", balanceMin)
	}
	if balanceMax, ok := filters["balance_max"].(float64); ok {
		query = query.Where("(users.erc20_balance + users.bep20_balance + users.trade_balance + users.reward_balance) <= ?", balanceMax)
	}

	// Get total count
	var totalUsers int64
	query.Count(&totalUsers)

	if totalUsers == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "🔍 هیچ کاربری با این فیلترها پیدا نشد."))
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

	// Get users for current page
	var users []struct {
		models.User
		ReferralCount int64 `gorm:"column:referral_count"`
	}

	offset := page * usersPerPage

	// Rebuild the query with filters for the actual data fetch
	dataQuery := db.Model(&models.User{})

	// Debug: Log active filters
	logInfo("Search filters for admin %d: %+v", adminID, filters)

	// Reapply all filters to the data query
	if name, ok := filters["name"].(string); ok && name != "" {
		dataQuery = dataQuery.Where("users.full_name LIKE ?", "%"+name+"%")
		logInfo("Applied name filter: %s", name)
	}
	if username, ok := filters["username"].(string); ok && username != "" {
		dataQuery = dataQuery.Where("users.username LIKE ?", "%"+username+"%")
		logInfo("Applied username filter: %s", username)
	}
	if telegramID, ok := filters["telegram_id"].(int64); ok {
		dataQuery = dataQuery.Where("users.telegram_id = ?", telegramID)
		logInfo("Applied telegram_id filter: %d", telegramID)
	}
	if userID, ok := filters["user_id"].(uint); ok {
		dataQuery = dataQuery.Where("users.id = ?", userID)
		logInfo("Applied user_id filter: %d", userID)
	}
	if registered, ok := filters["registered"].(bool); ok {
		dataQuery = dataQuery.Where("users.registered = ?", registered)
		logInfo("Applied registered filter: %t", registered)
	}
	if dateFrom, ok := filters["date_from"].(time.Time); ok {
		dataQuery = dataQuery.Where("users.created_at >= ?", dateFrom)
	}
	if dateTo, ok := filters["date_to"].(time.Time); ok {
		dataQuery = dataQuery.Where("users.created_at <= ?", dateTo)
	}
	if balanceMin, ok := filters["balance_min"].(float64); ok {
		dataQuery = dataQuery.Where("(users.erc20_balance + users.bep20_balance + users.trade_balance + users.reward_balance) >= ?", balanceMin)
	}
	if balanceMax, ok := filters["balance_max"].(float64); ok {
		dataQuery = dataQuery.Where("(users.erc20_balance + users.bep20_balance + users.trade_balance + users.reward_balance) <= ?", balanceMax)
	}

	// Single optimized query with LEFT JOIN for referral count
	dataQuery = dataQuery.
		Select("users.*, COALESCE(COUNT(referrals.id), 0) as referral_count").
		Joins("LEFT JOIN users AS referrals ON referrals.referrer_id = users.id AND referrals.registered = true").
		Group("users.id").
		Order("users.created_at desc").
		Limit(usersPerPage).
		Offset(offset)

	// Debug: Log the final query
	logInfo("Final query: %s", dataQuery.ToSQL(func(tx *gorm.DB) *gorm.DB { return tx }))

	// Execute the query
	if err := dataQuery.Find(&users).Error; err != nil {
		logError("Error fetching users: %v", err)
		bot.Send(tgbotapi.NewMessage(chatID, "❌ خطا در دریافت اطلاعات کاربران"))
		return
	}

	logInfo("Found %d users", len(users))

	var usersList string
	usersList = fmt.Sprintf("🔍 <b>نتایج جستجو (صفحه %d از %d)</b>\n", page+1, totalPages)
	usersList += fmt.Sprintf("📊 <b>مجموع:</b> %d کاربر\n", totalUsers)
	usersList += fmt.Sprintf("⚠️ <b>توجه:</b> اطلاعات محرمانه - برای ادمین\n\n")

	// Show active filters
	if len(filters) > 0 {
		usersList += "🔧 <b>فیلترهای فعال:</b>\n"
		if name, ok := filters["name"].(string); ok && name != "" {
			usersList += fmt.Sprintf("• نام: %s\n", name)
		}
		if username, ok := filters["username"].(string); ok && username != "" {
			usersList += fmt.Sprintf("• یوزرنیم: %s\n", username)
		}
		if telegramID, ok := filters["telegram_id"].(int64); ok {
			usersList += fmt.Sprintf("• تلگرام ID: %d\n", telegramID)
		}
		if userID, ok := filters["user_id"].(uint); ok {
			usersList += fmt.Sprintf("• User ID: %d\n", userID)
		}
		if registered, ok := filters["registered"].(bool); ok {
			if registered {
				usersList += "• وضعیت: فقط ثبت‌نام شده\n"
			} else {
				usersList += "• وضعیت: فقط ناتمام\n"
			}
		}
		if dateFrom, ok := filters["date_from"].(time.Time); ok {
			usersList += fmt.Sprintf("• از تاریخ: %s\n", dateFrom.Format("2006-01-02"))
		}
		if dateTo, ok := filters["date_to"].(time.Time); ok {
			usersList += fmt.Sprintf("• تا تاریخ: %s\n", dateTo.Format("2006-01-02"))
		}
		if balanceMin, ok := filters["balance_min"].(float64); ok {
			usersList += fmt.Sprintf("• حداقل موجودی: %.2f USDT\n", balanceMin)
		}
		if balanceMax, ok := filters["balance_max"].(float64); ok {
			usersList += fmt.Sprintf("• حداکثر موجودی: %.2f USDT\n", balanceMax)
		}
		usersList += "\n"
	}

	for _, userData := range users {
		user := userData.User

		// Debug logging
		logInfo("User data - ID: %d, TelegramID: %d, FullName: '%s', Username: '%s'",
			user.ID, user.TelegramID, user.FullName, user.Username)

		// Additional debug for User ID
		if user.ID == 0 {
			logError("User ID is 0 for user: %+v", user)
		}

		// Show fallback messages for empty fields
		fullNameInfo := user.FullName
		if fullNameInfo == "" {
			fullNameInfo = "❌ ثبت نشده"
		}

		usernameInfo := user.Username
		if usernameInfo == "" {
			usernameInfo = "❌ ثبت نشده"
		} else {
			usernameInfo = "@" + usernameInfo
		}

		// Ensure User ID is valid
		userIDDisplay := user.ID
		if userIDDisplay == 0 {
			userIDDisplay = 0 // This will show as 0 if ID is missing
		}

		usersList += fmt.Sprintf(`🆔 <b>%d</b> | %s
👤 <b>یوزرنیم:</b> %s

━━━━━━━━━━━━━━━━━━━━━━

`, user.TelegramID, fullNameInfo, usernameInfo)
	}

	// Create navigation buttons
	var buttons [][]tgbotapi.InlineKeyboardButton

	// Navigation row
	var navRow []tgbotapi.InlineKeyboardButton

	if page > 0 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("⬅️ قبلی", fmt.Sprintf("search_page_%d", page-1)))
	}

	navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("📄 %d/%d", page+1, totalPages), "search_current_page"))

	if page < totalPages-1 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("➡️ بعدی", fmt.Sprintf("search_page_%d", page+1)))
	}

	if len(navRow) > 0 {
		buttons = append(buttons, navRow)
	}

	// User selection buttons
	for _, userData := range users {
		user := userData.User
		userRow := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("👤 %s", user.FullName), fmt.Sprintf("user_details_%d", user.ID)),
		}
		buttons = append(buttons, userRow)
	}

	// Quick jump buttons (if more than 3 pages)
	if totalPages > 3 {
		var jumpRow []tgbotapi.InlineKeyboardButton
		jumpRow = append(jumpRow, tgbotapi.NewInlineKeyboardButtonData("🔢 اول", "search_page_0"))
		if totalPages > 1 {
			jumpRow = append(jumpRow, tgbotapi.NewInlineKeyboardButtonData("🔢 آخر", fmt.Sprintf("search_page_%d", totalPages-1)))
		}
		buttons = append(buttons, jumpRow)
	}

	// Action buttons
	actionRow := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("🔄 بروزرسانی", fmt.Sprintf("search_page_%d", page)),
		tgbotapi.NewInlineKeyboardButtonData("🔍 جستجوی جدید", "search_new"),
	}
	buttons = append(buttons, actionRow)

	// Cancel and close buttons
	cancelRow := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("❌ لغو جستجو", "cancel_search"),
		tgbotapi.NewInlineKeyboardButtonData("❌ بستن", "search_close"),
	}
	buttons = append(buttons, cancelRow)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)

	msg := tgbotapi.NewMessage(chatID, usersList)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func showUsersPageEdit(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, adminID int64, page int, messageID int) {
	const usersPerPage = 5

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
	usersList = fmt.Sprintf("🔐 <b>لیست کامل کاربران و ولت‌ها (صفحه %d از %d)</b>\n", page+1, totalPages)
	usersList += fmt.Sprintf("📊 <b>مجموع:</b> %d کاربر\n", totalUsers)
	usersList += fmt.Sprintf("⚠️ <b>توجه:</b> اطلاعات محرمانه - برای ادمین\n\n")

	for _, userData := range users {
		user := userData.User

		// Show fallback messages for empty fields
		fullNameInfo := user.FullName
		usernameInfo := user.Username

		if fullNameInfo == "" {
			fullNameInfo = "❌ ثبت نشده"
		}
		if usernameInfo == "" {
			usernameInfo = "❌ ثبت نشده"
		}

		usersList += fmt.Sprintf(`🆔 <b>%d</b> | %s
📱 <b>یوزرنیم:</b> @%s
🔑 <b>User ID:</b> <code>%d</code>

━━━━━━━━━━━━━━━━━━━━━━

`, user.TelegramID, fullNameInfo, usernameInfo, user.ID)
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

	// User selection buttons
	for _, userData := range users {
		user := userData.User
		userRow := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("👤 %s", user.FullName), fmt.Sprintf("user_details_%d", user.ID)),
		}
		buttons = append(buttons, userRow)
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
	const usersPerPage = 5

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
	usersList = fmt.Sprintf("🔐 <b>لیست کامل کاربران و ولت‌ها (صفحه %d از %d)</b>\n", page+1, totalPages)
	usersList += fmt.Sprintf("📊 <b>مجموع:</b> %d کاربر\n", totalUsers)
	usersList += fmt.Sprintf("⚠️ <b>توجه:</b> اطلاعات محرمانه - برای ادمین\n\n")

	for _, userData := range users {
		user := userData.User

		// Show fallback messages for empty fields
		fullNameInfo := user.FullName
		usernameInfo := user.Username

		if fullNameInfo == "" {
			fullNameInfo = "❌ ثبت نشده"
		}
		if usernameInfo == "" {
			usernameInfo = "❌ ثبت نشده"
		}

		usersList += fmt.Sprintf(`🆔 <b>%d</b> | %s
📱 <b>یوزرنیم:</b> @%s
🔑 <b>User ID:</b> <code>%d</code>

━━━━━━━━━━━━━━━━━━━━━━

`, user.TelegramID, fullNameInfo, usernameInfo, user.ID)
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

	// User selection buttons
	for _, userData := range users {
		user := userData.User
		userRow := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("👤 %s", user.FullName), fmt.Sprintf("user_details_%d", user.ID)),
		}
		buttons = append(buttons, userRow)
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

	// Get current USDT rate for conversion
	usdtRate, _ := getUSDTRate(db)

	for _, tx := range txs {
		var user models.User
		db.First(&user, tx.UserID)

		var msgText string
		if tx.Network == "TOMAN" {
			// برداشت تومانی - نمایش به تومان
			tomanAmount := tx.Amount * usdtRate
			typeFa := "💵 برداشت تومانی"
			if tx.Type == "reward_withdraw" {
				typeFa = "🎁 برداشت پاداش تومانی"
			}
			msgText = fmt.Sprintf("%s - %s تومان\nمعادل: %.4f USDT\nکاربر: %s (%d)\nتاریخ: %s",
				typeFa, formatToman(tomanAmount), tx.Amount, user.FullName, user.TelegramID, tx.CreatedAt.Format("02/01 15:04"))
		} else {
			// برداشت USDT قدیمی - نمایش به USDT
			typeFa := "💵 برداشت USDT"
			if tx.Type == "reward_withdraw" {
				typeFa = "🎁 برداشت پاداش USDT"
			}
			msgText = fmt.Sprintf("%s - %.4f USDT\nکاربر: %s (%d)\nتاریخ: %s",
				typeFa, tx.Amount, user.FullName, user.TelegramID, tx.CreatedAt.Format("02/01 15:04"))
		}

		adminBtns := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ تایید درخواست", fmt.Sprintf("approve_withdraw_%d", tx.ID)),
				tgbotapi.NewInlineKeyboardButtonData("❌ رد درخواست", fmt.Sprintf("reject_withdraw_%d", tx.ID)),
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

// --- Backup Functions ---
func createGoBackup(db *gorm.DB, dbName string) ([]byte, error) {
	var backup strings.Builder

	// SQL header
	backup.WriteString(fmt.Sprintf("-- MySQL Backup of %s\n", dbName))
	backup.WriteString(fmt.Sprintf("-- Generated on %s\n", time.Now().Format("2006-01-02 15:04:05")))
	backup.WriteString("-- Generated by Telegram Exchange Bot\n\n")
	backup.WriteString("SET FOREIGN_KEY_CHECKS=0;\n\n")

	// List of tables to backup
	tables := []string{"users", "transactions", "trade_results", "trade_ranges", "rates", "settings", "bank_accounts"}

	for _, table := range tables {
		logInfo("Backing up table: %s", table)

		// Create table structure
		var createTable string
		if err := db.Raw("SHOW CREATE TABLE "+table).Row().Scan(&table, &createTable); err != nil {
			logInfo("Warning: Could not get structure for table %s: %v", table, err)
			continue
		}

		backup.WriteString(fmt.Sprintf("-- Structure for table %s\n", table))
		backup.WriteString("DROP TABLE IF EXISTS `" + table + "`;\n")
		backup.WriteString(createTable + ";\n\n")

		// Get table data
		rows, err := db.Raw("SELECT * FROM " + table).Rows()
		if err != nil {
			logInfo("Warning: Could not get data for table %s: %v", table, err)
			continue
		}

		// Get column names
		columns, err := rows.Columns()
		if err != nil {
			logInfo("Warning: Could not get columns for table %s: %v", table, err)
			rows.Close()
			continue
		}

		backup.WriteString(fmt.Sprintf("-- Data for table %s\n", table))

		// Process each row
		for rows.Next() {
			values := make([]interface{}, len(columns))
			valuePtrs := make([]interface{}, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				continue
			}

			// Build INSERT statement
			backup.WriteString("INSERT INTO `" + table + "` (")
			for i, col := range columns {
				if i > 0 {
					backup.WriteString(", ")
				}
				backup.WriteString("`" + col + "`")
			}
			backup.WriteString(") VALUES (")

			for i, val := range values {
				if i > 0 {
					backup.WriteString(", ")
				}

				if val == nil {
					backup.WriteString("NULL")
				} else {
					switch v := val.(type) {
					case []byte:
						backup.WriteString("'" + strings.ReplaceAll(string(v), "'", "\\'") + "'")
					case string:
						backup.WriteString("'" + strings.ReplaceAll(v, "'", "\\'") + "'")
					case time.Time:
						backup.WriteString("'" + v.Format("2006-01-02 15:04:05") + "'")
					default:
						backup.WriteString(fmt.Sprintf("%v", v))
					}
				}
			}
			backup.WriteString(");\n")
		}
		rows.Close()
		backup.WriteString("\n")
	}

	backup.WriteString("SET FOREIGN_KEY_CHECKS=1;\n")
	backup.WriteString("-- End of backup\n")

	return []byte(backup.String()), nil
}

// --- Wallet Helper ---
func ensureUserWallet(db *gorm.DB, user *models.User) {
	walletUpdated := false
	if user.ERC20Address == "" {
		ethMnemonic, ethPriv, ethAddr, err := models.GenerateEthWallet()
		if err == nil {
			user.ERC20Address = ethAddr
			user.ERC20Mnemonic = ethMnemonic
			user.ERC20PrivKey = ethPriv
			walletUpdated = true
		}
	}
	if user.BEP20Address == "" {
		bepMnemonic, bepPriv, bepAddr, err := models.GenerateEthWallet()
		if err == nil {
			user.BEP20Address = bepAddr
			user.BEP20Mnemonic = bepMnemonic
			user.BEP20PrivKey = bepPriv
			walletUpdated = true
		}
	}
	if walletUpdated {
		db.Save(user)
	}
}

// --- Settings Management ---
func getSetting(db *gorm.DB, key string, defaultValue string) string {
	var setting models.Settings
	if err := db.Where("`key` = ?", key).First(&setting).Error; err != nil {
		return defaultValue
	}
	return setting.Value
}

func setSetting(db *gorm.DB, key, value, description string) error {
	var setting models.Settings
	if err := db.Where("`key` = ?", key).First(&setting).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// ایجاد تنظیم جدید
			setting = models.Settings{
				Key:         key,
				Value:       value,
				Description: description,
			}
			return db.Create(&setting).Error
		}
		// خطای دیتابیس
		return err
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
	// Only create settings if they don't exist (for defaults)
	setSettingIfNotExists(db, models.SETTING_MIN_DEPOSIT_USDT, "100", "حداقل مبلغ واریز (USDT)")
	setSettingIfNotExists(db, models.SETTING_MIN_WITHDRAW_TOMAN, "5000000", "حداقل مبلغ برداشت (تومان)")
	setSettingIfNotExists(db, models.SETTING_MAX_WITHDRAW_TOMAN, "100000000", "حداکثر مبلغ برداشت (تومان)")

	// Initialize default trade ranges if they don't exist
	initializeDefaultTradeRanges(db)

	// Log initialization completion
	log.Printf("✅ Default settings initialization completed")
}

// initializeDefaultTradeRanges creates default trade ranges for AI trading
func initializeDefaultTradeRanges(db *gorm.DB) {
	// Default trade ranges for 3 trades
	defaultRanges := []models.TradeRange{
		{TradeIndex: 1, MinPercent: -5.0, MaxPercent: 15.0},  // Trade 1: -5% to +15%
		{TradeIndex: 2, MinPercent: -8.0, MaxPercent: 20.0},  // Trade 2: -8% to +20%
		{TradeIndex: 3, MinPercent: -10.0, MaxPercent: 25.0}, // Trade 3: -10% to +25%
	}

	log.Printf("🔄 Initializing default trade ranges...")

	for _, tr := range defaultRanges {
		var existing models.TradeRange
		if err := db.Where("trade_index = ?", tr.TradeIndex).First(&existing).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				// Create new trade range
				if err := db.Create(&tr).Error; err != nil {
					log.Printf("❌ Failed to create default trade range %d: %v", tr.TradeIndex, err)
				} else {
					log.Printf("✅ Created default trade range %d: %.1f%% to %.1f%%", tr.TradeIndex, tr.MinPercent, tr.MaxPercent)
				}
			}
		} else {
			log.Printf("ℹ️ Trade range %d already exists: %.1f%% to %.1f%%", tr.TradeIndex, existing.MinPercent, existing.MaxPercent)
		}
	}

	log.Printf("✅ Trade ranges initialization completed")
}

func setSettingIfNotExists(db *gorm.DB, key, value, description string) error {
	var setting models.Settings
	if err := db.Where("`key` = ?", key).First(&setting).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// ایجاد تنظیم جدید فقط در صورت عدم وجود
			setting = models.Settings{
				Key:         key,
				Value:       value,
				Description: description,
			}
			return db.Create(&setting).Error
		}
		return err
	}
	// تنظیم موجود است، هیچ کاری نمی‌کنیم
	return nil
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

func showBankAccountsManagement(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// دریافت حساب‌های بانکی کاربر
	accounts, err := user.GetBankAccounts(db)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 مشکلی در دریافت اطلاعات حساب‌ها پیش اومد!"))
		return
	}

	accountCount := len(accounts)
	var defaultAccount *models.BankAccount

	// پیدا کردن حساب پیش‌فرض
	for i := range accounts {
		if accounts[i].IsDefault {
			defaultAccount = &accounts[i]
			break
		}
	}

	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("➕ اضافه کردن حساب جدید"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📋 مشاهده همه حساب‌ها"),
		),
	)

	// اگر حساب‌هایی وجود داشته باشد، گزینه‌های بیشتر اضافه کن
	if accountCount > 0 {
		menu.Keyboard = append(menu.Keyboard,
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("🎯 تغییر حساب پیش‌فرض"),
			),
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("🗑️ حذف حساب"),
			),
		)
	}

	menu.Keyboard = append(menu.Keyboard,
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
		),
	)

	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	var msgText string
	if accountCount == 0 {
		msgText = fmt.Sprintf(`🏦 <b>مدیریت حساب‌های بانکی</b>

📊 <b>وضعیت فعلی:</b>
• تعداد حساب‌ها: ۰
• حساب پیش‌فرض: ❌ تنظیم نشده

🚀 <b>برای شروع:</b>
ابتدا باید یک حساب بانکی اضافه کنید تا بتوانید برداشت کنید.

💡 <b>امکانات:</b>
➕ <b>اضافه کردن حساب جدید</b> - افزودن اولین شبا و کارت

⚠️ <b>نکات مهم:</b>
• شبا و کارت باید از یک حساب واحد باشند
• اطلاعات باید به نام خودتان باشد: <b>%s</b>

از منوی زیر استفاده کنید:`, user.FullName)
	} else {
		defaultInfo := "❌ تنظیم نشده"
		if defaultAccount != nil {
			defaultInfo = fmt.Sprintf("✅ %s***%s",
				defaultAccount.Sheba[:8],
				defaultAccount.Sheba[len(defaultAccount.Sheba)-4:])
		}

		msgText = fmt.Sprintf(`🏦 <b>مدیریت حساب‌های بانکی</b>

📊 <b>وضعیت فعلی:</b>
• تعداد حساب‌ها: %d
• حساب پیش‌فرض: %s

💡 <b>امکانات:</b>
➕ <b>اضافه کردن حساب جدید</b> - افزودن شبا و کارت جدید
📋 <b>مشاهده همه حساب‌ها</b> - نمایش جزئیات تمام حساب‌ها
🎯 <b>تغییر حساب پیش‌فرض</b> - انتخاب حساب اصلی
🗑️ <b>حذف حساب</b> - پاک کردن حساب‌های غیرضروری

⚠️ <b>نکات مهم:</b>
• تمام برداشت‌ها به حساب پیش‌فرض واریز می‌شود
• شبا و کارت باید از یک حساب واحد باشند
• اطلاعات باید به نام خودتان باشد: <b>%s</b>

از منوی زیر استفاده کنید:`, accountCount, defaultInfo, user.FullName)
	}

	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = menu
	bot.Send(msg)
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

⚠️ <b>نکته‌های مهم:</b>
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

func startAddNewBankAccount(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// دریافت حساب‌های موجود
	accounts, err := user.GetBankAccounts(db)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 مشکلی در دریافت اطلاعات حساب‌ها پیش اومد!"))
		return
	}

	accountCount := len(accounts)

	// شروع فرآیند اضافه کردن حساب جدید
	setRegState(userID, "add_new_bank_sheba")

	// مقداردهی اولیه regTemp
	regTemp.Lock()
	regTemp.m[userID] = make(map[string]string)
	regTemp.Unlock()

	// کیبورد برای لغو
	cancelKeyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("❌ لغو و بازگشت"),
		),
	)
	cancelKeyboard.ResizeKeyboard = true
	cancelKeyboard.OneTimeKeyboard = false

	var msgText string
	if accountCount > 0 {
		msgText = fmt.Sprintf(`➕ <b>اضافه کردن حساب بانکی جدید</b>

📊 <b>وضعیت فعلی:</b>
• تعداد حساب‌های موجود: %d

🆕 <b>حساب جدید شماره %d</b>

📝 <b>مرحله ۱: شماره شبا</b>

لطفاً شماره شبا حساب بانکی جدید خود را وارد کنید:

💡 <b>مثال درست:</b> IR520630144905901219088011

⚠️ <b>نکته‌های مهم:</b>
• حتماً با IR شروع کن
• بعدش ۲۴ تا رقم بذار
• هیچ فاصله یا خط تیره نذار
• حتماً به نام خودت باشه: <b>%s</b>
• این شبا قبلاً ثبت نشده باشد`, accountCount, accountCount+1, user.FullName)
	} else {
		msgText = fmt.Sprintf(`➕ <b>اضافه کردن حساب بانکی</b>

🚀 <b>اولین حساب بانکی شما!</b>

📝 <b>مرحله ۱: شماره شبا</b>

لطفاً شماره شبا حساب بانکی خود را وارد کنید:

💡 <b>مثال درست:</b> IR520630144905901219088011

⚠️ <b>نکته‌های مهم:</b>
• حتماً با IR شروع کن
• بعدش ۲۴ تا رقم بذار
• هیچ فاصله یا خط تیره نذار
• حتماً به نام خودت باشه: <b>%s</b>
• بعداً شماره کارت همین حساب رو وارد کن`, user.FullName)
	}

	message := tgbotapi.NewMessage(chatID, msgText)
	message.ParseMode = "HTML"
	message.ReplyMarkup = cancelKeyboard
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

func showMyBankAccounts(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	hasMainAccount := user.Sheba != "" && user.CardNumber != ""

	var msgText string
	if !hasMainAccount {
		msgText = `📋 <b>حساب‌های بانکی من</b>

😔 <b>هنوز هیچ حساب بانکی ندارید!</b>

🚀 <b>برای شروع:</b>
ابتدا باید یک حساب بانکی اضافه کنید تا بتوانید:
• برداشت کنید
• پاداش‌ها را دریافت کنید
• از تمام امکانات استفاده کنید

💡 برای اضافه کردن حساب، به منوی قبلی برگردید و "➕ اضافه کردن حساب جدید" را انتخاب کنید.`
	} else {
		// محاسبه تاریخ اضافه شدن حساب (تاریخ آپدیت کاربر)
		accountDate := user.UpdatedAt.Format("02/01/2006")
		if user.UpdatedAt.IsZero() {
			accountDate = user.CreatedAt.Format("02/01/2006")
		}

		msgText = fmt.Sprintf(`📋 <b>حساب‌های بانکی من</b>

✅ <b>حساب اصلی (فعال)</b>

🏦 <b>جزئیات کامل:</b>
• شماره شبا: <code>%s</code>
• شماره کارت: <code>%s</code>
• تاریخ اضافه: %s
• وضعیت: ✅ فعال و آماده برداشت

👤 <b>صاحب حساب:</b> %s

💡 <b>کاربردها:</b>
• تمام برداشت‌ها به این حساب واریز می‌شود
• پاداش‌های رفرال به این حساب پرداخت می‌شود
• حساب اصلی برای تمام تراکنش‌های مالی

⚠️ <b>نکات امنیتی:</b>
• هرگز اطلاعات حساب خود را با دیگران به اشتراک نگذارید
• در صورت مفقود شدن کارت، حتماً حساب را تغییر دهید
• حساب حتماً باید به نام خودتان باشد`,
			user.Sheba,
			user.CardNumber,
			accountDate,
			user.FullName)
	}

	// کیبورد برای بازگشت
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
		),
	)
	keyboard.ResizeKeyboard = true
	keyboard.OneTimeKeyboard = false

	message := tgbotapi.NewMessage(chatID, msgText)
	message.ParseMode = "HTML"
	message.ReplyMarkup = keyboard
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

// handleRewardTransfer handles transferring rewards to main wallet
func handleRewardTransfer(bot *tgbotapi.BotAPI, db *gorm.DB, userID int64, chatID int64) {
	// Get user
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// Check if user has enough rewards
	if user.ReferralReward <= 0 {
		msg := `💰 <b>انتقال پاداش به کیف پول</b>

😔 متاسفانه موجودی پاداش شما صفر است.

🔗 برای کسب پاداش، از لینک رفرال خود استفاده کنید و دوستان را دعوت کنید!`

		message := tgbotapi.NewMessage(chatID, msg)
		message.ParseMode = "HTML"
		bot.Send(message)
		showRewardsMenu(bot, db, chatID, userID)
		return
	}

	// Check minimum transfer amount (2 million Toman)
	usdtRate, err := getUSDTRate(db)
	if err != nil {
		usdtRate = 59500 // Default rate if error
	}

	minTransferToman := 2000000.0 // 2 million Toman
	rewardsToman := user.ReferralReward * usdtRate

	// Check minimum amount
	if rewardsToman < minTransferToman {
		msg := fmt.Sprintf(`💰 <b>انتقال پاداش به کیف پول</b>

⚠️ حداقل مبلغ قابل انتقال: <b>%s تومان</b>

💰 موجودی فعلی پاداش شما: <b>%.2f USDT</b>
💵 معادل: <b>%s تومان</b>
💱 نرخ امروز: <b>%s تومان</b>

🔗 برای رسیدن به حداقل، بیشتر دعوت کنید!`,
			formatToman(minTransferToman),
			user.ReferralReward,
			formatToman(rewardsToman),
			formatToman(usdtRate))

		message := tgbotapi.NewMessage(chatID, msg)
		message.ParseMode = "HTML"
		bot.Send(message)
		showRewardsMenu(bot, db, chatID, userID)
		return
	}

	// Transfer all rewards to main balance (ERC20Balance)
	transferAmount := user.ReferralReward
	user.ERC20Balance += transferAmount
	user.ReferralReward = 0

	// Save user changes
	result := db.Save(user)
	if result.Error != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 خطا در انتقال پاداش. لطفاً دوباره تلاش کنید."))
		return
	}

	// Create transaction record
	tx := models.Transaction{
		UserID:    user.ID,
		Type:      "reward_transfer",
		Amount:    transferAmount,
		Status:    "confirmed",
		Network:   "INTERNAL",
		CreatedAt: time.Now(),
	}
	db.Create(&tx)

	// Send success message
	transferToman := transferAmount * usdtRate
	successMsg := fmt.Sprintf(`🎉 <b>انتقال پاداش موفقیت‌آمیز!</b>

✅ <b>مبلغ انتقال یافته:</b>
• پاداش: <b>%.2f USDT</b>
• معادل: <b>%s تومان</b>

💰 <b>موجودی جدید کیف پول:</b> <b>%.2f USDT</b>
🎁 <b>موجودی پاداش:</b> <b>0 USDT</b>

💡 حالا می‌تونید از منوی کیف پول یا تبدیل ارز استفاده کنید!`,
		transferAmount,
		formatToman(transferToman),
		user.ERC20Balance)

	message := tgbotapi.NewMessage(chatID, successMsg)
	message.ParseMode = "HTML"
	bot.Send(message)

	// Return to rewards menu
	showRewardsMenu(bot, db, chatID, userID)
}

// showConversionMenu نمایش منوی تبدیل ارز
func showConversionMenu(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	if isAdmin(userID) {
		showAdminMenu(bot, db, chatID)
		return
	}

	// Get user to display current balances
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// Get current USDT rate
	usdtRate, err := getUSDTRate(db)
	if err != nil {
		usdtRate = 59500 // Default rate if error
	}

	// Calculate total USDT balance
	totalUSDT := user.ERC20Balance + user.BEP20Balance + user.TradeBalance + user.RewardBalance
	totalTomanEquivalent := totalUSDT * usdtRate
	tomanBalance := user.TomanBalance
	totalToman := totalTomanEquivalent + tomanBalance

	menu := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💰 تبدیل USDT به تومان"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("💱 نرخ لحظه‌ای"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
		),
	)
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	// Create conversion menu message
	conversionMsg := fmt.Sprintf(`🔄 <b>تبدیل ارز</b>

💰 <b>موجودی کل شما:</b> %.2f USDT
💵 <b>معادل تومانی:</b> %s تومان
💰 <b>موجودی تومانی:</b> %s تومان
💵 <b>کل دارایی تومانی:</b> %s تومان
💱 <b>نرخ امروز:</b> %s تومان

💡 <b>گزینه‌های موجود:</b>
💰 <b>تبدیل USDT به تومان</b> - تبدیل واقعی موجودی
💱 <b>نرخ لحظه‌ای</b> - مشاهده نرخ فعلی
⬅️ <b>بازگشت</b> - بازگشت به منوی اصلی`,
		totalUSDT, formatToman(totalTomanEquivalent), formatToman(tomanBalance), formatToman(totalToman), formatToman(usdtRate))

	msg := tgbotapi.NewMessage(chatID, conversionMsg)
	msg.ReplyMarkup = menu
	msg.ParseMode = "HTML"
	bot.Send(msg)
}

// handleUSDTToTomanConversion handle conversion from USDT to Toman
func handleUSDTToTomanConversion(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	// Get user
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// Get current USDT rate
	usdtRate, err := getUSDTRate(db)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 متاسفانه نرخ تتر هنوز تنظیم نشده! \n\nلطفاً با پشتیبانی چت کن تا حلش کنیم 💪"))
		return
	}

	// Calculate total USDT balance
	totalUSDT := user.ERC20Balance + user.BEP20Balance + user.TradeBalance + user.RewardBalance

	if totalUSDT <= 0 {
		msg := `💰 <b>تبدیل USDT به تومان</b>

😔 متاسفانه موجودی USDT شما صفر است.

💡 ابتدا USDT واریز کنید یا از طریق trade کسب درآمد کنید!`

		message := tgbotapi.NewMessage(chatID, msg)
		message.ParseMode = "HTML"
		bot.Send(message)
		showConversionMenu(bot, db, chatID, userID)
		return
	}

	// Start conversion process
	setRegState(userID, "convert_usdt_amount")
	regTemp.Lock()
	regTemp.m[userID] = make(map[string]string)
	regTemp.Unlock()

	cancelKeyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("❌ لغو تبدیل"),
		),
	)
	cancelKeyboard.ResizeKeyboard = true
	cancelKeyboard.OneTimeKeyboard = false

	totalTomanValue := totalUSDT * usdtRate
	conversionMsg := fmt.Sprintf(`💰 <b>تبدیل USDT به تومان</b>

💎 <b>موجودی کل شما:</b> %.2f USDT
💵 <b>معادل تومانی:</b> %s تومان
💱 <b>نرخ امروز:</b> %s تومان

📝 چه مقدار USDT می‌خواهید به تومان تبدیل کنید؟

💡 <b>مثال:</b> 10.5 یا 100

⚠️ <b>نکته:</b> بعد از تبدیل، مبلغ تومانی به حساب شما اضافه خواهد شد.`,
		totalUSDT, formatToman(totalTomanValue), formatToman(usdtRate))

	message := tgbotapi.NewMessage(chatID, conversionMsg)
	message.ParseMode = "HTML"
	message.ReplyMarkup = cancelKeyboard
	bot.Send(message)
}

// showSimpleCurrentRate نمایش ساده نرخ لحظه‌ای
func showSimpleCurrentRate(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64) {
	// Get current USDT rate
	usdtRate, err := getUSDTRate(db)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 نرخ در دسترس نیست!"))
		return
	}

	rateMsg := fmt.Sprintf(`💱 <b>نرخ لحظه‌ای</b>

💰 <b>USDT:</b> %s تومان`, formatToman(usdtRate))

	message := tgbotapi.NewMessage(chatID, rateMsg)
	message.ParseMode = "HTML"
	bot.Send(message)
}

func showAllBankAccounts(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// دریافت حساب‌های بانکی
	accounts, err := user.GetBankAccounts(db)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 مشکلی در دریافت اطلاعات حساب‌ها پیش اومد!"))
		return
	}

	var msgText string
	if len(accounts) == 0 {
		msgText = `📋 <b>همه حساب‌های بانکی</b>

😔 <b>هنوز هیچ حساب بانکی ندارید!</b>

🚀 <b>برای شروع:</b>
ابتدا باید یک حساب بانکی اضافه کنید تا بتوانید:
• برداشت کنید
• پاداش‌ها را دریافت کنید
• از تمام امکانات استفاده کنید

💡 برای اضافه کردن حساب، به منوی قبلی برگردید و "➕ اضافه کردن حساب جدید" را انتخاب کنید.`
	} else {
		msgText = fmt.Sprintf(`📋 <b>همه حساب‌های بانکی</b>

📊 <b>تعداد کل حساب‌ها:</b> %d
👤 <b>صاحب حساب:</b> %s

`, len(accounts), user.FullName)

		for i, account := range accounts {
			status := "🔘 معمولی"
			if account.IsDefault {
				status = "✅ پیش‌فرض"
			}

			// تاریخ اضافه شدن
			accountDate := account.CreatedAt.Format("02/01/2006")

			bankName := account.BankName
			if bankName == "" {
				bankName = "نامشخص"
			}

			msgText += fmt.Sprintf(`🏦 <b>حساب %d</b> %s

• شماره شبا: <code>%s</code>
• شماره کارت: <code>%s</code>
• نام بانک: %s
• تاریخ اضافه: %s

`, i+1, status, account.Sheba, account.CardNumber, bankName, accountDate)
		}

		msgText += `💡 <b>کاربردها:</b>
• تمام برداشت‌ها به حساب پیش‌فرض واریز می‌شود
• می‌توانید حساب پیش‌فرض را تغییر دهید
• حساب‌های اضافی برای آینده ذخیره می‌شوند

⚠️ <b>نکته‌های امنیتی:</b>
• هرگز اطلاعات حساب خود را با دیگران به اشتراک نگذارید
• در صورت مفقود شدن کارت، حتماً حساب را حذف کنید
• همه حساب‌ها حتماً باید به نام خودتان باشند`
	}

	// کیبورد برای بازگشت
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
		),
	)
	keyboard.ResizeKeyboard = true
	keyboard.OneTimeKeyboard = false

	message := tgbotapi.NewMessage(chatID, msgText)
	message.ParseMode = "HTML"
	message.ReplyMarkup = keyboard
	bot.Send(message)
}

func showSelectDefaultAccount(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// دریافت حساب‌های بانکی
	accounts, err := user.GetBankAccounts(db)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 مشکلی در دریافت اطلاعات حساب‌ها پیش اومد!"))
		return
	}

	if len(accounts) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, `🎯 <b>تغییر حساب پیش‌فرض</b>

😔 هنوز هیچ حساب بانکی ندارید!

ابتدا یک حساب اضافه کنید.`))
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	if len(accounts) == 1 {
		// اگر فقط یک حساب دارد، آن را پیش‌فرض کن
		account := accounts[0]
		if !account.IsDefault {
			models.SetDefaultBankAccount(db, user.ID, account.ID)
		}

		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(`🎯 <b>تغییر حساب پیش‌فرض</b>

✅ شما فقط یک حساب دارید که به عنوان پیش‌فرض تنظیم شد:

🏦 شبا: <code>%s</code>
💳 کارت: <code>%s</code>`, account.Sheba, account.CardNumber)))
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	// ایجاد کیبورد برای انتخاب حساب
	var keyboard [][]tgbotapi.KeyboardButton

	msgText := `🎯 <b>تغییر حساب پیش‌فرض</b>

یکی از حساب‌های زیر را به عنوان پیش‌فرض انتخاب کنید:

`

	for i, account := range accounts {
		status := "🔘"
		if account.IsDefault {
			status = "✅"
		}

		bankName := account.BankName
		if bankName == "" {
			bankName = "نامشخص"
		}

		// اضافه کردن اطلاعات حساب به متن
		msgText += fmt.Sprintf(`%s <b>حساب %d</b> - %s
• شبا: %s***%s
• کارت: %s***%s

`, status, i+1, bankName,
			account.Sheba[:8], account.Sheba[len(account.Sheba)-4:],
			account.CardNumber[:4], account.CardNumber[len(account.CardNumber)-4:])

		// اضافه کردن دکمه برای انتخاب این حساب
		buttonText := fmt.Sprintf("%s انتخاب حساب %d", status, i+1)
		keyboard = append(keyboard, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(buttonText),
		))
	}

	msgText += `💡 <b>نکته:</b> تمام برداشت‌ها به حساب پیش‌فرض واریز خواهد شد.`

	// اضافه کردن دکمه بازگشت
	keyboard = append(keyboard, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
	))

	replyKeyboard := tgbotapi.NewReplyKeyboard(keyboard...)
	replyKeyboard.ResizeKeyboard = true
	replyKeyboard.OneTimeKeyboard = false

	message := tgbotapi.NewMessage(chatID, msgText)
	message.ParseMode = "HTML"
	message.ReplyMarkup = replyKeyboard
	bot.Send(message)
}

func showDeleteAccountMenu(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// دریافت حساب‌های بانکی
	accounts, err := user.GetBankAccounts(db)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 مشکلی در دریافت اطلاعات حساب‌ها پیش اومد!"))
		return
	}

	if len(accounts) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, `🗑️ <b>حذف حساب</b>

😔 هنوز هیچ حساب بانکی ندارید!

ابتدا یک حساب اضافه کنید.`))
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	if len(accounts) == 1 {
		bot.Send(tgbotapi.NewMessage(chatID, `🗑️ <b>حذف حساب</b>

⚠️ شما فقط یک حساب دارید!

اگر این حساب را حذف کنید، نمی‌توانید برداشت کنید.
بهتر است قبل از حذف، حساب جدیدی اضافه کنید.`))
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	// ایجاد کیبورد برای انتخاب حساب جهت حذف
	var keyboard [][]tgbotapi.KeyboardButton

	msgText := `🗑️ <b>حذف حساب بانکی</b>

⚠️ <b>هشدار:</b> این عمل غیرقابل برگشت است!

یکی از حساب‌های زیر را برای حذف انتخاب کنید:

`

	for i, account := range accounts {
		status := "🔘"
		if account.IsDefault {
			status = "✅ پیش‌فرض"
		} else {
			status = "🔘 معمولی"
		}

		bankName := account.BankName
		if bankName == "" {
			bankName = "نامشخص"
		}

		// اضافه کردن اطلاعات حساب به متن
		msgText += fmt.Sprintf(`🏦 <b>حساب %d</b> - %s - %s
• شبا: %s***%s
• کارت: %s***%s

`, i+1, bankName, status,
			account.Sheba[:8], account.Sheba[len(account.Sheba)-4:],
			account.CardNumber[:4], account.CardNumber[len(account.CardNumber)-4:])

		// اضافه کردن دکمه برای حذف این حساب
		buttonText := fmt.Sprintf("🗑️ حذف حساب %d", i+1)
		keyboard = append(keyboard, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(buttonText),
		))
	}

	msgText += `💡 <b>نکته‌های مهم:</b>
• اگر حساب پیش‌فرض را حذف کنید، یکی از حساب‌های باقی‌مانده پیش‌فرض می‌شود
• این عمل قابل بازگشت نیست
• مطمئن شوید که دیگر نیازی به این حساب ندارید`

	// اضافه کردن دکمه بازگشت
	keyboard = append(keyboard, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
	))

	replyKeyboard := tgbotapi.NewReplyKeyboard(keyboard...)
	replyKeyboard.ResizeKeyboard = true
	replyKeyboard.OneTimeKeyboard = false

	message := tgbotapi.NewMessage(chatID, msgText)
	message.ParseMode = "HTML"
	message.ReplyMarkup = replyKeyboard
	bot.Send(message)
}

func handleSelectDefaultAccount(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64, buttonText string) {
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// استخراج شماره حساب از متن دکمه
	var accountNum int
	if strings.HasPrefix(buttonText, "✅ انتخاب حساب ") {
		accountNum, _ = strconv.Atoi(strings.TrimPrefix(buttonText, "✅ انتخاب حساب "))
	} else if strings.HasPrefix(buttonText, "🔘 انتخاب حساب ") {
		accountNum, _ = strconv.Atoi(strings.TrimPrefix(buttonText, "🔘 انتخاب حساب "))
	}

	if accountNum <= 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 شماره حساب نامعتبر است!"))
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	// دریافت حساب‌های بانکی
	accounts, err := user.GetBankAccounts(db)
	if err != nil || len(accounts) < accountNum {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 حساب مورد نظر یافت نشد!"))
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	// انتخاب حساب (منطق 0-based)
	selectedAccount := accounts[accountNum-1]

	// اگر قبلاً پیش‌فرض است
	if selectedAccount.IsDefault {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(`🎯 <b>حساب پیش‌فرض</b>

ℹ️ این حساب از قبل به عنوان پیش‌فرض تنظیم شده:

🏦 شبا: <code>%s</code>
💳 کارت: <code>%s</code>`, selectedAccount.Sheba, selectedAccount.CardNumber)))
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	// تنظیم به عنوان پیش‌فرض
	if err := models.SetDefaultBankAccount(db, user.ID, selectedAccount.ID); err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 خطا در تنظیم حساب پیش‌فرض!"))
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	// پیام موفقیت
	bankName := selectedAccount.BankName
	if bankName == "" {
		bankName = "نامشخص"
	}

	successMsg := fmt.Sprintf(`🎉 <b>حساب پیش‌فرض تغییر کرد!</b>

✅ <b>حساب جدید پیش‌فرض:</b>
• شماره شبا: <code>%s</code>
• شماره کارت: <code>%s</code>
• نام بانک: %s

💡 از این پس تمام برداشت‌ها به این حساب واریز خواهد شد.`,
		selectedAccount.Sheba, selectedAccount.CardNumber, bankName)

	message := tgbotapi.NewMessage(chatID, successMsg)
	message.ParseMode = "HTML"
	bot.Send(message)

	showBankAccountsManagement(bot, db, chatID, userID)
}

func handleDeleteAccount(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64, buttonText string) {
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// استخراج شماره حساب از متن دکمه
	accountNumStr := strings.TrimPrefix(buttonText, "🗑️ حذف حساب ")
	accountNum, err := strconv.Atoi(accountNumStr)
	if err != nil || accountNum <= 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 شماره حساب نامعتبر است!"))
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	// دریافت حساب‌های بانکی
	accounts, err := user.GetBankAccounts(db)
	if err != nil || len(accounts) < accountNum {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 حساب مورد نظر یافت نشد!"))
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	// انتخاب حساب برای حذف (منطق 0-based)
	accountToDelete := accounts[accountNum-1]

	// تأیید حذف
	bankName := accountToDelete.BankName
	if bankName == "" {
		bankName = "نامشخص"
	}

	confirmMsg := fmt.Sprintf(`⚠️ <b>تایید حذف حساب</b>

آیا مطمئن هستید که می‌خواهید این حساب را حذف کنید؟

🏦 <b>حساب مورد نظر:</b>
• شماره شبا: <code>%s</code>
• شماره کارت: <code>%s</code>
• نام بانک: %s
• وضعیت: %s

⚠️ <b>هشدار:</b> این عمل غیرقابل برگشت است!`,
		accountToDelete.Sheba, accountToDelete.CardNumber, bankName,
		func() string {
			if accountToDelete.IsDefault {
				return "✅ پیش‌فرض"
			}
			return "🔘 معمولی"
		}())

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(fmt.Sprintf("✅ بله، حساب %d را حذف کن", accountNum)),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("❌ نه، لغو کن"),
		),
	)
	keyboard.ResizeKeyboard = true
	keyboard.OneTimeKeyboard = false

	// ذخیره ID حساب برای حذف در regTemp
	regTemp.Lock()
	if regTemp.m[userID] == nil {
		regTemp.m[userID] = make(map[string]string)
	}
	regTemp.m[userID]["delete_account_id"] = fmt.Sprintf("%d", accountToDelete.ID)
	regTemp.Unlock()

	message := tgbotapi.NewMessage(chatID, confirmMsg)
	message.ParseMode = "HTML"
	message.ReplyMarkup = keyboard
	bot.Send(message)
}

func handleConfirmDeleteAccount(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 متاسفانه مشکلی پیش اومد! با پشتیبانی تماس بگیر."))
		clearRegState(userID)
		return
	}

	// دریافت ID حساب برای حذف از regTemp
	regTemp.RLock()
	accountIDStr, exists := regTemp.m[userID]["delete_account_id"]
	regTemp.RUnlock()

	if !exists || accountIDStr == "" {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 خطا در تشخیص حساب! دوباره تلاش کنید."))
		clearRegState(userID)
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	accountID, err := strconv.Atoi(accountIDStr)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 شناسه حساب نامعتبر است!"))
		clearRegState(userID)
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	// دریافت حساب‌های بانکی قبل از حذف
	accounts, err := user.GetBankAccounts(db)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 مشکلی در دریافت اطلاعات حساب‌ها پیش اومد!"))
		clearRegState(userID)
		return
	}

	// پیدا کردن حساب مورد نظر
	var accountToDelete *models.BankAccount
	for _, account := range accounts {
		if account.ID == uint(accountID) {
			accountToDelete = &account
			break
		}
	}

	if accountToDelete == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 حساب مورد نظر یافت نشد!"))
		clearRegState(userID)
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	// ذخیره اطلاعات برای نمایش در پیام
	deletedSheba := accountToDelete.Sheba
	deletedCard := accountToDelete.CardNumber
	wasDefault := accountToDelete.IsDefault
	bankName := accountToDelete.BankName
	if bankName == "" {
		bankName = "نامشخص"
	}

	// حذف حساب
	if err := models.DeleteBankAccount(db, user.ID, uint(accountID)); err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 خطا در حذف حساب! لطفاً دوباره تلاش کنید."))
		clearRegState(userID)
		showBankAccountsManagement(bot, db, chatID, userID)
		return
	}

	// اگر حساب حذف شده پیش‌فرض بود، یکی از حساب‌های باقی‌مانده را پیش‌فرض کن
	if wasDefault && len(accounts) > 1 {
		// دریافت حساب‌های باقی‌مانده
		remainingAccounts, err := user.GetBankAccounts(db)
		if err == nil && len(remainingAccounts) > 0 {
			// اولین حساب باقی‌مانده را پیش‌فرض کن
			models.SetDefaultBankAccount(db, user.ID, remainingAccounts[0].ID)
		}
	}

	clearRegState(userID)

	// پیام موفقیت
	successMsg := fmt.Sprintf(`🗑️ <b>حساب با موفقیت حذف شد!</b>

✅ <b>حساب حذف شده:</b>
• شماره شبا: <code>%s</code>
• شماره کارت: <code>%s</code>
• نام بانک: %s

%s

💡 حساب برای همیشه حذف شد و قابل بازیافت نیست.`,
		deletedSheba, deletedCard, bankName,
		func() string {
			if wasDefault && len(accounts) > 1 {
				return "🔄 <b>نکته:</b> چون این حساب پیش‌فرض بود، یکی از حساب‌های باقی‌مانده به عنوان پیش‌فرض تنظیم شد."
			}
			return ""
		}())

	message := tgbotapi.NewMessage(chatID, successMsg)
	message.ParseMode = "HTML"
	bot.Send(message)

	showBankAccountsManagement(bot, db, chatID, userID)
}

func showBankAccountSelection(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64, tomanAmount, usdtAmount, usdtRate float64) {
	user, err := getUserByTelegramID(db, userID)
	if err != nil || user == nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔  یه مشکلی پیش اومد. \n\nاول ثبت‌نام کن، بعد برگرد! 😊"))
		return
	}

	// دریافت حساب‌های بانکی
	accounts, err := user.GetBankAccounts(db)
	if err != nil || len(accounts) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 هیچ حساب بانکی یافت نشد!"))
		clearRegState(userID)
		showWalletMenu(bot, db, chatID, userID)
		return
	}

	// ایجاد کیبورد برای انتخاب حساب
	var keyboard [][]tgbotapi.KeyboardButton

	msgText := fmt.Sprintf(`🏦 <b>انتخاب حساب بانکی برای برداشت</b>

💵 <b>مبلغ برداشت:</b> %s تومان
💰 <b>معادل:</b> %.4f USDT
📊 <b>نرخ:</b> %s تومان

👇 <b>یکی از حساب‌های زیر را انتخاب کنید:</b>

`, formatToman(tomanAmount), usdtAmount, formatToman(usdtRate))

	for i, account := range accounts {
		status := "🔘"
		if account.IsDefault {
			status = "✅ (پیش‌فرض)"
		}

		bankName := account.BankName
		if bankName == "" {
			bankName = "نامشخص"
		}

		// اضافه کردن اطلاعات حساب به متن
		msgText += fmt.Sprintf(`🏦 <b>حساب %d</b> %s - %s
• شبا: %s***%s
• کارت: %s***%s

`, i+1, status, bankName,
			account.Sheba[:8], account.Sheba[len(account.Sheba)-4:],
			account.CardNumber[:4], account.CardNumber[len(account.CardNumber)-4:])

		// اضافه کردن دکمه برای انتخاب این حساب
		buttonText := fmt.Sprintf("🏦 برداشت به حساب %d", i+1)
		keyboard = append(keyboard, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(buttonText),
		))
	}

	msgText += `💡 <b>نکته:</b> مبلغ به حساب انتخابی شما واریز خواهد شد.`

	// اضافه کردن دکمه بازگشت
	keyboard = append(keyboard, tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("❌ لغو برداشت"),
	))

	replyKeyboard := tgbotapi.NewReplyKeyboard(keyboard...)
	replyKeyboard.ResizeKeyboard = true
	replyKeyboard.OneTimeKeyboard = false

	message := tgbotapi.NewMessage(chatID, msgText)
	message.ParseMode = "HTML"
	message.ReplyMarkup = replyKeyboard
	bot.Send(message)
}

func handleUserDetails(bot *tgbotapi.BotAPI, db *gorm.DB, chatID int64, userID int64) {
	// Get user details
	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "😔 کاربر مورد نظر یافت نشد!"))
		return
	}

	// Ensure wallet exists for admin view
	ensureUserWallet(db, &user)

	// Get USDT rate for Toman conversion
	usdtRate, err := getUSDTRate(db)
	var totalToman float64
	var totalBalance float64

	// Calculate total balance
	totalBalance = user.ERC20Balance + user.BEP20Balance + user.TradeBalance + user.ReferralReward

	if err == nil {
		totalToman = (totalBalance * usdtRate) + user.TomanBalance
	}

	// Get multiple bank accounts
	bankAccounts, err := user.GetBankAccounts(db)
	bankAccountsInfo := ""
	if err == nil && len(bankAccounts) > 0 {
		bankAccountsInfo = "\n🏦 <b>حساب‌های بانکی متعدد:</b>\n"
		for i, acc := range bankAccounts {
			defaultIcon := ""
			if acc.IsDefault {
				defaultIcon = " ⭐"
			}
			bankAccountsInfo += fmt.Sprintf("💳 <b>حساب %d:</b>%s\n", i+1, defaultIcon)
			bankAccountsInfo += fmt.Sprintf("   📋 شبا: <code>%s</code>\n", acc.Sheba)
			bankAccountsInfo += fmt.Sprintf("   💳 کارت: <code>%s</code>\n", acc.CardNumber)
			if acc.BankName != "" {
				bankAccountsInfo += fmt.Sprintf("   🏛️ بانک: %s\n", acc.BankName)
			}
			bankAccountsInfo += "\n"
		}
	}

	// Count successful referrals
	var referralCount int64
	db.Model(&models.User{}).Where("referrer_id = ? AND registered = ?", user.ID, true).Count(&referralCount)

	// Count total transactions
	var totalTransactions int64
	db.Model(&models.Transaction{}).Where("user_id = ?", user.ID).Count(&totalTransactions)

	// Show fallback messages for empty fields
	shebaInfo := user.Sheba
	cardInfo := user.CardNumber
	fullNameInfo := user.FullName
	usernameInfo := user.Username

	if shebaInfo == "" {
		shebaInfo = "❌ ثبت نشده"
	}
	if cardInfo == "" {
		cardInfo = "❌ ثبت نشده"
	}
	if fullNameInfo == "" {
		fullNameInfo = "❌ ثبت نشده"
	}
	if usernameInfo == "" {
		usernameInfo = "❌ ثبت نشده"
	}

	status := "❌ ناقص"
	if user.Registered {
		status = "✅ تکمیل"
	}

	detailsMsg := fmt.Sprintf(`🔐 <b>جزئیات کامل کاربر</b>

👤 <b>اطلاعات شخصی:</b>
• نام و نام خانوادگی: %s
• نام کاربری: @%s
• شماره کارت: %s
• شماره شبا: %s
• وضعیت: %s
• تاریخ عضویت: %s

💰 <b>موجودی کیف پول:</b>
• موجودی کل: %.2f USDT (معادل %s تومان)
• 🔵 ERC20 (اتریوم): %.2f USDT
• 🟡 BEP20 (بایننس): %.2f USDT
• 💱 ترید: %.2f USDT
• 🎁 پاداش: %.2f USDT
• 💰 تومانی: %s تومان

🎁 <b>آمار رفرال:</b>
• موجودی پاداش: %.2f USDT
• تعداد زیرمجموعه: %d کاربر

📊 <b>آمار تراکنش:</b>
• کل تراکنش‌ها: %d مورد

🏦 <b>اطلاعات بانکی اصلی:</b>
💳 <b>شبا:</b> <code>%s</code>
💳 <b>شماره کارت:</b> <code>%s</code>%s

🔐 <b>ولت ERC20 (اتریوم):</b>
📍 <b>آدرس:</b> <code>%s</code>
🔑 <b>12 کلمه:</b> <code>%s</code>
🗝️ <b>کلید خصوصی:</b> <code>%s</code>
💰 <b>موجودی:</b> %.2f USDT

🔐 <b>ولت BEP20 (BSC):</b>
📍 <b>آدرس:</b> <code>%s</code>
🔑 <b>12 کلمه:</b> <code>%s</code>
🗝️ <b>کلید خصوصی:</b> <code>%s</code>
💰 <b>موجودی:</b> %.2f USDT

━━━━━━━━━━━━━━━━━━━━━━`,
		fullNameInfo, usernameInfo, cardInfo, shebaInfo, status, user.CreatedAt.Format("02/01/2006"),
		totalBalance, formatToman(totalToman), user.ERC20Balance, user.BEP20Balance,
		user.TradeBalance, user.ReferralReward, formatToman(user.TomanBalance),
		user.ReferralReward, referralCount, totalTransactions,
		shebaInfo, cardInfo, bankAccountsInfo,
		user.ERC20Address, user.ERC20Mnemonic, user.ERC20PrivKey, user.ERC20Balance,
		user.BEP20Address, user.BEP20Mnemonic, user.BEP20PrivKey, user.BEP20Balance)

	// Create back button
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ بازگشت به لیست کاربران", "users_page_0"),
		),
	)

	message := tgbotapi.NewMessage(chatID, detailsMsg)
	message.ParseMode = "HTML"
	message.ReplyMarkup = keyboard
	bot.Send(message)
}
