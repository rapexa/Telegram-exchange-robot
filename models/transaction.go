package models

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	ERC20USDTContract = "0xdAC17F958D2ee523a2206206994597C13D831ec7"
	BEP20USDTContract = "0x55d398326f99059fF775485246999027B3197955"
)

type TradeResult struct {
	ID            uint `gorm:"primaryKey"`
	TransactionID uint // به کدام واریز مربوط است
	UserID        uint
	TradeIndex    int     // شماره معامله (۱، ۲، ۳)
	Percent       float64 // درصد سود/ضرر
	ResultAmount  float64 // مبلغ نهایی بعد از این ترید
	CreatedAt     time.Time
}

// ... existing code ...

type Transaction struct {
	ID            uint   `gorm:"primaryKey"`
	UserID        uint   `gorm:"index"`
	Type          string // deposit or withdraw
	Network       string // ERC20 or BEP20
	Amount        float64
	TxHash        string `gorm:"size:128"`
	Status        string // pending, approved, completed, failed
	BankAccountID *uint  `gorm:"index"` // ID حساب بانکی انتخابی برای برداشت
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     gorm.DeletedAt `gorm:"index"`
	TradeCount    int            `gorm:"default:0"` // تعداد دفعات ترید برای این تراکنش
}

// Etherscan Multichain API endpoint
const etherscanAPIBase = "https://api.etherscan.io/api"
const bscscanAPIBase = "https://api.bscscan.com/api"

// FetchUSDTTransfers fetches USDT token transfers for a given address and network (ERC20/BEP20)
func FetchUSDTTransfers(address, network, apiKey string) ([]map[string]interface{}, error) {
	var apiBase, contract, url string
	if network == "ERC20" {
		apiBase = "https://api.etherscan.io/v2/api"
		contract = ERC20USDTContract
		url = fmt.Sprintf("%s?chainid=1&module=account&action=tokentx&contractaddress=%s&address=%s&sort=desc&apikey=%s", apiBase, contract, address, apiKey)
	} else if network == "BEP20" {
		apiBase = "https://api.etherscan.io/v2/api"
		contract = BEP20USDTContract
		url = fmt.Sprintf("%s?chainid=56&module=account&action=tokentx&contractaddress=%s&address=%s&sort=desc&apikey=%s", apiBase, contract, address, apiKey)
	} else {
		return nil, fmt.Errorf("unsupported network: %s", network)
	}

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result struct {
		Status  string                   `json:"status"`
		Message string                   `json:"message"`
		Result  []map[string]interface{} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Status != "1" {
		return nil, fmt.Errorf("API error: %s", result.Message)
	}
	return result.Result, nil
}

// SyncAllUserDeposits fetches and stores new deposit transactions for all users
func SyncAllUserDeposits(db *gorm.DB, apiKey string) error {
	var users []User
	if err := db.Find(&users).Error; err != nil {
		return err
	}
	for _, user := range users {
		// ERC20
		if user.ERC20Address != "" {
			txs, err := FetchUSDTTransfers(user.ERC20Address, "ERC20", apiKey)
			if err == nil {
				for _, tx := range txs {
					txHash, _ := tx["hash"].(string)
					amountStr, _ := tx["value"].(string)
					amountFloat := parseUSDTAmount(amountStr)
					// Deposit: incoming transfers to this address
					if to, ok := tx["to"].(string); ok && strings.EqualFold(to, user.ERC20Address) {
						var count int64
						db.Model(&Transaction{}).Where("tx_hash = ? AND network = ?", txHash, "ERC20").Count(&count)
						if count == 0 {
							fmt.Printf("[DEBUG] ERC20 DEPOSIT: user_id=%d, address=%s, tx=%s, amount=%.6f -> INSERTED\n", user.ID, user.ERC20Address, txHash, amountFloat)
							t := Transaction{
								UserID:  user.ID,
								Type:    "deposit",
								Network: "ERC20",
								Amount:  amountFloat,
								TxHash:  txHash,
								Status:  "confirmed",
							}
							db.Create(&t)

							// به‌روزرسانی موجودی کاربر
							user.ERC20Balance += amountFloat
							db.Save(&user)

							// پاداش رفرال فقط برای تریدها پرداخت می‌شود، نه برای واریز
						} else {
							fmt.Printf("[DEBUG] ERC20 DEPOSIT: user_id=%d, address=%s, tx=%s, amount=%.6f -> SKIPPED (exists)\n", user.ID, user.ERC20Address, txHash, amountFloat)
						}
					}
					// Withdraw: outgoing transfers from this address
					if from, ok := tx["from"].(string); ok && strings.EqualFold(from, user.ERC20Address) {
						var count int64
						db.Model(&Transaction{}).Where("tx_hash = ? AND network = ?", txHash, "ERC20").Count(&count)
						if count == 0 {
							fmt.Printf("[DEBUG] ERC20 WITHDRAW: user_id=%d, address=%s, tx=%s, amount=%.6f -> INSERTED\n", user.ID, user.ERC20Address, txHash, amountFloat)
							t := Transaction{
								UserID:  user.ID,
								Type:    "withdraw",
								Network: "ERC20",
								Amount:  amountFloat,
								TxHash:  txHash,
								Status:  "confirmed",
							}
							db.Create(&t)

							// به‌روزرسانی موجودی کاربر
							user.ERC20Balance -= amountFloat
							db.Save(&user)
						} else {
							fmt.Printf("[DEBUG] ERC20 WITHDRAW: user_id=%d, address=%s, tx=%s, amount=%.6f -> SKIPPED (exists)\n", user.ID, user.ERC20Address, txHash, amountFloat)
						}
					}
				}
			}
		}
		// BEP20
		if user.BEP20Address != "" {
			txs, err := FetchUSDTTransfers(user.BEP20Address, "BEP20", apiKey)
			if err == nil {
				for _, tx := range txs {
					txHash, _ := tx["hash"].(string)
					amountStr, _ := tx["value"].(string)
					amountFloat := parseUSDTAmount(amountStr)
					// Deposit: incoming transfers to this address
					if to, ok := tx["to"].(string); ok && strings.EqualFold(to, user.BEP20Address) {
						var count int64
						db.Model(&Transaction{}).Where("tx_hash = ? AND network = ?", txHash, "BEP20").Count(&count)
						if count == 0 {
							fmt.Printf("[DEBUG] BEP20 DEPOSIT: user_id=%d, address=%s, tx=%s, amount=%.6f -> INSERTED\n", user.ID, user.BEP20Address, txHash, amountFloat)
							t := Transaction{
								UserID:  user.ID,
								Type:    "deposit",
								Network: "BEP20",
								Amount:  amountFloat,
								TxHash:  txHash,
								Status:  "confirmed",
							}
							db.Create(&t)

							// به‌روزرسانی موجودی کاربر
							user.BEP20Balance += amountFloat
							db.Save(&user)

							// پاداش رفرال فقط برای تریدها پرداخت می‌شود، نه برای واریز
						} else {
							fmt.Printf("[DEBUG] BEP20 DEPOSIT: user_id=%d, address=%s, tx=%s, amount=%.6f -> SKIPPED (exists)\n", user.ID, user.BEP20Address, txHash, amountFloat)
						}
					}
					// Withdraw: outgoing transfers from this address
					if from, ok := tx["from"].(string); ok && strings.EqualFold(from, user.BEP20Address) {
						var count int64
						db.Model(&Transaction{}).Where("tx_hash = ? AND network = ?", txHash, "BEP20").Count(&count)
						if count == 0 {
							fmt.Printf("[DEBUG] BEP20 WITHDRAW: user_id=%d, address=%s, tx=%s, amount=%.6f -> INSERTED\n", user.ID, user.BEP20Address, txHash, amountFloat)
							t := Transaction{
								UserID:  user.ID,
								Type:    "withdraw",
								Network: "BEP20",
								Amount:  amountFloat,
								TxHash:  txHash,
								Status:  "confirmed",
							}
							db.Create(&t)

							// به‌روزرسانی موجودی کاربر
							user.BEP20Balance -= amountFloat
							db.Save(&user)
						} else {
							fmt.Printf("[DEBUG] BEP20 WITHDRAW: user_id=%d, address=%s, tx=%s, amount=%.6f -> SKIPPED (exists)\n", user.ID, user.BEP20Address, txHash, amountFloat)
						}
					}
				}
			}
		}
	}
	return nil
}

// parseUSDTAmount converts the raw value string to float64 USDT (6 decimals)
func parseUSDTAmount(val string) float64 {
	if val == "" {
		return 0
	}
	intVal, err := strconv.ParseFloat(val, 64)
	if err != nil {
		fmt.Printf("[DEBUG] parseUSDTAmount: invalid value input: %s\n", val)
		return 0
	}
	var amount float64
	if intVal >= 1e15 { // 18 decimals (e.g. 1e18 for 1 USDT)
		amount = intVal / 1e18
		fmt.Printf("[DEBUG] parseUSDTAmount: raw value=%s, float=%.0f, amount=%.6f (18 decimals)\n", val, intVal, amount)
	} else {
		amount = intVal / 1e6
		fmt.Printf("[DEBUG] parseUSDTAmount: raw value=%s, float=%.0f, amount=%.6f (6 decimals)\n", val, intVal, amount)
	}
	return amount
}

// UpdateUserBalancesFromTransactions محاسبه و به‌روزرسانی موجودی کاربران از تراکنش‌های قبلی
func UpdateUserBalancesFromTransactions(db *gorm.DB) error {
	var users []User
	if err := db.Find(&users).Error; err != nil {
		return err
	}

	for _, user := range users {
		// محاسبه موجودی ERC20
		var erc20Deposits, erc20Withdrawals float64
		db.Model(&Transaction{}).
			Where("user_id = ? AND network = ? AND type = ? AND status = ?", user.ID, "ERC20", "deposit", "confirmed").
			Select("COALESCE(SUM(amount), 0)").
			Scan(&erc20Deposits)

		db.Model(&Transaction{}).
			Where("user_id = ? AND network = ? AND type = ? AND status = ?", user.ID, "ERC20", "withdraw", "confirmed").
			Select("COALESCE(SUM(amount), 0)").
			Scan(&erc20Withdrawals)

		// محاسبه موجودی BEP20
		var bep20Deposits, bep20Withdrawals float64
		db.Model(&Transaction{}).
			Where("user_id = ? AND network = ? AND type = ? AND status = ?", user.ID, "BEP20", "deposit", "confirmed").
			Select("COALESCE(SUM(amount), 0)").
			Scan(&bep20Deposits)

		db.Model(&Transaction{}).
			Where("user_id = ? AND network = ? AND type = ? AND status = ?", user.ID, "BEP20", "withdraw", "confirmed").
			Select("COALESCE(SUM(amount), 0)").
			Scan(&bep20Withdrawals)

		// محاسبه conversion transactions (USDT که تبدیل شده)
		var conversionAmount float64
		db.Model(&Transaction{}).
			Where("user_id = ? AND type = ? AND status = ?", user.ID, "conversion", "confirmed").
			Select("COALESCE(SUM(amount), 0)").
			Scan(&conversionAmount)

		// به‌روزرسانی موجودی کاربر (منهای مقدار تبدیل شده)
		user.ERC20Balance = erc20Deposits - erc20Withdrawals - conversionAmount
		user.BEP20Balance = bep20Deposits - bep20Withdrawals

		// If ERC20 goes negative due to conversion, deduct from BEP20
		if user.ERC20Balance < 0 {
			user.BEP20Balance += user.ERC20Balance // Add negative value (subtract)
			user.ERC20Balance = 0
			if user.BEP20Balance < 0 {
				user.BEP20Balance = 0 // Safety check
			}
		}

		db.Save(&user)
	}

	return nil
}
