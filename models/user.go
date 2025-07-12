package models

import (
	"fmt"
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

	// Split by spaces and check for at least 2 parts (first name and last name)
	parts := strings.Fields(fullName)
	if len(parts) < 2 {
		return false
	}

	// Check each part has at least 2 characters and contains non-Latin characters
	for _, part := range parts {
		trimmedPart := strings.TrimSpace(part)
		if len(trimmedPart) < 2 {
			return false
		}

		// Check if the part contains non-Latin characters (Persian/Arabic)
		hasNonLatin := false
		for _, char := range trimmedPart {
			if char > 127 { // Non-ASCII characters (Persian/Arabic)
				hasNonLatin = true
				break
			}
		}
		if !hasNonLatin {
			return false
		}
	}

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

	// Pattern for Iranian Sheba: IR + 22 digits
	// Example: IR520630144905901219088011
	pattern := `^IR\d{22}$`
	matched, err := regexp.MatchString(pattern, sheba)

	// Debug logging
	fmt.Printf("Sheba validation: input='%s', length=%d, matched=%v, err=%v\n",
		sheba, len(sheba), matched, err)

	// Additional debug: show each character
	if !matched {
		fmt.Printf("Sheba characters: ")
		for i, char := range sheba {
			fmt.Printf("'%c'(%d) ", char, char)
			if i > 30 { // Limit output
				fmt.Printf("...")
				break
			}
		}
		fmt.Printf("\n")
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
