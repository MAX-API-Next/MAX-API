package relay

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWriteMidjourneyStatusCodeUsesChannelMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("status_code_mapping", `{"429":"503"}`)

	writeMidjourneyStatusCode(c, http.StatusTooManyRequests)

	require.Equal(t, http.StatusServiceUnavailable, c.Writer.Status())
}

func TestRelayMidjourneyImageRejectsUnsignedAndWrongUserURLs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB := model.DB
	oldSecret := common.CryptoSecret
	common.CryptoSecret = "midjourney-handler-test-secret"
	db, err := gorm.Open(sqlite.Open("file:mjproxy_auth?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))
	require.NoError(t, db.Create(&model.Midjourney{
		Id: 1, UserId: 42, MjId: "mj-private-task", ImageUrl: "https://example.com/private.png",
	}).Error)
	t.Cleanup(func() {
		model.DB = oldDB
		common.CryptoSecret = oldSecret
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	router := gin.New()
	router.GET("/mj/image/:id", RelayMidjourneyImage)

	unsigned := httptest.NewRecorder()
	router.ServeHTTP(unsigned, httptest.NewRequest(http.MethodGet, "/mj/image/mj-private-task", nil))
	require.Equal(t, http.StatusForbidden, unsigned.Code)
	require.Contains(t, unsigned.Body.String(), "midjourney_image_authorization_failed")

	now := time.Now()
	expiresAt := now.Add(service.MidjourneyImageURLTTL).Unix()
	wrongUserURL := "/mj/image/mj-private-task?uid=43&expires=" +
		strconv.FormatInt(expiresAt, 10) + "&signature=" +
		service.SignMidjourneyImageURL("mj-private-task", 43, expiresAt)
	wrongUser := httptest.NewRecorder()
	router.ServeHTTP(wrongUser, httptest.NewRequest(http.MethodGet, wrongUserURL, nil))
	require.Equal(t, http.StatusForbidden, wrongUser.Code)
	require.Contains(t, wrongUser.Body.String(), "midjourney_image_authorization_failed")
}
