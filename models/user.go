package models

import (
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
}

// ValidatePersianFullName validates Persian full name format
func ValidatePersianFullName(fullName string) bool {
	// Remove extra spaces
	fullName = strings.TrimSpace(fullName)

	// Check if empty
	if fullName == "" {
		return false
	}

	// Pattern for Persian characters (including ی and ک)
	// Persian Unicode range: \u0600-\u06FF, \uFB50-\uFDFF, \uFE70-\uFEFF
	persianPattern := `^[\u0600-\u06FF\uFB50-\uFDFF\uFE70-\uFEFF\s]+$`
	matched, _ := regexp.MatchString(persianPattern, fullName)
	if !matched {
		return false
	}

	// Split by spaces and check for at least 2 parts (first name and last name)
	parts := strings.Fields(fullName)
	if len(parts) < 2 {
		return false
	}

	// Check each part has at least 2 characters
	for _, part := range parts {
		if len(strings.TrimSpace(part)) < 2 {
			return false
		}
	}

	return true
}

// ValidateSheba validates Iranian Sheba number format
func ValidateSheba(sheba string) bool {
	// Pattern for Iranian Sheba: IR + 2 digits + 3 digits + 16 digits
	// Example: IR520630144905901219088011
	pattern := `^IR\d{22}$`
	matched, _ := regexp.MatchString(pattern, sheba)
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
