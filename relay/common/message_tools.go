package common

import (
	"fmt"
	"reflect"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
)

// CaptureMessageTools snapshots declarations before an adaptor can mutate the
// request. The ordinal is part of the contract: tools become available at that
// point in the conversation, not as unconditional top-level tools.
func CaptureMessageTools(request *dto.GeneralOpenAIRequest) (map[int]any, error) {
	declarations := map[int]any{}
	for index, message := range request.Messages {
		if len(message.Tools) == 0 {
			continue
		}
		var value any
		if err := common.Unmarshal(message.Tools, &value); err != nil {
			return nil, err
		}
		declarations[index] = value
	}
	return declarations, nil
}

func ValidateMessageToolsConversion(declarations map[int]any, converted any) error {
	if len(declarations) == 0 {
		return nil
	}
	data, err := common.Marshal(converted)
	if err != nil {
		return err
	}
	var request dto.GeneralOpenAIRequest
	if err := common.Unmarshal(data, &request); err != nil {
		return fmt.Errorf("message-scoped tools cannot be converted to this upstream protocol")
	}
	outgoing, err := CaptureMessageTools(&request)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(declarations, outgoing) {
		return fmt.Errorf("message-scoped tools require an OpenAI-compatible Chat Completions upstream; this conversion would lose or move tool declarations")
	}
	return nil
}
