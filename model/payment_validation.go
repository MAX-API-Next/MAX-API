package model

import (
	"errors"
	"strings"

	"github.com/shopspring/decimal"
)

var (
	ErrPaymentAmountMismatch   = errors.New("payment amount mismatch")
	ErrPaymentCurrencyMismatch = errors.New("payment currency mismatch")
)

type PaymentValidation struct {
	HasAmount        bool
	PaidAmountMinor  int64
	Currency         string
	ExpectedCurrency string
	AllowDiscount    bool
}

func PaymentValidationFromMinorUnits(amountMinor int64, currency string, expectedCurrency string, allowDiscount bool) PaymentValidation {
	return PaymentValidation{
		HasAmount:        true,
		PaidAmountMinor:  amountMinor,
		Currency:         normalizePaymentCurrency(currency),
		ExpectedCurrency: normalizePaymentCurrency(expectedCurrency),
		AllowDiscount:    allowDiscount,
	}
}

func PaymentValidationFromMajorString(amount string, currency string, expectedCurrency string, allowDiscount bool) PaymentValidation {
	paid, err := decimal.NewFromString(strings.TrimSpace(amount))
	if err != nil {
		return PaymentValidation{
			HasAmount:        true,
			PaidAmountMinor:  -1,
			Currency:         normalizePaymentCurrency(currency),
			ExpectedCurrency: normalizePaymentCurrency(expectedCurrency),
			AllowDiscount:    allowDiscount,
		}
	}
	return PaymentValidationFromMinorUnits(paid.Mul(decimal.NewFromInt(100)).Round(0).IntPart(), currency, expectedCurrency, allowDiscount)
}

func normalizePaymentCurrency(currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency))
}

func validatePaymentAgainstExpectedAmount(expectedAmount float64, validation PaymentValidation) error {
	if validation.ExpectedCurrency != "" && validation.Currency != "" && validation.Currency != validation.ExpectedCurrency {
		return ErrPaymentCurrencyMismatch
	}
	if !validation.HasAmount {
		return nil
	}
	if validation.PaidAmountMinor <= 0 {
		return ErrPaymentAmountMismatch
	}
	expectedMinor := decimal.NewFromFloat(expectedAmount).Mul(decimal.NewFromInt(100)).Round(0).IntPart()
	if expectedMinor <= 0 {
		return ErrPaymentAmountMismatch
	}
	if validation.AllowDiscount {
		if validation.PaidAmountMinor > expectedMinor {
			return ErrPaymentAmountMismatch
		}
		return nil
	}
	if validation.PaidAmountMinor != expectedMinor {
		return ErrPaymentAmountMismatch
	}
	return nil
}

func validatePaymentAgainstTopUp(topUp *TopUp, validations []PaymentValidation) error {
	if topUp == nil {
		return ErrTopUpNotFound
	}
	for _, validation := range validations {
		if err := validatePaymentAgainstExpectedAmount(topUp.Money, validation); err != nil {
			return err
		}
	}
	return nil
}

func validatePaymentAgainstSubscriptionOrder(order *SubscriptionOrder, plan *SubscriptionPlan, validations []PaymentValidation) error {
	if order == nil {
		return ErrSubscriptionOrderNotFound
	}
	expectedCurrency := ""
	if plan != nil {
		expectedCurrency = normalizePaymentCurrency(plan.Currency)
	}
	for _, validation := range validations {
		if validation.ExpectedCurrency == "" {
			validation.ExpectedCurrency = expectedCurrency
		}
		if err := validatePaymentAgainstExpectedAmount(order.Money, validation); err != nil {
			return err
		}
	}
	return nil
}
