package types

type TaskBillingInput struct {
	Model         string             `json:"model,omitempty"`
	UpstreamModel string             `json:"upstream_model,omitempty"`
	Action        string             `json:"action,omitempty"`
	Platform      string             `json:"platform,omitempty"`
	Fields        map[string]string  `json:"fields,omitempty"`
	Numbers       map[string]float64 `json:"numbers,omitempty"`
}

type TaskBillingTraceItem struct {
	Name  string  `json:"name"`
	Value string  `json:"value,omitempty"`
	Unit  string  `json:"unit,omitempty"`
	Price float64 `json:"price,omitempty"`
}

type TaskBillingResult struct {
	RuleKey        string                 `json:"rule_key,omitempty"`
	RowID          string                 `json:"row_id,omitempty"`
	Unit           string                 `json:"unit,omitempty"`
	Quantity       float64                `json:"quantity,omitempty"`
	UnitPrice      float64                `json:"unit_price,omitempty"`
	TotalPrice     float64                `json:"total_price,omitempty"`
	Quota          int                    `json:"quota,omitempty"`
	Fields         map[string]string      `json:"fields,omitempty"`
	Matched        map[string]string      `json:"matched,omitempty"`
	Trace          []TaskBillingTraceItem `json:"trace,omitempty"`
	PerCallBilling bool                   `json:"per_call_billing,omitempty"`
}

func (i *TaskBillingInput) SetField(key, value string) {
	if i.Fields == nil {
		i.Fields = make(map[string]string)
	}
	i.Fields[key] = value
}

func (i *TaskBillingInput) SetNumber(key string, value float64) {
	if i.Numbers == nil {
		i.Numbers = make(map[string]float64)
	}
	i.Numbers[key] = value
}
