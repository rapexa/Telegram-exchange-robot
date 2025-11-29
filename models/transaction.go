package models

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// FetchUSDTTransfers fetches USDT token transfers for a given address and network (ERC20/BEP20)
func FetchUSDTTransfers(address, network, apiKey string) ([]map[string]interface{}, error) {
	var apiBase, contract, url string
	if network == "ERC20" {
		apiBase = "https://api.etherscan.io/v2/api"
		contract = ERC20USDTContract
		url = fmt.Sprintf("%s?chainid=1&module=account&action=tokentx&contractaddress=%s&address=%s&sort=desc&apikey=%s", apiBase, contract, address, apiKey)
		log.Printf("[BLOCKCHAIN] 🔵 ERC20 API Request: address=%s, contract=%s", address, contract)
	} else if network == "BEP20" {
		apiBase = "https://api.etherscan.io/v2/api"
		contract = BEP20USDTContract
		url = fmt.Sprintf("%s?chainid=56&module=account&action=tokentx&contractaddress=%s&address=%s&sort=desc&apikey=%s", apiBase, contract, address, apiKey)
		log.Printf("[BLOCKCHAIN] 🟡 BEP20 API Request: address=%s, contract=%s", address, contract)
	} else {
		return nil, fmt.Errorf("unsupported network: %s", network)
	}

	log.Printf("[BLOCKCHAIN] 📡 Fetching from: %s (network: %s)", url[:100]+"...", network)

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		log.Printf("[BLOCKCHAIN] ❌ HTTP Request Error: %v (network: %s, address: %s)", err, network, address)
		return nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	log.Printf("[BLOCKCHAIN] 📥 HTTP Response Status: %d %s (network: %s)", resp.StatusCode, resp.Status, network)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[BLOCKCHAIN] ❌ HTTP Error Response Body: %s", string(bodyBytes))
		return nil, fmt.Errorf("HTTP error: %d %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[BLOCKCHAIN] ❌ Read Body Error: %v (network: %s)", err, network)
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	// Log first 500 chars of response for debugging
	bodyPreview := string(body)
	if len(bodyPreview) > 500 {
		bodyPreview = bodyPreview[:500] + "..."
	}
	log.Printf("[BLOCKCHAIN] 📄 Response Body Preview: %s", bodyPreview)

	var result struct {
		Status  string                   `json:"status"`
		Message string                   `json:"message"`
		Result  []map[string]interface{} `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[BLOCKCHAIN] ❌ JSON Parse Error: %v (network: %s)", err, network)
		log.Printf("[BLOCKCHAIN] ❌ Raw Response: %s", string(body))
		return nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	log.Printf("[BLOCKCHAIN] 📊 API Response: status=%s, message=%s, result_count=%d (network: %s)",
		result.Status, result.Message, len(result.Result), network)

	// Check API response status
	if result.Status != "1" {
		log.Printf("[BLOCKCHAIN] ⚠️ API Error Response: status=%s, message=%s (network: %s)",
			result.Status, result.Message, network)
		return nil, fmt.Errorf("API error: status=%s, message=%s", result.Status, result.Message)
	}

	log.Printf("[BLOCKCHAIN] ✅ Successfully fetched %d transactions (network: %s, address: %s)",
		len(result.Result), network, address)

	return result.Result, nil
}

// SyncStats holds statistics about blockchain sync operation
type SyncStats struct {
	TotalUsers          int
	NewDeposits         int
	NewWithdrawals      int
	NewERC20Deposits    int
	NewERC20Withdrawals int
	NewBEP20Deposits    int
	NewBEP20Withdrawals int
	SkippedTransactions int
	Error               error
}

// SyncAllUserDepositsWithStats fetches and stores new transactions with detailed statistics
func SyncAllUserDepositsWithStats(db *gorm.DB, apiKey string) SyncStats {
	stats := SyncStats{}
	var users []User
	if err := db.Find(&users).Error; err != nil {
		stats.Error = err
		return stats
	}

	stats.TotalUsers = len(users)
	log.Printf("[BLOCKCHAIN] 🔍 Starting sync for %d users", stats.TotalUsers)

	for i, user := range users {
		log.Printf("[BLOCKCHAIN] 👤 Processing user %d/%d: ID=%d, TelegramID=%d",
			i+1, stats.TotalUsers, user.ID, user.TelegramID)

		// ERC20
		if user.ERC20Address != "" {
			log.Printf("[BLOCKCHAIN] 🔵 Checking ERC20 wallet: %s (user_id=%d)", user.ERC20Address, user.ID)
			txs, err := FetchUSDTTransfers(user.ERC20Address, "ERC20", apiKey)
			if err != nil {
				log.Printf("[BLOCKCHAIN] ❌ ERC20 Fetch Error for user %d: %v", user.ID, err)
			} else {
				log.Printf("[BLOCKCHAIN] ✅ ERC20: Found %d transactions for user %d", len(txs), user.ID)
				for txIndex, tx := range txs {
					txHash, _ := tx["hash"].(string)
					amountStr, _ := tx["value"].(string)
					fromAddr, _ := tx["from"].(string)
					toAddr, _ := tx["to"].(string)
					amountFloat := parseUSDTAmount(amountStr)

					log.Printf("[BLOCKCHAIN] 🔍 ERC20 TX %d/%d: hash=%s, from=%s, to=%s, amount=%s (%.6f USDT)",
						txIndex+1, len(txs), txHash[:10]+"...", fromAddr[:10]+"...", toAddr[:10]+"...", amountStr, amountFloat)

					// Deposit: incoming transfers to this address
					if strings.EqualFold(toAddr, user.ERC20Address) {
						var count int64
						db.Model(&Transaction{}).Where("tx_hash = ? AND network = ?", txHash, "ERC20").Count(&count)
						if count == 0 {
							log.Printf("[BLOCKCHAIN] 💰 NEW ERC20 DEPOSIT: user_id=%d, tx=%s, amount=%.6f USDT",
								user.ID, txHash, amountFloat)
							t := Transaction{
								UserID:  user.ID,
								Type:    "deposit",
								Network: "ERC20",
								Amount:  amountFloat,
								TxHash:  txHash,
								Status:  "confirmed",
							}
							if err := db.Create(&t).Error; err != nil {
								log.Printf("[BLOCKCHAIN] ❌ Failed to create ERC20 deposit transaction: %v", err)
							} else {
								// به‌روزرسانی موجودی کاربر
								user.ERC20Balance += amountFloat
								if err := db.Save(&user).Error; err != nil {
									log.Printf("[BLOCKCHAIN] ❌ Failed to update user balance: %v", err)
								} else {
									log.Printf("[BLOCKCHAIN] ✅ Updated user %d ERC20 balance: +%.6f USDT (new: %.6f)",
										user.ID, amountFloat, user.ERC20Balance)
								}
								stats.NewDeposits++
								stats.NewERC20Deposits++
							}
						} else {
							log.Printf("[BLOCKCHAIN] ⏭️ SKIP ERC20 DEPOSIT (exists): user_id=%d, tx=%s", user.ID, txHash)
							stats.SkippedTransactions++
						}
					}
					// Withdraw: outgoing transfers from this address
					if strings.EqualFold(fromAddr, user.ERC20Address) {
						var count int64
						db.Model(&Transaction{}).Where("tx_hash = ? AND network = ?", txHash, "ERC20").Count(&count)
						if count == 0 {
							log.Printf("[BLOCKCHAIN] 💸 NEW ERC20 WITHDRAWAL: user_id=%d, tx=%s, amount=%.6f USDT",
								user.ID, txHash, amountFloat)
							t := Transaction{
								UserID:  user.ID,
								Type:    "withdraw",
								Network: "ERC20",
								Amount:  amountFloat,
								TxHash:  txHash,
								Status:  "confirmed",
							}
							if err := db.Create(&t).Error; err != nil {
								log.Printf("[BLOCKCHAIN] ❌ Failed to create ERC20 withdrawal transaction: %v", err)
							} else {
								// به‌روزرسانی موجودی کاربر
								user.ERC20Balance -= amountFloat
								if user.ERC20Balance < 0 {
									user.ERC20Balance = 0
								}
								if err := db.Save(&user).Error; err != nil {
									log.Printf("[BLOCKCHAIN] ❌ Failed to update user balance: %v", err)
								} else {
									log.Printf("[BLOCKCHAIN] ✅ Updated user %d ERC20 balance: -%.6f USDT (new: %.6f)",
										user.ID, amountFloat, user.ERC20Balance)
								}
								stats.NewWithdrawals++
								stats.NewERC20Withdrawals++
							}
						} else {
							log.Printf("[BLOCKCHAIN] ⏭️ SKIP ERC20 WITHDRAWAL (exists): user_id=%d, tx=%s", user.ID, txHash)
							stats.SkippedTransactions++
						}
					}
				}
			}
		}
		// BEP20
		if user.BEP20Address != "" {
			log.Printf("[BLOCKCHAIN] 🟡 Checking BEP20 wallet: %s (user_id=%d)", user.BEP20Address, user.ID)
			txs, err := FetchUSDTTransfers(user.BEP20Address, "BEP20", apiKey)
			if err != nil {
				log.Printf("[BLOCKCHAIN] ❌ BEP20 Fetch Error for user %d: %v", user.ID, err)
			} else {
				log.Printf("[BLOCKCHAIN] ✅ BEP20: Found %d transactions for user %d", len(txs), user.ID)
				for txIndex, tx := range txs {
					txHash, _ := tx["hash"].(string)
					amountStr, _ := tx["value"].(string)
					fromAddr, _ := tx["from"].(string)
					toAddr, _ := tx["to"].(string)
					amountFloat := parseUSDTAmount(amountStr)

					log.Printf("[BLOCKCHAIN] 🔍 BEP20 TX %d/%d: hash=%s, from=%s, to=%s, amount=%s (%.6f USDT)",
						txIndex+1, len(txs), txHash[:10]+"...", fromAddr[:10]+"...", toAddr[:10]+"...", amountStr, amountFloat)

					// Deposit: incoming transfers to this address
					if strings.EqualFold(toAddr, user.BEP20Address) {
						var count int64
						db.Model(&Transaction{}).Where("tx_hash = ? AND network = ?", txHash, "BEP20").Count(&count)
						if count == 0 {
							log.Printf("[BLOCKCHAIN] 💰 NEW BEP20 DEPOSIT: user_id=%d, tx=%s, amount=%.6f USDT",
								user.ID, txHash, amountFloat)
							t := Transaction{
								UserID:  user.ID,
								Type:    "deposit",
								Network: "BEP20",
								Amount:  amountFloat,
								TxHash:  txHash,
								Status:  "confirmed",
							}
							if err := db.Create(&t).Error; err != nil {
								log.Printf("[BLOCKCHAIN] ❌ Failed to create BEP20 deposit transaction: %v", err)
							} else {
								// به‌روزرسانی موجودی کاربر
								user.BEP20Balance += amountFloat
								if err := db.Save(&user).Error; err != nil {
									log.Printf("[BLOCKCHAIN] ❌ Failed to update user balance: %v", err)
								} else {
									log.Printf("[BLOCKCHAIN] ✅ Updated user %d BEP20 balance: +%.6f USDT (new: %.6f)",
										user.ID, amountFloat, user.BEP20Balance)
								}
								stats.NewDeposits++
								stats.NewBEP20Deposits++
							}
						} else {
							log.Printf("[BLOCKCHAIN] ⏭️ SKIP BEP20 DEPOSIT (exists): user_id=%d, tx=%s", user.ID, txHash)
							stats.SkippedTransactions++
						}
					}
					// Withdraw: outgoing transfers from this address
					if strings.EqualFold(fromAddr, user.BEP20Address) {
						var count int64
						db.Model(&Transaction{}).Where("tx_hash = ? AND network = ?", txHash, "BEP20").Count(&count)
						if count == 0 {
							log.Printf("[BLOCKCHAIN] 💸 NEW BEP20 WITHDRAWAL: user_id=%d, tx=%s, amount=%.6f USDT",
								user.ID, txHash, amountFloat)
							t := Transaction{
								UserID:  user.ID,
								Type:    "withdraw",
								Network: "BEP20",
								Amount:  amountFloat,
								TxHash:  txHash,
								Status:  "confirmed",
							}
							if err := db.Create(&t).Error; err != nil {
								log.Printf("[BLOCKCHAIN] ❌ Failed to create BEP20 withdrawal transaction: %v", err)
							} else {
								// به‌روزرسانی موجودی کاربر
								user.BEP20Balance -= amountFloat
								if user.BEP20Balance < 0 {
									user.BEP20Balance = 0
								}
								if err := db.Save(&user).Error; err != nil {
									log.Printf("[BLOCKCHAIN] ❌ Failed to update user balance: %v", err)
								} else {
									log.Printf("[BLOCKCHAIN] ✅ Updated user %d BEP20 balance: -%.6f USDT (new: %.6f)",
										user.ID, amountFloat, user.BEP20Balance)
								}
								stats.NewWithdrawals++
								stats.NewBEP20Withdrawals++
							}
						} else {
							log.Printf("[BLOCKCHAIN] ⏭️ SKIP BEP20 WITHDRAWAL (exists): user_id=%d, tx=%s", user.ID, txHash)
							stats.SkippedTransactions++
						}
					}
				}
			}
		} else {
			log.Printf("[BLOCKCHAIN] ⚠️ User %d has no BEP20 address", user.ID)
		}
	}

	log.Printf("[BLOCKCHAIN] ✅ Sync completed: %d users, %d new deposits (ERC20: %d, BEP20: %d), %d new withdrawals (ERC20: %d, BEP20: %d), %d skipped",
		stats.TotalUsers, stats.NewDeposits, stats.NewERC20Deposits, stats.NewBEP20Deposits,
		stats.NewWithdrawals, stats.NewERC20Withdrawals, stats.NewBEP20Withdrawals, stats.SkippedTransactions)

	return stats
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
