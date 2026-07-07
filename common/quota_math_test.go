package common

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQuotaFromFloat(t *testing.T) {
	assert.Equal(t, 42, QuotaFromFloat(42.4))
	assert.Equal(t, -42, QuotaFromFloat(-42.4))
	assert.Equal(t, math.MaxInt32, QuotaFromFloat(2000*1.8446744073686647e19))
	assert.Equal(t, math.MinInt32, QuotaFromFloat(-2000*1.8446744073686647e19))
	assert.Equal(t, math.MaxInt32, QuotaFromFloat(math.Inf(1)))
	assert.Equal(t, math.MinInt32, QuotaFromFloat(math.Inf(-1)))
	assert.Equal(t, 0, QuotaFromFloat(math.NaN()))
}
