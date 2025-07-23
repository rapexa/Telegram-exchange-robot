package models

type TradeRange struct {
	ID         uint `gorm:"primaryKey"`
	TradeIndex int  // 1, 2, 3
	MinPercent float64
	MaxPercent float64
}

type Rate struct {
	ID    uint    `gorm:"primaryKey"`
	Asset string  `gorm:"size:32;uniqueIndex"` // مثلاً USDT
	Value float64 // نرخ به تومان
}

// Settings برای تنظیمات پلتفرم
type Settings struct {
	ID          uint   `gorm:"primaryKey"`
	Key         string `gorm:"size:64;uniqueIndex"` // نام تنظیم
	Value       string `gorm:"type:text"`           // مقدار
	Description string `gorm:"size:255"`            // توضیحات
}

// پیش‌فرض‌های تنظیمات
const (
	SETTING_MIN_DEPOSIT_USDT   = "min_deposit_usdt"   // حداقل واریز (USDT)
	SETTING_MIN_WITHDRAW_TOMAN = "min_withdraw_toman" // حداقل برداشت (تومان)
	SETTING_MAX_WITHDRAW_TOMAN = "max_withdraw_toman" // حداکثر برداشت (تومان)
)
