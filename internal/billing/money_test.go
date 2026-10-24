package billing

import "testing"

func TestParseMoneyUsesExactMinorUnits(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int64
	}{
		{value: "0", want: 0},
		{value: "12.3", want: 1230},
		{value: "12.34", want: 1234},
	} {
		got, err := ParseMoney(test.value)
		if err != nil || got != test.want || FormatMoney(got) != map[int64]string{0: "0.00", 1230: "12.30", 1234: "12.34"}[test.want] {
			t.Fatalf("ParseMoney(%q) = %d, %v", test.value, got, err)
		}
	}
}

func TestParseMoneyRejectsNegativeAndExcessPrecision(t *testing.T) {
	for _, value := range []string{"-1.00", "+1", "1.001", "1e2", "1,000", ".25", "100000000000.00"} {
		if _, err := ParseMoney(value); err == nil {
			t.Errorf("ParseMoney(%q) unexpectedly succeeded", value)
		}
	}
}

func TestLineTotalChecksQuantityAndOverflow(t *testing.T) {
	if total, err := LineTotal(125, 3); err != nil || total != 375 {
		t.Fatalf("LineTotal(125, 3) = %d, %v", total, err)
	}
	for _, quantity := range []int{0, -1, 10001} {
		if _, err := LineTotal(100, quantity); err == nil {
			t.Errorf("quantity %d unexpectedly succeeded", quantity)
		}
	}
	if _, err := LineTotal(MaxMinorUnits, 2); err == nil {
		t.Fatal("expected oversized line total to fail")
	}
}

func TestValidateCurrencyRequiresUppercaseThreeLetterCode(t *testing.T) {
	for _, value := range []string{"USD", "EUR"} {
		if !ValidateCurrency(value) {
			t.Errorf("%q rejected", value)
		}
	}
	for _, value := range []string{"usd", "US", "USDD", "1SD"} {
		if ValidateCurrency(value) {
			t.Errorf("%q accepted", value)
		}
	}
}

func TestAddLineAccumulatesExactInvoiceTotal(t *testing.T) {
	firstLine, subtotal, err := AddLine(0, 125, 3)
	if err != nil || firstLine != 375 || subtotal != 375 {
		t.Fatalf("first AddLine = (%d, %d, %v)", firstLine, subtotal, err)
	}
	secondLine, subtotal, err := AddLine(subtotal, 990, 2)
	if err != nil || secondLine != 1980 || subtotal != 2355 || FormatMoney(subtotal) != "23.55" {
		t.Fatalf("second AddLine = (%d, %d, %v)", secondLine, subtotal, err)
	}
	if _, _, err := AddLine(MaxMinorUnits, 1, 1); err == nil {
		t.Fatal("expected aggregate overflow to fail")
	}
}
