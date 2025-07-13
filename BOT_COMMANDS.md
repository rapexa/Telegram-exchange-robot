# 🤖 Bot Commands for BotFather

## 📋 Commands to Add in BotFather

Use these commands when setting up your bot with @BotFather:

```
start - 🚀 شروع ربات و ثبت‌نام
fixuser - 🔧 تکمیل ثبت‌نام ناتمام
```

## 🔧 How to Add Commands

### Method 1: Via BotFather Chat
1. Open chat with @BotFather
2. Send: `/setcommands`
3. Select your bot
4. Send the commands list above

### Method 2: Via BotFather Menu
1. Open @BotFather
2. Click "My Bots"
3. Select your bot
4. Click "Edit Bot"
5. Click "Edit Commands"
6. Paste the commands list

## 📝 Command Descriptions

### `/start`
- **Purpose**: شروع ربات و فرآیند ثبت‌نام
- **Usage**: `/start`
- **Description**: 
  - اگر کاربر جدید باشد: شروع فرآیند ثبت‌نام
  - اگر کاربر ثبت‌نام شده باشد: نمایش اطلاعات و منوی اصلی
  - اگر ثبت‌نام ناتمام باشد: ادامه فرآیند ثبت‌نام

### `/fixuser`
- **Purpose**: تکمیل ثبت‌نام برای کاربران ناتمام
- **Usage**: `/fixuser`
- **Description**: 
  - برای کاربرانی که ثبت‌نام ناتمام دارند
  - شروع مجدد فرآیند ثبت‌نام
  - مفید برای رفع مشکلات ثبت‌نام

## 🎯 Additional Commands (Future Implementation)

These commands can be added later when you implement admin features:

```
# Admin Commands (for future)
setrate - 💱 تنظیم نرخ ارز (ادمین)
rates - 📊 نمایش نرخ‌های فعلی
pending - ⏳ سفارشات در انتظار (ادمین)
approve - ✅ تایید سفارش (ادمین)
reject - ❌ رد سفارش (ادمین)
stats - 📈 آمار کلی (ادمین)
broadcast - 📢 پیام همگانی (ادمین)
userinfo - 👤 اطلاعات کاربر (ادمین)
backup - 💾 پشتیبان‌گیری (ادمین)
```

## 🔍 Command Usage Examples

### For Users:
```
/start - شروع کار با ربات
/fixuser - اگر مشکل ثبت‌نام دارید
/help - راهنمای استفاده
```

### For Admins (Future):
```
/setrate USDT 58500 - تنظیم نرخ تتر
/rates - نمایش نرخ‌ها
/pending - دیدن سفارشات در انتظار
/approve 123 - تایید سفارش شماره 123
/reject 123 دلیل - رد سفارش با دلیل
/stats - آمار کلی سیستم
/broadcast پیام مهم - ارسال پیام به همه
/userinfo 123456789 - اطلاعات کاربر
/backup - پشتیبان‌گیری از دیتابیس
```

## 📱 Menu Commands

These are not BotFather commands but menu buttons in your bot:

### Main Menu:
- 💰 کیف پول
- 🎁 پاداش
- 📊 آمار
- 🆘 پشتیبانی

### Wallet Menu:
- 💵 برداشت
- 📋 تاریخچه
- 💳 واریز USDT
- ⬅️ بازگشت

### Rewards Menu:
- 🔗 لینک رفرال
- 💰 دریافت پاداش
- ⬅️ بازگشت

### Stats Menu:
- 📈 آمار شخصی
- 👥 زیرمجموعه‌ها
- ⬅️ بازگشت

## ⚙️ Bot Settings in BotFather

### Recommended Settings:
1. **Bot Description**: "ربات صرافی ارز دیجیتال - خرید و فروش تتر"
2. **About Text**: "ربات صرافی امن و سریع برای خرید و فروش ارزهای دیجیتال"
3. **Profile Picture**: آپلود لوگوی صرافی
4. **Commands**: همان لیست بالا

### Privacy Settings:
- **Group Privacy**: Disabled (if you want bot to work in groups)
- **Inline Mode**: Disabled (unless you need inline features)

## 🔒 Security Notes

1. **Admin Commands**: Only implement admin commands when you have proper authentication
2. **Rate Limiting**: Consider implementing rate limiting for commands
3. **Logging**: All command usage is logged in your bot logs
4. **Validation**: Commands should validate user permissions before execution

## 📊 Command Analytics

You can track command usage through your bot logs:
```bash
# Count /start commands
grep "/start" logs/bot.log | wc -l

# Count /fixuser commands
grep "/fixuser" logs/bot.log | wc -l

# Count errors
grep "\[ERROR\]" logs/bot.log | wc -l
```

## 🚀 Quick Setup

1. **Open @BotFather**
2. **Send**: `/setcommands`
3. **Select your bot**
4. **Paste this**:
```
start - 🚀 شروع ربات و ثبت‌نام
fixuser - 🔧 تکمیل ثبت‌نام ناتمام
help - 📖 راهنمای استفاده از ربات
```
5. **Done!** Your bot now has commands 