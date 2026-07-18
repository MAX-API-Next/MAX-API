package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchRedemptionsTreatsUnderscoreAsLiteral(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&Redemption{Name: "promo_code", Key: "underscore-key"}).Error)
	require.NoError(t, DB.Create(&Redemption{Name: "promoXcode", Key: "wildcard-key"}).Error)

	rows, total, err := SearchRedemptions("promo_", "", 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	assert.Equal(t, "promo_code", rows[0].Name)
}
