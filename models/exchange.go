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
	log.Printf("[EXCHANGE] 📡 Fetching USDT price from Nobitex: %s", url)

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		log.Printf("[EXCHANGE] ❌ HTTP Request Error: %v", err)
		return 0, fmt.Errorf("failed to fetch price: %v", err)
	}
	defer resp.Body.Close()

	log.Printf("[EXCHANGE] 📥 HTTP Response Status: %d %s", resp.StatusCode, resp.Status)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[EXCHANGE] ❌ HTTP Error Response Body: %s", string(bodyBytes))
		return 0, fmt.Errorf("HTTP error: %d %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[EXCHANGE] ❌ Read Body Error: %v", err)
		return 0, fmt.Errorf("failed to read response: %v", err)
	}

	// Log first 500 chars of response for debugging
	bodyPreview := string(body)
	if len(bodyPreview) > 500 {
		bodyPreview = bodyPreview[:500] + "..."
	}
	log.Printf("[EXCHANGE] 📄 Response Body Preview: %s", bodyPreview)

	var orderbook NobitexOrderbook
	err = json.Unmarshal(body, &orderbook)
	if err != nil {
		log.Printf("[EXCHANGE] ❌ JSON Parse Error: %v", err)
		log.Printf("[EXCHANGE] ❌ Raw Response: %s", string(body))
		return 0, fmt.Errorf("failed to parse JSON: %v", err)
	}

	log.Printf("[EXCHANGE] 📊 Nobitex Orderbook: status=%s, lastUpdate=%d, lastTradePrice=%s, bids=%d, asks=%d",
		orderbook.Status, orderbook.LastUpdate, orderbook.LastTradePrice, len(orderbook.Bids), len(orderbook.Asks))

	// Calculate average of first 10 bids and asks
	n := 10
	var sum float64
	var count float64
	var bidPrices []float64
	var askPrices []float64

	// Add bid prices
	log.Printf("[EXCHANGE] 🔵 Processing %d bid prices (max %d)", len(orderbook.Bids), n)
	for i := 0; i < n && i < len(orderbook.Bids); i++ {
		price, err := strconv.ParseFloat(orderbook.Bids[i][0], 64)
		if err == nil {
			sum += price
			count++
			bidPrices = append(bidPrices, price)
			log.Printf("[EXCHANGE]   Bid %d: %.0f Rial", i+1, price)
		} else {
			log.Printf("[EXCHANGE]   ⚠️ Failed to parse bid %d: %v", i+1, err)
		}
	}

	// Add ask prices
	log.Printf("[EXCHANGE] 🟢 Processing %d ask prices (max %d)", len(orderbook.Asks), n)
	for i := 0; i < n && i < len(orderbook.Asks); i++ {
		price, err := strconv.ParseFloat(orderbook.Asks[i][0], 64)
		if err == nil {
			sum += price
			count++
			askPrices = append(askPrices, price)
			log.Printf("[EXCHANGE]   Ask %d: %.0f Rial", i+1, price)
		} else {
			log.Printf("[EXCHANGE]   ⚠️ Failed to parse ask %d: %v", i+1, err)
		}
	}

	if count == 0 {
		log.Printf("[EXCHANGE] ❌ No valid price data available (bids: %d, asks: %d)", len(bidPrices), len(askPrices))
		return 0, fmt.Errorf("no price data available")
	}

	avg := sum / count
	avgToman := avg / 10 // Convert Rial to Toman (1 Toman = 10 Rial)

	log.Printf("[EXCHANGE] 💰 Price Calculation: sum=%.0f Rial, count=%.0f, avg=%.0f Rial (%.0f Toman)",
		sum, count, avg, avgToman)
	log.Printf("[EXCHANGE] ✅ USDT Price from Nobitex: %.0f Rial (%.0f Toman)", avg, avgToman)

	return avgToman, nil
}

// UpdateUSDTRateInDB updates USDT rate in database
func UpdateUSDTRateInDB(db *gorm.DB, price float64) error {
	log.Printf("[EXCHANGE] 💾 Updating USDT rate in database: %.0f Toman", price)

	var rate Rate
	err := db.Where("asset = ?", "USDT").First(&rate).Error

	if err == gorm.ErrRecordNotFound {
		// Create new rate if doesn't exist
		log.Printf("[EXCHANGE] 📝 Creating new USDT rate record (first time)")
		rate = Rate{
			Asset: "USDT",
			Value: price,
		}
		if err := db.Create(&rate).Error; err != nil {
			log.Printf("[EXCHANGE] ❌ Failed to create USDT rate: %v", err)
			return err
		}
		log.Printf("[EXCHANGE] ✅ Created new USDT rate: ID=%d, price=%.0f Toman", rate.ID, rate.Value)
		return nil
	} else if err != nil {
		log.Printf("[EXCHANGE] ❌ Database error while fetching USDT rate: %v", err)
		return err
	}

	// Update existing rate
	oldPrice := rate.Value
	rate.Value = price
	if err := db.Save(&rate).Error; err != nil {
		log.Printf("[EXCHANGE] ❌ Failed to update USDT rate: %v", err)
		return err
	}
	log.Printf("[EXCHANGE] ✅ Updated USDT rate: ID=%d, %.0f → %.0f Toman (Δ%.0f)",
		rate.ID, oldPrice, price, price-oldPrice)
	return nil
}

// AutoUpdateUSDTPrice runs in background and updates USDT price every 3 minutes
func AutoUpdateUSDTPrice(db *gorm.DB, interval time.Duration) {
	log.Printf("[EXCHANGE] 🚀 Starting auto USDT price update service (interval: %v)", interval)

	// Run immediately on startup (in goroutine to not block)
	go func() {
		log.Printf("[EXCHANGE] ⏳ Waiting 2 seconds for database to be fully ready...")
		// Wait a bit for database to be fully ready
		time.Sleep(2 * time.Second)
		log.Printf("[EXCHANGE] 🔄 Running initial USDT price update...")
		updatePrice(db)
	}()

	// Then run every interval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	updateCount := 0
	for range ticker.C {
		updateCount++
		log.Printf("[EXCHANGE] ⏰ Scheduled update #%d (every %v)", updateCount, interval)
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

	log.Printf("[EXCHANGE] 🔄 Starting USDT price update cycle...")

	// Get old price for logging
	var oldRate Rate
	var oldPrice float64
	if err := db.Where("asset = ?", "USDT").First(&oldRate).Error; err == nil {
		if oldRate.ID != 0 {
			oldPrice = oldRate.Value
			log.Printf("[EXCHANGE] 📊 Current USDT rate in DB: ID=%d, price=%.0f Toman", oldRate.ID, oldPrice)
		}
	} else {
		log.Printf("[EXCHANGE] ℹ️ No existing USDT rate found in database (will create new)")
	}

	log.Printf("[EXCHANGE] 🌐 Fetching latest USDT price from Nobitex...")
	price, err := GetUSDTPriceFromNobitex()
	if err != nil {
		log.Printf("[EXCHANGE] ❌ Error fetching USDT price from Nobitex: %v", err)
		return
	}

	log.Printf("[EXCHANGE] 💾 Saving USDT price to database...")
	err = UpdateUSDTRateInDB(db, price)
	if err != nil {
		log.Printf("[EXCHANGE] ❌ Error updating USDT rate in database: %v", err)
		return
	}

	// Log the auto update summary
	if oldPrice > 0 {
		change := price - oldPrice
		changePercent := (change / oldPrice) * 100
		log.Printf("[EXCHANGE] ✅ USDT price update completed: %.0f → %.0f Toman (Δ%.0f, %.2f%%)",
			oldPrice, price, change, changePercent)
	} else {
		log.Printf("[EXCHANGE] ✅ USDT price set for first time: %.0f Toman", price)
	}
}
