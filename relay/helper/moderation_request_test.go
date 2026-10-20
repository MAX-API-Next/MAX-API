package helper

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	relayconstant "github.com/MAX-API-Next/MAX-API/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetAndValidateTextRequestDefaultsOmittedModerationModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/moderations", bytes.NewBufferString(`{"input":"check this"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })

	request, err := GetAndValidateTextRequest(ctx, relayconstant.RelayModeModerations)
	require.NoError(t, err)
	require.Equal(t, "omni-moderation-latest", request.Model)
}
