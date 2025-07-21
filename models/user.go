package models

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Username   string `gorm:"size:255"`
	TelegramID int64  `gorm:"uniqueIndex"`
	FullName   string `gorm:"size:255"`
	Sheba      string `gorm:"size:32"`
	CardNumber string `gorm:"size:32"`
	Registered bool   `gorm:"default:false"`

	ReferrerID     *uint   `gorm:"index"`     // ID of the user who referred this user (nullable)
	ReferralReward float64 `gorm:"default:0"` // Total earned from referrals

	// Wallet fields (plain text)
	ERC20Address  string  `gorm:"size:64"`
	ERC20Mnemonic string  `gorm:"size:256"`
	ERC20PrivKey  string  `gorm:"size:128"`
	ERC20Balance  float64 `gorm:"default:0"` // موجودی ERC20

	BEP20Address  string  `gorm:"size:64"`
	BEP20Mnemonic string  `gorm:"size:256"`
	BEP20PrivKey  string  `gorm:"size:128"`
	BEP20Balance  float64 `gorm:"default:0"` // موجودی BEP20

	TradeBalance  float64 `gorm:"default:0"` // سود/ضرر تریدها (در ربات)
	RewardBalance float64 `gorm:"default:0"` // پاداش‌ها (در ربات)
}

func logDebug(format string, v ...interface{}) {
	log.Printf("[DEBUG] "+format, v...)
}

// ValidatePersianFullName validates Persian full name format
func ValidatePersianFullName(fullName string) bool {
	// Debug logging
	logDebug("Validating Persian full name: '%s'", fullName)

	// Remove extra spaces
	fullName = strings.TrimSpace(fullName)

	// Check if empty
	if fullName == "" {
		logDebug("Name is empty")
		return false
	}

	// Split by spaces and check for at least 2 parts (first name and last name)
	parts := strings.Fields(fullName)
	logDebug("Name parts: %v (count: %d)", parts, len(parts))
	if len(parts) < 2 {
		logDebug("Not enough parts (need at least 2)")
		return false
	}

	// Check each part has at least 2 characters and contains non-Latin characters
	for i, part := range parts {
		trimmedPart := strings.TrimSpace(part)
		logDebug("Part %d: '%s' (length: %d)", i+1, trimmedPart, len(trimmedPart))

		if len(trimmedPart) < 2 {
			logDebug("Part %d too short (length: %d)", i+1, len(trimmedPart))
			return false
		}

		// Check if the part contains non-Latin characters (Persian/Arabic)
		hasNonLatin := false
		for j, char := range trimmedPart {
			if char > 127 { // Non-ASCII characters (Persian/Arabic)
				hasNonLatin = true
				logDebug("Part %d, char %d: '%c' (code: %d) - non-Latin found", i+1, j+1, char, char)
				break
			}
		}
		if !hasNonLatin {
			logDebug("Part %d has no non-Latin characters", i+1)
			return false
		}
	}

	logDebug("Persian full name validation passed")
	return true
}

// ValidateSheba validates Iranian Sheba number format
func ValidateSheba(sheba string) bool {
	// Remove any spaces, tabs, newlines, and other whitespace
	sheba = strings.TrimSpace(sheba)

	// Remove any invisible characters
	sheba = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1 // Remove control characters
		}
		return r
	}, sheba)

	// Pattern for Iranian Sheba: IR + 24 digits (total 26 characters)
	// Example: IR520630144905901219088011
	pattern := `^IR\d{24}$`
	matched, err := regexp.MatchString(pattern, sheba)

	// Debug logging
	logDebug("Sheba validation: input='%s', length=%d, matched=%v, err=%v",
		sheba, len(sheba), matched, err)

	// Additional debug: show each character
	if !matched {
		charInfo := "Sheba characters: "
		for i, char := range sheba {
			charInfo += fmt.Sprintf("'%c'(%d) ", char, char)
			if i > 30 { // Limit output
				charInfo += "..."
				break
			}
		}
		logDebug(charInfo)
	}

	return matched
}

// ValidateCardNumber validates Iranian card number format
func ValidateCardNumber(cardNumber string) bool {
	// Pattern for Iranian card number: 16 digits
	// Example: 6037998215325563
	pattern := `^\d{16}$`
	matched, _ := regexp.MatchString(pattern, cardNumber)
	return matched
}
