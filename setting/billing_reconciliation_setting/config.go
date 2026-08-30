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
	BlockUserByDefault: true,
}

func init() {
	config.GlobalConfig.Register("billing_reconciliation_setting", &billingReconciliationSetting)
}

// BlockUserByDefault returns the persisted admission policy. Reading through
// OptionMap keeps the hot path synchronized with runtime option updates while
// retaining a fail-closed default before options are initialized.
func BlockUserByDefault() bool {
	common.OptionMapRWMutex.RLock()
	raw, ok := common.OptionMap[OptionKeyBlockUserByDefault]
	common.OptionMapRWMutex.RUnlock()
	if !ok {
		return true
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return true
	}
	return enabled
}
