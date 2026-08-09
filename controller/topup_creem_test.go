package controller

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/setting"
	"github.com/stretchr/testify/require"
)

func TestCreemTopUpPaymentValidationUsesConfiguredProductCurrency(t *testing.T) {
	originalProducts := setting.CreemProducts
	t.Cleanup(func() { setting.CreemProducts = originalProducts })
	setting.CreemProducts = `[{"productId":"prod-usd","currency":"USD"}]`

	event := &CreemWebhookEvent{}
	event.Object.Product.Id = "prod-usd"
	event.Object.Order.AmountPaid = 1234
	event.Object.Order.Currency = "usd"

	validation, err := creemTopUpPaymentValidation(event)
	require.NoError(t, err)
	require.EqualValues(t, 1234, validation.PaidAmountMinor)
	require.Equal(t, "USD", validation.Currency)
	require.Equal(t, "USD", validation.ExpectedCurrency)
}

func TestCreemTopUpPaymentValidationRejectsUnknownProductCurrency(t *testing.T) {
	originalProducts := setting.CreemProducts
	t.Cleanup(func() { setting.CreemProducts = originalProducts })
	setting.CreemProducts = `[{"productId":"configured-product","currency":"USD"}]`

	event := &CreemWebhookEvent{}
	event.Object.Product.Id = "unknown-product"
	event.Object.Product.Currency = "EUR"
	event.Object.Order.AmountPaid = 1234
	event.Object.Order.Currency = "EUR"

	_, err := creemTopUpPaymentValidation(event)
	require.Error(t, err)
	require.Contains(t, err.Error(), "currency is not configured")
}
