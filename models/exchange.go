package models

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// NobitexOrderbook represents the API response from Nobitex
type NobitexOrderbook struct {
	Status         string     `json:"status"`
	LastUpdate     int64      `json:"lastUpdate"`
	LastTradePrice string     `json:"lastTradePrice"`
	Bids           [][]string `json:"bids"`
	Asks           [][]string `json:"asks"`
}

// GetUSDTPriceFromNobitex fetches current USDT price from Nobitex
func GetUSDTPriceFromNobitex() (float64, error) {
	url := "https://apiv2.nobitex.ir/v3/orderbook/USDTIRT"

	resp, err := http.Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch price: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %v", err)
	}

	var orderbook NobitexOrderbook
	err = json.Unmarshal(body, &orderbook)
	if err != nil {
		return 0, fmt.Errorf("failed to parse JSON: %v", err)
	}

	// Calculate average of first 10 bids and asks
	n := 10
	var sum float64
	var count float64

	// Add bid prices
	for i := 0; i < n && i < len(orderbook.Bids); i++ {
		price, err := strconv.ParseFloat(orderbook.Bids[i][0], 64)
		if err == nil {
			sum += price
			count++
		}
	}

	// Add ask prices
	for i := 0; i < n && i < len(orderbook.Asks); i++ {
		price, err := strconv.ParseFloat(orderbook.Asks[i][0], 64)
		if err == nil {
			sum += price
			count++
		}
	}

	if count == 0 {
		return 0, fmt.Errorf("no price data available")
	}

	avg := sum / count
	avgToman := avg / 10 // Convert Rial to Toman (1 Toman = 10 Rial)

	log.Printf("[EXCHANGE] USDT Price from Nobitex: %.0f Rial (%.0f Toman)", avg, avgToman)

	return avgToman, nil
}

// UpdateUSDTRateInDB updates USDT rate in database
func UpdateUSDTRateInDB(db *gorm.DB, price float64) error {
	var rate Rate
	err := db.Where("asset = ?", "USDT").First(&rate).Error

	if err == gorm.ErrRecordNotFound {
		// Create new rate if doesn't exist
		rate = Rate{
			Asset: "USDT",
			Value: price,
		}
		return db.Create(&rate).Error
	} else if err != nil {
		return err
	}

	// Update existing rate
	rate.Value = price
	return db.Save(&rate).Error
}

// AutoUpdateUSDTPrice runs in background and updates USDT price every 3 minutes
func AutoUpdateUSDTPrice(db *gorm.DB, interval time.Duration) {
	log.Printf("[EXCHANGE] Starting auto USDT price update service (interval: %v)", interval)

	// Run immediately on startup (in goroutine to not block)
	go func() {
		// Wait a bit for database to be fully ready
		time.Sleep(2 * time.Second)
		updatePrice(db)
	}()

	// Then run every interval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		updatePrice(db)
	}
}

// updatePrice fetches price from Nobitex and updates database
func updatePrice(db *gorm.DB) {
	// Recover from any panic
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[EXCHANGE] ❌ Panic in updatePrice: %v", r)
		}
	}()

	// Get old price for logging
	var oldRate Rate
	var oldPrice float64
	if err := db.Where("asset = ?", "USDT").First(&oldRate).Error; err == nil {
		if oldRate.ID != 0 {
			oldPrice = oldRate.Value
		}
	}

	price, err := GetUSDTPriceFromNobitex()
	if err != nil {
		log.Printf("[EXCHANGE] Error fetching USDT price from Nobitex: %v", err)
		return
	}

	err = UpdateUSDTRateInDB(db, price)
	if err != nil {
		log.Printf("[EXCHANGE] Error updating USDT rate in database: %v", err)
		return
	}

	// Log the auto update
	if oldPrice > 0 {
		log.Printf("[EXCHANGE] ✅ USDT price updated: %.0f → %.0f Toman (Δ%.0f)", oldPrice, price, price-oldPrice)
	} else {
		log.Printf("[EXCHANGE] ✅ USDT price set: %.0f Toman (first time)", price)
	}
}
