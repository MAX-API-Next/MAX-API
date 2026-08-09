package coze

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertCozeChatRequestPreservesExplicitFalseStream(t *testing.T) {
	stream := false
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := convertCozeChatRequest(c, dto.GeneralOpenAIRequest{Stream: &stream, User: json.RawMessage(`"user"`)})
	request.AutoSaveHistory = &stream

	data, err := common.Marshal(request)
	require.NoError(t, err)
	require.JSONEq(t, `{"bot_id":"","user_id":"user","stream":false,"auto_save_history":false}`, string(data))
}
