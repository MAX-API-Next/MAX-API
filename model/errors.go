package model

import "errors"

// Common errors
var (
	ErrDatabase               = errors.New("database error")
	ErrUserNotFound           = errors.New("user not found")
	ErrUserDeleted            = errors.New("user deleted")
	ErrTokenNotFound          = errors.New("token not found")
	ErrUserQuotaInsufficient  = errors.New("user quota is not enough")
	ErrTokenQuotaInsufficient = errors.New("token quota is not enough")
)

// User auth errors
var (
	ErrInvalidCredentials        = errors.New("invalid credentials")
	ErrUserEmptyCredentials      = errors.New("empty credentials")
	ErrEmailAlreadyTaken         = errors.New("email already taken")
	ErrEmailNotFound             = errors.New("email not found")
	ErrEmailAmbiguous            = errors.New("email matches multiple users")
	ErrOAuthIdentityAlreadyTaken = errors.New("OAuth identity is already bound")
)

// Token auth errors
var (
	ErrTokenNotProvided = errors.New("token not provided")
	ErrTokenInvalid     = errors.New("token invalid")
)

// Redemption errors
var ErrRedeemFailed = errors.New("redeem.failed")

// 2FA errors
var ErrTwoFANotEnabled = errors.New("2fa not enabled")
