package models

type TradeRange struct {
	ID         uint `gorm:"primaryKey"`
	TradeIndex int  // 1, 2, 3
	MinPercent float64
	MaxPercent float64
}

type Rate struct {
	ID    uint    `gorm:"primaryKey"`
	Asset string  `gorm:"uniqueIndex"` // مثلاً USDT
	Value float64 // نرخ به تومان
}
