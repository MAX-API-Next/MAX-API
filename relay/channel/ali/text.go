package ali

import (
	"github.com/MAX-API-Next/MAX-API/dto"
)

// https://help.aliyun.com/document_detail/613695.html?spm=a2c4g.2399480.0.0.1adb778fAdzP9w#341800c0f8w0r

const EnableSearchModelSuffix = "-internet"

func requestOpenAI2Ali(request dto.GeneralOpenAIRequest) *dto.GeneralOpenAIRequest {
	if request.TopP != nil {
		if *request.TopP >= 1 {
			value := 0.99
			request.TopP = &value
		} else if *request.TopP <= 0 {
			value := 0.01
			request.TopP = &value
		}
	}
	return &request
}
