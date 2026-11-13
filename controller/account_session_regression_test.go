package controller

import (
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
	"gorm.io/gorm"
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
	r.ServeHTTP(login, httptest.NewRequestWithContext(t.Context(), "GET", "/cookie", nil))
	common.RegisterVerificationCodeWithKey("new@example.com", "123456", common.EmailVerificationPurpose)
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/bind", strings.NewReader(`{"email":"new@example.com","code":"123456"}`))
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

func TestTwoFABackupLoginCommitsWithChallenge(t *testing.T) {
	require.NoError(t, appi18n.Init())
	for _, scenario := range []string{"expired", "revoked", "consumed", "backup-write-failed", "usage-write-failed", "wrong-code", "success"} {
		t.Run(scenario, func(t *testing.T) {
			db := setupUserSettingControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.AuthFlow{}, &model.TwoFA{}, &model.TwoFABackupCode{}, &model.Log{}))
			oldEnabled := common.PasswordLoginEnabled
			common.PasswordLoginEnabled = true
			t.Cleanup(func() { common.PasswordLoginEnabled = oldEnabled })
			hash, err := common.Password2Hash("synthetic-password")
			require.NoError(t, err)
			user := model.User{Username: "backup-audit", Password: hash, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default"}
			require.NoError(t, db.Create(&user).Error)
			factor := model.TwoFA{UserId: user.Id, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true, FailedAttempts: 2}
			require.NoError(t, db.Create(&factor).Error)
			const code = "ABCD-EFGH"
			require.NoError(t, model.CreateBackupCodes(user.Id, []string{code}))
			r := gin.New()
			r.Use(sessions.Sessions("session", cookie.NewStore([]byte("backup-session-test"))))
			r.POST("/login", Login)
			r.POST("/verify", Verify2FALogin)
			login := httptest.NewRecorder()
			q := httptest.NewRequestWithContext(t.Context(), "POST", "/login", strings.NewReader(`{"username":"backup-audit","password":"synthetic-password"}`))
			q.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(login, q)
			require.Contains(t, login.Body.String(), `"require_2fa":true`)
			// Inject a competing change after the challenge preflight but before
			// the factor is verified. This is the controller's real ordering.
			changed := false
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:backup_preflight", func(tx *gorm.DB) {
				if changed || tx.Statement.Table != "two_fas" {
					return
				}
				changed = true
				fresh := tx.Session(&gorm.Session{NewDB: true})
				var err error
				switch scenario {
				case "expired":
					err = fresh.Model(&model.AuthFlow{}).Where("user_id = ?", user.Id).Update("expires_at", time.Now().Add(-time.Minute)).Error
				case "revoked":
					err = fresh.Model(&model.User{}).Where("id = ?", user.Id).Update("session_generation", 1).Error
				case "consumed":
					err = fresh.Model(&model.AuthFlow{}).Where("user_id = ?", user.Id).Update("consumed_at", time.Now()).Error
				}
				if err != nil {
					tx.AddError(err)
				}
			}))
			if scenario == "usage-write-failed" || scenario == "backup-write-failed" {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:backup_usage_failure", func(tx *gorm.DB) {
					if (scenario == "usage-write-failed" && tx.Statement.Table == "two_fas") ||
						(scenario == "backup-write-failed" && tx.Statement.Table == "two_fa_backup_codes") {
						tx.AddError(errors.New("synthetic usage write failed"))
					}
				}))
			}
			perform := func(input string) *httptest.ResponseRecorder {
				q := httptest.NewRequestWithContext(t.Context(), "POST", "/verify", strings.NewReader(`{"code":"`+input+`"}`))
				q.Header.Set("Content-Type", "application/json")
				for _, ck := range login.Result().Cookies() {
					q.AddCookie(ck)
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, q)
				return w
			}
			input := code
			if scenario == "wrong-code" {
				input = "IJKL-MNOP"
			}
			result := perform(input)
			var backup model.TwoFABackupCode
			require.NoError(t, db.First(&backup, "user_id = ?", user.Id).Error)
			var flow model.AuthFlow
			require.NoError(t, db.First(&flow, "user_id = ?", user.Id).Error)
			if scenario == "success" {
				require.Contains(t, result.Body.String(), `"success":true`)
				require.True(t, backup.IsUsed)
				require.NotNil(t, backup.UsedAt)
				require.NotNil(t, flow.ConsumedAt)
				require.Contains(t, perform(code).Body.String(), `"success":false`)
			} else {
				require.Contains(t, result.Body.String(), `"success":false`)
				require.False(t, backup.IsUsed, "rejected login must not burn the backup code")
				require.Nil(t, backup.UsedAt)
				if scenario == "usage-write-failed" || scenario == "backup-write-failed" || scenario == "wrong-code" {
					require.Nil(t, flow.ConsumedAt, "factor write failure must also roll back challenge consumption")
				}
				if scenario == "wrong-code" {
					var stored model.TwoFA
					require.NoError(t, db.First(&stored, factor.Id).Error)
					require.Equal(t, 3, stored.FailedAttempts)
					require.Contains(t, perform(code).Body.String(), `"success":true`, "wrong code must leave the challenge available for a valid retry")
				}
			}
		})
	}
}

func TestTwoFALoginConcurrentRedemptionSucceedsOnce(t *testing.T) {
	require.NoError(t, appi18n.Init())
	for _, kind := range []string{"totp", "backup"} {
		t.Run(kind, func(t *testing.T) {
			db := setupUserSettingControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.AuthFlow{}, &model.TwoFA{}, &model.TwoFABackupCode{}, &model.Log{}))
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			oldEnabled := common.PasswordLoginEnabled
			common.PasswordLoginEnabled = true
			t.Cleanup(func() { common.PasswordLoginEnabled = oldEnabled })
			hash, err := common.Password2Hash("synthetic-password")
			require.NoError(t, err)
			user := model.User{Username: "concurrent-factor", Password: hash, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default"}
			require.NoError(t, db.Create(&user).Error)
			const secret = "JBSWY3DPEHPK3PXP"
			require.NoError(t, db.Create(&model.TwoFA{UserId: user.Id, Secret: secret, IsEnabled: true}).Error)
			const backupCode = "ABCD-EFGH"
			require.NoError(t, model.CreateBackupCodes(user.Id, []string{backupCode}))
			r := gin.New()
			r.Use(sessions.Sessions("session", cookie.NewStore([]byte("concurrent-session-test"))))
			r.POST("/login", Login)
			r.POST("/verify", Verify2FALogin)
			login := httptest.NewRecorder()
			q := httptest.NewRequestWithContext(t.Context(), "POST", "/login", strings.NewReader(`{"username":"concurrent-factor","password":"synthetic-password"}`))
			q.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(login, q)
			require.Contains(t, login.Body.String(), `"require_2fa":true`)
			arrived, release := make(chan struct{}, 2), make(chan struct{})
			var reads atomic.Int32
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:concurrent_factor_preflight", func(tx *gorm.DB) {
				if tx.Statement.Table == "two_fas" && reads.Add(1) <= 2 {
					arrived <- struct{}{}
					<-release
				}
			}))
			code := backupCode
			if kind == "totp" {
				code, err = totp.GenerateCode(secret, time.Now())
				require.NoError(t, err)
			}
			results := make(chan bool, 2)
			cookies := login.Result().Cookies()
			var wg sync.WaitGroup
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					q := httptest.NewRequestWithContext(t.Context(), "POST", "/verify", strings.NewReader(`{"code":"`+code+`"}`))
					q.Header.Set("Content-Type", "application/json")
					for _, ck := range cookies {
						q.AddCookie(ck)
					}
					w := httptest.NewRecorder()
					r.ServeHTTP(w, q)
					results <- strings.Contains(w.Body.String(), `"success":true`)
				}()
			}
			ready := true
			for range 2 {
				select {
				case <-arrived:
				case <-time.After(3 * time.Second):
					ready = false
				}
			}
			close(release)
			wg.Wait()
			require.True(t, ready, "both requests must pass challenge preflight before either completes")
			successes := 0
			for range 2 {
				if <-results {
					successes++
				}
			}
			require.Equal(t, 1, successes)
			var flow model.AuthFlow
			require.NoError(t, db.First(&flow, "user_id = ?", user.Id).Error)
			require.NotNil(t, flow.ConsumedAt)
			var used int64
			require.NoError(t, db.Model(&model.TwoFABackupCode{}).Where("user_id = ? AND is_used = ?", user.Id, true).Count(&used).Error)
			require.Equal(t, map[string]int64{"totp": 0, "backup": 1}[kind], used)
		})
	}
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
	q := httptest.NewRequestWithContext(t.Context(), "POST", "/login", strings.NewReader(`{"username":"factor-db-error","password":"synthetic-password"}`))
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
			req := httptest.NewRequestWithContext(t.Context(), "POST", "/login", strings.NewReader(`{"username":"pending-audit","password":"synthetic-password"}`))
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
				q := httptest.NewRequestWithContext(t.Context(), "POST", "/verify", strings.NewReader(`{"code":"`+code+`"}`))
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
