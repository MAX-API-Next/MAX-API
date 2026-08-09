package dto

import (
	"encoding/json"
	"strconv"

	"github.com/MAX-API-Next/MAX-API/common"
)

type StringValue string

func (s *StringValue) UnmarshalJSON(data []byte) error {
	var str string
	if err := common.Unmarshal(data, &str); err == nil {
		*s = StringValue(str)
		return nil
	}

	var raw json.Number
	if err := common.Unmarshal(data, &raw); err == nil {
		*s = StringValue(raw.String())
		return nil
	}

	return common.Unmarshal(data, &str)
}

func (s StringValue) MarshalJSON() ([]byte, error) {
	return common.Marshal(string(s))
}

type IntValue int

func (i *IntValue) UnmarshalJSON(b []byte) error {
	var n int
	if err := common.Unmarshal(b, &n); err == nil {
		*i = IntValue(n)
		return nil
	}
	var s string
	if err := common.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*i = IntValue(v)
	return nil
}

func (i IntValue) MarshalJSON() ([]byte, error) {
	return common.Marshal(int(i))
}

type BoolValue bool

func (b *BoolValue) UnmarshalJSON(data []byte) error {
	var boolean bool
	if err := common.Unmarshal(data, &boolean); err == nil {
		*b = BoolValue(boolean)
		return nil
	}
	var str string
	if err := common.Unmarshal(data, &str); err != nil {
		return err
	}
	if str == "true" {
		*b = BoolValue(true)
	} else if str == "false" {
		*b = BoolValue(false)
	} else {
		return common.Unmarshal(data, &boolean)
	}
	return nil
}
func (b BoolValue) MarshalJSON() ([]byte, error) {
	return common.Marshal(bool(b))
}
