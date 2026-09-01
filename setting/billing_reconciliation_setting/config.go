package billing_reconciliation_setting

import (
	"strconv"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/config"
)

const OptionKeyBlockUserByDefault = "billing_reconciliation_setting.block_user_by_default"

type BillingReconciliationSetting struct {
	BlockUserByDefault bool `json:"block_user_by_default"`
}

var billingReconciliationSetting = BillingReconciliationSetting{
	BlockUserByDefault: false,
}

func init() {
	config.GlobalConfig.Register("billing_reconciliation_setting", &billingReconciliationSetting)
}

// BlockUserByDefault returns the persisted admission policy. Reading through
// OptionMap keeps the hot path synchronized with runtime option updates. New
// installations allow paid requests by default; malformed persisted values
// still fail closed instead of silently weakening an explicit policy.
func BlockUserByDefault() bool {
	common.OptionMapRWMutex.RLock()
	raw, ok := common.OptionMap[OptionKeyBlockUserByDefault]
	common.OptionMapRWMutex.RUnlock()
	if !ok {
		return false
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return true
	}
	return enabled
}
