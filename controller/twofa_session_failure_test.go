package controller

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	ginSessions "github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

type failingSaveSession struct {
	values map[interface{}]interface{}
}

func (*failingSaveSession) ID() string { return "failing-save-session" }

func (session *failingSaveSession) Get(key interface{}) interface{} {
	return session.values[key]
}

func (session *failingSaveSession) Set(key interface{}, value interface{}) {
	session.values[key] = value
}

func (session *failingSaveSession) Delete(key interface{}) {
	delete(session.values, key)
}

func (session *failingSaveSession) Clear() {
	clear(session.values)
}

func (*failingSaveSession) AddFlash(interface{}, ...string) {}

func (*failingSaveSession) Flashes(...string) []interface{} { return nil }

func (*failingSaveSession) Options(ginSessions.Options) {}

func (*failingSaveSession) Save() error {
	return errors.New("forced session save failure")
}

func setupTwoFASessionFailureTest(
	t *testing.T,
	enabled bool,
) (*model.User, string) {
	t.Helper()

	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.TwoFA{},
		&model.TwoFABackupCode{},
		&model.Log{},
	))

	user := &model.User{
		Id:       71001,
		Username: "twofa-session-save-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(user).Error)

	key, err := common.GenerateTOTPSecret(user.Username)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.TwoFA{
		UserId:    user.Id,
		Secret:    key.Secret(),
		IsEnabled: enabled,
	}).Error)
	if enabled {
		require.NoError(t, model.CreateBackupCodes(user.Id, []string{"OLD1-CODE"}))
	}

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	require.NoError(t, err)
	return user, code
}

func performTwoFARequestWithFailingSessionSave(
	t *testing.T,
	userID int,
	code string,
	handler gin.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(ginSessions.DefaultKey, &failingSaveSession{
			values: map[interface{}]interface{}{},
		})
		c.Next()
	})
	router.POST("/twofa", func(c *gin.Context) {
		c.Set("id", userID)
		handler(c)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/twofa",
		bytes.NewReader([]byte(`{"code":"`+code+`"}`)),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestEnable2FAReturnsCommittedSuccessWhenSessionPreservationFails(t *testing.T) {
	user, code := setupTwoFASessionFailureTest(t, false)

	recorder := performTwoFARequestWithFailingSessionSave(t, user.Id, code, Enable2FA)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	twoFA, err := model.GetTwoFAByUserId(user.Id)
	require.NoError(t, err)
	require.NotNil(t, twoFA)
	require.True(t, twoFA.IsEnabled)
}

func TestDisable2FAReturnsCommittedSuccessWhenSessionPreservationFails(t *testing.T) {
	user, code := setupTwoFASessionFailureTest(t, true)

	recorder := performTwoFARequestWithFailingSessionSave(t, user.Id, code, Disable2FA)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	twoFA, err := model.GetTwoFAByUserId(user.Id)
	require.NoError(t, err)
	require.Nil(t, twoFA)
}

func TestRegenerateBackupCodesReturnsCodesWhenSessionPreservationFails(t *testing.T) {
	user, code := setupTwoFASessionFailureTest(t, true)

	recorder := performTwoFARequestWithFailingSessionSave(
		t,
		user.Id,
		code,
		RegenerateBackupCodes,
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	require.Contains(t, recorder.Body.String(), `"backup_codes"`)
	var stored model.User
	require.NoError(t, model.DB.First(&stored, user.Id).Error)
	require.EqualValues(t, 1, stored.SessionGeneration)
}
