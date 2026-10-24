package billing

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const MaxMinorUnits int64 = 9_999_999_999_999

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func ParseMoney(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") {
		return 0, fmt.Errorf("amount must be a nonnegative decimal with at most two fractional digits")
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || !digits(parts[0]) {
		return 0, fmt.Errorf("invalid amount")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if len(fraction) > 2 || !digits(fraction) {
			return 0, fmt.Errorf("amount supports at most two fractional digits")
		}
	}
	if len(parts[0]) > 11 {
		return 0, fmt.Errorf("amount exceeds supported precision")
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > MaxMinorUnits/100 {
		return 0, fmt.Errorf("amount exceeds supported precision")
	}
	fraction += strings.Repeat("0", 2-len(fraction))
	minorFraction, _ := strconv.ParseInt(fraction, 10, 64)
	minor := whole*100 + minorFraction
	if minor > MaxMinorUnits {
		return 0, fmt.Errorf("amount exceeds supported precision")
	}
	return minor, nil
}

func FormatMoney(minor int64) string {
	if minor < 0 {
		return ""
	}
	return fmt.Sprintf("%d.%02d", minor/100, minor%100)
}

func ValidateCurrency(value string) bool {
	return currencyPattern.MatchString(value)
}

func LineTotal(unitPrice int64, quantity int) (int64, error) {
	if unitPrice < 0 || quantity < 1 || quantity > 10000 || int64(quantity) > math.MaxInt64/max(unitPrice, 1) {
		return 0, fmt.Errorf("invalid quantity or unit price")
	}
	total := unitPrice * int64(quantity)
	if total > MaxMinorUnits {
		return 0, fmt.Errorf("line total exceeds supported precision")
	}
	return total, nil
}

func AddLine(subtotal, unitPrice int64, quantity int) (int64, int64, error) {
	lineTotal, err := LineTotal(unitPrice, quantity)
	if err != nil {
		return 0, 0, err
	}
	if subtotal < 0 || subtotal > MaxMinorUnits-lineTotal {
		return 0, 0, fmt.Errorf("invoice total exceeds supported precision")
	}
	return lineTotal, subtotal + lineTotal, nil
}

func digits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
