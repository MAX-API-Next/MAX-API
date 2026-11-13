package controller

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	appi18n "github.com/MAX-API-Next/MAX-API/i18n"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

func TestEmailBindingRejectsRevokedSessionAtWriteBoundary(t *testing.T) {
	db := setupUserSettingControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.CacheInvalidationTask{}))
	user := model.User{Username: "binding-audit", Email: "old@example.com", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, SessionGeneration: 1}
	require.NoError(t, db.Create(&user).Error)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("binding-session-test"))))
	r.GET("/cookie", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("id", user.Id)
		s.Set("session_generation", int64(0))
		require.NoError(t, s.Save())
		c.Status(204)
	})
	r.POST("/bind", EmailBind)
	login := httptest.NewRecorder()
	r.ServeHTTP(login, httptest.NewRequest("GET", "/cookie", nil))
	common.RegisterVerificationCodeWithKey("new@example.com", "123456", common.EmailVerificationPurpose)
	req := httptest.NewRequest("POST", "/bind", strings.NewReader(`{"email":"new@example.com","code":"123456"}`))
	req.Header.Set("Content-Type", "application/json")
	for _, ck := range login.Result().Cookies() {
		req.AddCookie(ck)
	}
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	require.Contains(t, res.Body.String(), `"success":false`)
	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, "old@example.com", stored.Email)
}

func TestPasswordLoginFailsClosedWhenFactorLookupFails(t *testing.T) {
	require.NoError(t, appi18n.Init())
	db := setupUserSettingControllerTestDB(t)
	oldEnabled := common.PasswordLoginEnabled
	common.PasswordLoginEnabled = true
	t.Cleanup(func() { common.PasswordLoginEnabled = oldEnabled })
	hash, err := common.Password2Hash("synthetic-password")
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.User{Username: "factor-db-error", Password: hash, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default"}).Error)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("factor-error-test"))))
	r.POST("/login", Login)
	q := httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"factor-db-error","password":"synthetic-password"}`))
	q.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	require.Contains(t, w.Body.String(), `"success":false`, "missing factor table is a query failure, not proof 2FA is disabled")
}

func TestTwoFALoginChallengeRejectsRevocationExpiryAndReplay(t *testing.T) {
	require.NoError(t, appi18n.Init())
	for _, scenario := range []string{"revoked", "expired", "replayed"} {
		t.Run(scenario, func(t *testing.T) {
			db := setupUserSettingControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.AuthFlow{}, &model.TwoFA{}, &model.TwoFABackupCode{}, &model.Log{}, &model.CacheInvalidationTask{}))
			oldEnabled := common.PasswordLoginEnabled
			common.PasswordLoginEnabled = true
			t.Cleanup(func() { common.PasswordLoginEnabled = oldEnabled })
			hash, err := common.Password2Hash("synthetic-password")
			require.NoError(t, err)
			user := model.User{Username: "pending-audit", Password: hash, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default"}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, db.Create(&model.TwoFA{UserId: user.Id, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true}).Error)
			r := gin.New()
			r.Use(sessions.Sessions("session", cookie.NewStore([]byte("pending-session-test"))))
			r.POST("/login", Login)
			r.POST("/verify", Verify2FALogin)
			login := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"pending-audit","password":"synthetic-password"}`))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(login, req)
			require.Contains(t, login.Body.String(), `"require_2fa":true`)
			if scenario == "revoked" {
				require.NoError(t, db.Model(&user).Update("session_generation", 1).Error)
			}
			if scenario == "expired" {
				require.NoError(t, db.Model(&model.AuthFlow{}).Where("user_id = ? AND purpose = ?", user.Id, model.AuthFlowPurposeTwoFALogin).Update("expires_at", time.Now().Add(-time.Minute)).Error)
			}
			perform := func() *httptest.ResponseRecorder {
				code, err := totp.GenerateCode("JBSWY3DPEHPK3PXP", time.Now())
				require.NoError(t, err)
				q := httptest.NewRequest("POST", "/verify", strings.NewReader(`{"code":"`+code+`"}`))
				q.Header.Set("Content-Type", "application/json")
				for _, ck := range login.Result().Cookies() {
					q.AddCookie(ck)
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, q)
				return w
			}
			if scenario == "replayed" {
				require.Contains(t, perform().Body.String(), `"success":true`)
			}
			require.Contains(t, perform().Body.String(), `"success":false`)
		})
	}
}
