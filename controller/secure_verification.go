package controller

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	// SecureVerificationSessionKey means the user has fully passed secure verification.
	SecureVerificationSessionKey         = "secure_verified_at"
	secureVerificationMethodSessionKey   = "secure_verified_method"
	secureVerificationUserSessionKey     = "secure_verified_user_id"
	secureVerificationScopeSessionKey    = "secure_verified_scope"
	secureVerificationMethod2FA          = "2fa"
	secureVerificationMethodPasskey      = "passkey"
	secureVerificationMethodPassword     = "password"
	secureVerificationScopeAccessToken   = "access_token"
	secureVerificationScopeAccountDelete = "account_delete"
	secureVerificationScopeCredentials   = "credentials"
	secureVerificationScopeAPIToken      = "api_token"
	// PasskeyReadySessionKey means WebAuthn finished and /api/verify can finalize step-up verification.
	PasskeyReadySessionKey = "secure_passkey_ready_at"
	// SecureVerificationTimeout 验证有效期（秒）
	SecureVerificationTimeout = 300 // 5分钟
	// PasskeyReadyTimeout passkey ready 标记有效期（秒）
	PasskeyReadyTimeout = 60
)

type UniversalVerifyRequest struct {
	Method   string `json:"method"` // "2fa"、"passkey" 或 "password"
	Code     string `json:"code,omitempty"`
	Password string `json:"password,omitempty"`
	Scope    string `json:"scope,omitempty"`
}

type VerificationMethodsResponse struct {
	Has2FA      bool `json:"has_2fa"`
	HasPasskey  bool `json:"has_passkey"`
	HasPassword bool `json:"has_password"`
}

type secureVerificationMethods struct {
	twoFA       *model.TwoFA
	has2FA      bool
	hasPasskey  bool
	hasPassword bool
}

func loadSecureVerificationMethods(user *model.User, allowPassword bool) (secureVerificationMethods, error) {
	// PasswordLoginEnabled gates new password logins; it does not invalidate an
	// existing credential used to re-verify an already authenticated session.
	methods := secureVerificationMethods{
		hasPassword: user != nil && user.Password != "",
	}
	if user == nil || user.Id == 0 {
		return methods, errors.New("用户不存在")
	}

	twoFA, err := model.GetTwoFAByUserId(user.Id)
	if err != nil {
		return methods, err
	}
	methods.twoFA = twoFA
	methods.has2FA = twoFA != nil && twoFA.IsEnabled

	_, err = model.GetPasskeyByUserID(user.Id)
	switch {
	case err == nil:
		methods.hasPasskey = true
	case errors.Is(err, model.ErrPasskeyNotFound):
		methods.hasPasskey = false
	default:
		return methods, err
	}

	// Password is a fallback only when no stronger verification method is enrolled.
	methods.hasPassword = allowPassword && methods.hasPassword && !methods.has2FA && !methods.hasPasskey
	return methods, nil
}

func GetVerificationMethods(c *gin.Context) {
	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user.Status != common.UserStatusEnabled {
		common.ApiError(c, fmt.Errorf("该用户已被禁用"))
		return
	}

	scope := c.Query("scope")
	if !isSupportedSecureVerificationScope(scope) {
		common.ApiError(c, fmt.Errorf("不支持的验证作用域: %s", scope))
		return
	}
	methods, err := loadSecureVerificationMethods(user, passwordVerificationAllowed(scope))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": VerificationMethodsResponse{
			Has2FA:      methods.has2FA,
			HasPasskey:  methods.hasPasskey,
			HasPassword: methods.hasPassword,
		},
	})
}

// UniversalVerify 通用验证接口
// 支持 2FA 和 Passkey 验证，验证成功后在 session 中记录时间戳
func UniversalVerify(c *gin.Context) {
	userId := c.GetInt("id")
	if userId == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "未登录",
		})
		return
	}

	var req UniversalVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, fmt.Errorf("参数错误: %v", err))
		return
	}

	// 获取用户信息
	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, fmt.Errorf("获取用户信息失败: %v", err))
		return
	}

	if user.Status != common.UserStatusEnabled {
		common.ApiError(c, fmt.Errorf("该用户已被禁用"))
		return
	}

	if !isSupportedSecureVerificationScope(req.Scope) {
		common.ApiError(c, fmt.Errorf("不支持的验证作用域: %s", req.Scope))
		return
	}
	methods, err := loadSecureVerificationMethods(user, passwordVerificationAllowed(req.Scope))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 根据验证方式进行验证
	var verified bool
	var verifyMethod string

	switch req.Method {
	case "2fa":
		if !methods.has2FA {
			common.ApiError(c, fmt.Errorf("用户未启用2FA"))
			return
		}
		if req.Code == "" {
			common.ApiError(c, fmt.Errorf("验证码不能为空"))
			return
		}
		verified = validateTwoFactorAuth(methods.twoFA, req.Code)
		verifyMethod = "2FA"

	case "passkey":
		if !methods.hasPasskey {
			common.ApiError(c, fmt.Errorf("用户未启用Passkey"))
			return
		}
		// Passkey branch only trusts the short-lived marker written by PasskeyVerifyFinish.
		verified, err = consumePasskeyReady(c)
		if err != nil {
			common.ApiError(c, fmt.Errorf("Passkey 验证状态异常: %v", err))
			return
		}
		if !verified {
			common.ApiError(c, fmt.Errorf("请先完成 Passkey 验证"))
			return
		}
		verifyMethod = "Passkey"

	case "password":
		if !methods.hasPassword {
			common.ApiError(c, fmt.Errorf("密码验证不可用，请使用2FA或Passkey"))
			return
		}
		if req.Password == "" {
			common.ApiError(c, fmt.Errorf("密码不能为空"))
			return
		}
		verified = common.ValidatePasswordAndHash(req.Password, user.Password)
		verifyMethod = "Password"

	default:
		common.ApiError(c, fmt.Errorf("不支持的验证方式: %s", req.Method))
		return
	}

	if !verified {
		if req.Method == secureVerificationMethodPassword {
			common.ApiError(c, fmt.Errorf("密码错误"))
		} else {
			common.ApiError(c, fmt.Errorf("验证失败，请检查验证码"))
		}
		return
	}

	// 验证成功，在 session 中记录时间戳
	now, err := setSecureVerificationSession(c, userId, req.Method, req.Scope)
	if err != nil {
		common.ApiError(c, fmt.Errorf("保存验证状态失败: %v", err))
		return
	}

	// 记录日志
	model.RecordLog(userId, model.LogTypeSystem, fmt.Sprintf("通用安全验证成功 (验证方式: %s)", verifyMethod))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "验证成功",
		"data": gin.H{
			"verified":   true,
			"expires_at": now + SecureVerificationTimeout,
		},
	})
}

func isSupportedSecureVerificationScope(scope string) bool {
	switch scope {
	case "", secureVerificationScopeAccessToken, secureVerificationScopeAccountDelete,
		secureVerificationScopeCredentials, secureVerificationScopeAPIToken:
		return true
	default:
		return false
	}
}

func passwordVerificationAllowed(scope string) bool {
	return scope == secureVerificationScopeAccessToken ||
		scope == secureVerificationScopeAccountDelete ||
		scope == secureVerificationScopeCredentials ||
		scope == secureVerificationScopeAPIToken
}

func setSecureVerificationSession(c *gin.Context, userId int, method string, scope string) (int64, error) {
	session := sessions.Default(c)
	session.Delete(PasskeyReadySessionKey)
	now := time.Now().Unix()
	session.Set(SecureVerificationSessionKey, now)
	session.Set(secureVerificationMethodSessionKey, method)
	session.Set(secureVerificationUserSessionKey, userId)
	if scope == "" {
		session.Delete(secureVerificationScopeSessionKey)
	} else {
		session.Set(secureVerificationScopeSessionKey, scope)
	}
	if err := session.Save(); err != nil {
		return 0, err
	}
	return now, nil
}

func consumePasskeyReady(c *gin.Context) (bool, error) {
	session := sessions.Default(c)
	readyAtRaw := session.Get(PasskeyReadySessionKey)
	if readyAtRaw == nil {
		return false, nil
	}

	readyAt, ok := readyAtRaw.(int64)
	if !ok {
		session.Delete(PasskeyReadySessionKey)
		_ = session.Save()
		return false, fmt.Errorf("无效的 Passkey 验证状态")
	}
	session.Delete(PasskeyReadySessionKey)
	if err := session.Save(); err != nil {
		return false, err
	}
	// Expired ready markers cannot be reused.
	if time.Now().Unix()-readyAt >= PasskeyReadyTimeout {
		return false, nil
	}
	return true, nil
}
