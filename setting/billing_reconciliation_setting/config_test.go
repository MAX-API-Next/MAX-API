package billing_reconciliation_setting

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/stretchr/testify/assert"
)

func TestDefaultBlockingPolicyAllowsPaidRequests(t *testing.T) {
	registered := config.GlobalConfig.ExportAllConfigs()

	assert.Equal(t, "false", registered[OptionKeyBlockUserByDefault])

	restoreOptionMap := replaceOptionMapForTest(map[string]string{})
	t.Cleanup(restoreOptionMap)
	assert.False(t, BlockUserByDefault())
}

func TestBlockUserByDefaultParsesRuntimeOptionFailClosed(t *testing.T) {
	restoreOptionMap := replaceOptionMapForTest(map[string]string{
		OptionKeyBlockUserByDefault: "true",
	})
	t.Cleanup(restoreOptionMap)

	assert.True(t, BlockUserByDefault())

	common.OptionMapRWMutex.Lock()
	common.OptionMap[OptionKeyBlockUserByDefault] = "false"
	common.OptionMapRWMutex.Unlock()
	assert.False(t, BlockUserByDefault())

	common.OptionMapRWMutex.Lock()
	common.OptionMap[OptionKeyBlockUserByDefault] = "not-a-boolean"
	common.OptionMapRWMutex.Unlock()
	assert.True(t, BlockUserByDefault())
}

func replaceOptionMapForTest(replacement map[string]string) func() {
	common.OptionMapRWMutex.Lock()
	original := common.OptionMap
	common.OptionMap = replacement
	common.OptionMapRWMutex.Unlock()
	return func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = original
		common.OptionMapRWMutex.Unlock()
	}
}
