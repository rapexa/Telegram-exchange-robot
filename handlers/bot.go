package handlers

import (
	"fmt"
	"log"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

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
			tgbotapi.NewKeyboardButton("📢 پیام همگانی"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📋 مدیریت برداشت‌ها"),
		),
	)
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	helpText := `🛠️ <b>پنل مدیریت</b>

<b>دستورات ادمین:</b>

📊 <b>/settrade</b> [شماره معامله] [حداقل درصد] [حداکثر درصد]

یکی از گزینه‌های زیر را انتخاب کنید:`

	msg := tgbotapi.NewMessage(chatID, helpText)
	msg.ReplyMarkup = menu
	msg.ParseMode = "HTML"
	bot.Send(msg)
}

func handleAdminMenu(bot *tgbotapi.BotAPI, db *gorm.DB, msg *tgbotapi.Message) {
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
		statsMsg := fmt.Sprintf("📊 آمار کلی ربات\n\n👥 کل کاربران: %d\n✅ ثبت‌نام کامل: %d\n💰 مجموع واریز: %.2f USDT\n💸 مجموع برداشت: %.2f USDT", userCount, regCount, totalDeposit, totalWithdraw)
		message := tgbotapi.NewMessage(msg.Chat.ID, statsMsg)
		message.ParseMode = "HTML"
		bot.Send(message)
		return
	case "📢 پیام همگانی":
		// Set admin state for broadcast
		adminState[msg.From.ID] = "awaiting_broadcast"
		adminBroadcastState[msg.From.ID] = "awaiting_broadcast"
		m := tgbotapi.NewMessage(msg.Chat.ID, "✏️ پیام خود را برای ارسال همگانی بنویسید:")
		bot.Send(m)
		return
	case "📋 مدیریت برداشت‌ها":
		showAllPendingWithdrawals(bot, db, msg.Chat.ID)
		return
	case "⬅️ بازگشت":
		showMainMenu(bot, db, msg.Chat.ID, msg.From.ID)
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
	message := tgbotapi.NewMessage(msg.Chat.ID, "دستور نامعتبر در پنل مدیریت.")
	bot.Send(message)
	return
}

// Track admin state for broadcast
var adminState = make(map[int64]string)

var adminBroadcastState = make(map[int64]string) // "awaiting_broadcast", "confirm_broadcast", ""
var adminBroadcastDraft = make(map[int64]*tgbotapi.Message)

func logInfo(format string, v ...interface{}) {
	log.Printf("[INFO] "+format, v...)
}

func logError(format string, v ...interface{}) {
	log.Printf("[ERROR] "+format, v...)
}

func logDebug(format string, v ...interface{}) {
	log.Printf("[DEBUG] "+format, v...)
}

func StartBot(bot *tgbotapi.BotAPI, db *gorm.DB) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	logInfo("🔄 Bot update channel started, waiting for messages...")

	for update := range updates {
		// --- هندل دستور ادمین برای /settrade ---
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

						// کم کردن مبلغ قبلی از موجودی بلاکچین (در صورت نیاز)
						if tx.Network == "ERC20" {
							user.ERC20Balance -= lastAmount
							user.ERC20Balance += resultAmount
						} else if tx.Network == "BEP20" {
							user.BEP20Balance -= lastAmount
							user.BEP20Balance += resultAmount
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

					// پیام به کاربر: بعد از ۱ ثانیه نتیجه را ارسال کن
					go func(chatID int64, amount float64, percent float64, resultAmount float64, tradeIndex int) {
						time.Sleep(1 * time.Second) //TODO: change this to 30 minute later
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
				state := adminBroadcastState[userID]
				data := update.CallbackQuery.Data
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
			redirectMsg := `🔒 *دسترسی محدود*

⚠️ برای استفاده از خدمات ربات، ابتدا باید ثبت‌نام خود را تکمیل کنید.

📝 *مراحل ثبت‌نام:*
1️⃣ نام و نام خانوادگی
2️⃣ شماره شبا
3️⃣ شماره کارت

🔄 در حال انتقال به صفحه ثبت‌نام...`

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
• بدون فاصله یا کاراکتر اضافی`

		message := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(shebaMsg, msg.Text))
		message.ParseMode = "Markdown"
		bot.Send(message)
		return true
	} else if state == "sheba" {
		// Validate Sheba format
		logInfo("Validating sheba: '%s'", msg.Text)
		if !models.ValidateSheba(msg.Text) {
			logError("Sheba validation failed for: '%s'", msg.Text)

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
		logInfo("Saving card number: %s for user %d", msg.Text, userID)
		saveRegTemp(userID, "card_number", msg.Text)
		regTemp.RLock()
		info := regTemp.m[userID]
		regTemp.RUnlock()

		logInfo("Completing registration for user %d with info: %+v", userID, info)

		err := registerUser(db, userID, info["full_name"], info["sheba"], info["card_number"])
		if err != nil {
			logError("Error registering user: %v", err)
			errorMsg := `❌ *خطا در ثبت اطلاعات*

متأسفانه خطایی در ثبت اطلاعات رخ داد. لطفاً دوباره تلاش کنید.

🔄 برای شروع مجدد، دستور /start را بزنید.`

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

		showMainMenu(bot, db, msg.Chat.ID, userID)
		return true
	}
	if state == "withdraw_amount" {
		if msg.Text == "لغو برداشت" {
			clearRegState(userID)
			showMainMenu(bot, db, msg.Chat.ID, userID)
			return true
		}
		amount, err := strconv.ParseFloat(msg.Text, 64)
		if err != nil || amount <= 0 {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ مبلغ نامعتبر است. لطفاً فقط عدد وارد کنید."))
			return true
		}
		user, _ := getUserByTelegramID(db, userID)
		// Calculate confirmed balance
		var depositSum, withdrawSum float64
		db.Model(&models.Transaction{}).Where("user_id = ? AND type = ? AND status = ?", user.ID, "deposit", "confirmed").Select("COALESCE(SUM(amount),0)").Scan(&depositSum)
		db.Model(&models.Transaction{}).Where("user_id = ? AND type = ? AND status = ?", user.ID, "withdraw", "confirmed").Select("COALESCE(SUM(amount),0)").Scan(&withdrawSum)
		balance := depositSum - withdrawSum
		if user == nil || balance < amount {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "❌ موجودی کافی نیست."))
			return true
		}
		// Create pending transaction
		tx := models.Transaction{
			UserID: user.ID,
			Type:   "withdraw",
			Amount: amount,
			Status: "pending",
		}
		db.Create(&tx)
		// Notify admin
		adminMsg := fmt.Sprintf("درخواست برداشت جدید:\n\nکاربر: %s (%d)\nمبلغ: %.2f USDT", user.FullName, user.TelegramID, amount)
		adminBtns := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("پرداخت شد", fmt.Sprintf("approve_withdraw_%d", tx.ID)),
				tgbotapi.NewInlineKeyboardButtonData("رد شد", fmt.Sprintf("reject_withdraw_%d", tx.ID)),
			),
		)
		msgToAdmin := tgbotapi.NewMessage(adminUserID, adminMsg)
		msgToAdmin.ReplyMarkup = adminBtns
		bot.Send(msgToAdmin)
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ درخواست برداشت ثبت شد و در انتظار تایید ادمین است."))
		clearRegState(userID)
		return true
	}
	if state == "reward_withdraw_amount" {
		if msg.Text == "لغو برداشت" {
			clearRegState(userID)
			showMainMenu(bot, db, msg.Chat.ID, userID)
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
		adminMsg := fmt.Sprintf("درخواست برداشت پاداش:\n\nکاربر: %s (%d)\nمبلغ: %.2f USDT", user.FullName, user.TelegramID, amount)
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
		redirectMsg := `🔒 *دسترسی محدود*

⚠️ برای استفاده از خدمات ربات، ابتدا باید ثبت‌نام خود را تکمیل کنید.

📝 *مراحل ثبت‌نام:*
1️⃣ نام و نام خانوادگی
2️⃣ شماره شبا
3️⃣ شماره کارت

🔄 در حال انتقال به صفحه ثبت‌نام...`

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
	case "🆘 پشتیبانی":
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "برای پشتیبانی با ادمین تماس بگیرید: @YourAdminUsername"))
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
		setRegState(userID, "withdraw_amount")
		cancelKeyboard := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("لغو برداشت"),
			),
		)
		msgSend := tgbotapi.NewMessage(msg.Chat.ID, "💵 لطفاً مبلغ برداشت را به عدد وارد کنید (USDT):")
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
		bot.Send(tgbotapi.NewMessage(chatID, "کاربر یافت نشد. لطفاً ابتدا ثبت‌نام کنید."))
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
			tgbotapi.NewKeyboardButton("🆘 پشتیبانی"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("ترید با 🤖"),
		),
	)
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	// Create main menu message with summary
	mainMsg := fmt.Sprintf(`💠 *منوی اصلی*

👋 سلام %s!

💰 *خلاصه موجودی:*
• موجودی کل: %.2f USDT
• موجودی بلاکچین: %.2f USDT
• سود/ضرر ترید: %.2f USDT
• پاداش: %.2f USDT
• تعداد زیرمجموعه: %d کاربر

💡 دستورات ربات:
/trades [id] - مشاهده نتایج ترید برای یک واریز

💡 *گزینه‌های موجود:*
💰 *کیف پول* - مدیریت موجودی و تراکنش‌ها
🎁 *پاداش* - سیستم رفرال و پاداش‌ها
📊 *آمار* - آمار شخصی و زیرمجموعه‌ها
🆘 *پشتیبانی* - ارتباط با پشتیبانی`,
		user.FullName, totalBalance, blockchainBalance, tradeBalance, rewardBalance, referralCount)

	msg := tgbotapi.NewMessage(chatID, mainMsg)
	msg.ReplyMarkup = menu
	msg.ParseMode = "Markdown"
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
		bot.Send(tgbotapi.NewMessage(chatID, "کاربر یافت نشد. لطفاً ابتدا ثبت‌نام کنید."))
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
			tgbotapi.NewKeyboardButton("⬅️ بازگشت"),
		),
	)
	menu.ResizeKeyboard = true
	menu.OneTimeKeyboard = false

	// Create balance display message
	balanceMsg := fmt.Sprintf(`💰 *منوی کیف پول*

💎 *موجودی کل:* %.2f USDT

�� *جزئیات موجودی:*
• موجودی بلاکچین: %.2f USDT
• سود/ضرر ترید: %.2f USDT
• پاداش: %.2f USDT

• 🔵 *ERC20 (اتریوم):* %.2f USDT
• 🟡 *BEP20 (بایننس):* %.2f USDT

💡 *گزینه‌های موجود:*
💵 *برداشت* - درخواست برداشت ریالی
📋 *تاریخچه* - مشاهده تراکنش‌های قبلی
💳 *واریز USDT* - واریز ارز دیجیتال
⬅️ *بازگشت* - بازگشت به منوی اصلی`,
		totalBalance, blockchainBalance, tradeBalance, rewardBalance, erc20Balance, bep20Balance)

	msg := tgbotapi.NewMessage(chatID, balanceMsg)
	msg.ReplyMarkup = menu
	msg.ParseMode = "Markdown"
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
		bot.Send(tgbotapi.NewMessage(chatID, "کاربر یافت نشد. لطفاً ابتدا ثبت‌نام کنید."))
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
		bot.Send(tgbotapi.NewMessage(chatID, "کاربر یافت نشد. لطفاً ابتدا ثبت‌نام کنید."))
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
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "آدرس کیف پول شما ساخته نشد. لطفاً با پشتیبانی تماس بگیرید."))
			return
		}
	}

	msgText := fmt.Sprintf(`💳 *آدرس‌های واریز USDT شما*

💰 *موجودی فعلی:*
• 🔵 ERC20 (اتریوم): %.2f USDT
• 🟡 BEP20 (بایننس): %.2f USDT

📥 *آدرس‌های واریز:*

🔵 *ERC20 (اتریوم):*
`+"`%s`"+`

🟡 *BEP20 (بایننس اسمارت چین):*
`+"`%s`"+`

⚠️ *هشدار مهم:*
• فقط USDT را به شبکه صحیح واریز کنید
• ارسال اشتباه باعث از دست رفتن دارایی می‌شود
• حداقل واریز: 10 USDT`,
		erc20Balance, bep20Balance, user.ERC20Address, user.BEP20Address)

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
		emptyMsg := tgbotapi.NewMessage(msg.Chat.ID, "👥 <b>لیست زیرمجموعه‌ها</b>\n\nشما هنوز هیچ زیرمجموعه‌ای ندارید.\n\n💡 برای جذب زیرمجموعه، لینک رفرال خود را به اشتراک بگذارید.")
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
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "📋 *تاریخچه تراکنش‌ها*\n\nهیچ تراکنشی ثبت نشده است.\n\n💡 برای مشاهده تراکنش‌ها، ابتدا باید واریز یا برداشتی انجام دهید."))
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

	history := fmt.Sprintf(`📋 *تاریخچه تراکنش‌ها*

📊 *خلاصه (آخرین ۱۰ تراکنش):*
• کل واریز: %.2f USDT (%d تراکنش)
• کل برداشت: %.2f USDT (%d تراکنش)
• کل برداشت پاداش: %.2f USDT (%d تراکنش)

📋 *جزئیات تراکنش‌ها:*`, totalDeposits, depositCount, totalWithdrawals, withdrawCount, totalRewardWithdrawals, rewardWithdrawCount)

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
	message.ParseMode = "Markdown"
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
