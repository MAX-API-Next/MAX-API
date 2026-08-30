package billing_reconciliation_setting

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/stretchr/testify/assert"
)

func TestDefaultBlockingPolicyIsRegisteredFailClosed(t *testing.T) {
	registered := config.GlobalConfig.ExportAllConfigs()

	assert.Equal(t, "true", registered[OptionKeyBlockUserByDefault])
	assert.True(t, BlockUserByDefault())
}
