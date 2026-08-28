package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"

	"gorm.io/gorm"
)

// TwoFA 用户2FA设置表
type TwoFA struct {
	Id             int            `json:"id" gorm:"primaryKey"`
	UserId         int            `json:"user_id" gorm:"unique;not null;index"`
	Secret         string         `json:"-" gorm:"type:varchar(255);not null"` // TOTP密钥，不返回给前端
	IsEnabled      bool           `json:"is_enabled"`
	FailedAttempts int            `json:"failed_attempts" gorm:"default:0"`
	LockedUntil    *time.Time     `json:"locked_until,omitempty"`
	LastUsedAt     *time.Time     `json:"last_used_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"index"`
}

// TwoFABackupCode 备用码使用记录表
type TwoFABackupCode struct {
	Id        int            `json:"id" gorm:"primaryKey"`
	UserId    int            `json:"user_id" gorm:"not null;index"`
	CodeHash  string         `json:"-" gorm:"type:varchar(255);not null"` // 备用码哈希
	IsUsed    bool           `json:"is_used"`
	UsedAt    *time.Time     `json:"used_at,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

// GetTwoFAByUserId 根据用户ID获取2FA设置
func GetTwoFAByUserId(userId int) (*TwoFA, error) {
	if userId == 0 {
		return nil, errors.New("用户ID不能为空")
	}

	var twoFA TwoFA
	err := DB.Where("user_id = ?", userId).First(&twoFA).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // 返回nil表示未设置2FA
		}
		return nil, err
	}

	return &twoFA, nil
}

// IsTwoFAEnabled 检查用户是否启用了2FA
func IsTwoFAEnabled(userId int) bool {
	twoFA, err := GetTwoFAByUserId(userId)
	if err != nil || twoFA == nil {
		return false
	}
	return twoFA.IsEnabled
}

// CreateTwoFA 创建2FA设置
func (t *TwoFA) Create() error {
	// 检查用户是否已存在2FA设置
	existing, err := GetTwoFAByUserId(t.UserId)
	if err != nil {
		return err
	}
	if existing != nil {
		return errors.New("用户已存在2FA设置")
	}

	// 验证用户存在
	var user User
	if err := DB.First(&user, t.UserId).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("用户不存在")
		}
		return err
	}

	return DB.Create(t).Error
}

// Update 更新2FA设置
func (t *TwoFA) Update() error {
	if t.Id == 0 {
		return errors.New("2FA记录ID不能为空")
	}
	return DB.Save(t).Error
}

// Delete 删除2FA设置
func (t *TwoFA) Delete() error {
	if t.Id == 0 {
		return errors.New("2FA记录ID不能为空")
	}

	// 使用事务确保原子性
	return DB.Transaction(func(tx *gorm.DB) error {
		// 同时删除相关的备用码记录（硬删除）
		if err := tx.Unscoped().Where("user_id = ?", t.UserId).Delete(&TwoFABackupCode{}).Error; err != nil {
			return err
		}

		// 硬删除2FA记录
		return tx.Unscoped().Delete(t).Error
	})
}

// ResetFailedAttempts 重置失败尝试次数
func (t *TwoFA) ResetFailedAttempts() error {
	t.FailedAttempts = 0
	t.LockedUntil = nil
	return t.Update()
}

// IncrementFailedAttempts 增加失败尝试次数
func (t *TwoFA) IncrementFailedAttempts() error {
	t.FailedAttempts++

	// 检查是否需要锁定
	if t.FailedAttempts >= common.MaxFailAttempts {
		lockUntil := time.Now().Add(time.Duration(common.LockoutDuration) * time.Second)
		t.LockedUntil = &lockUntil
	}

	return t.Update()
}

// IsLocked 检查账户是否被锁定
func (t *TwoFA) IsLocked() bool {
	if t.LockedUntil == nil {
		return false
	}
	return time.Now().Before(*t.LockedUntil)
}

// CreateBackupCodes 创建备用码
func CreateBackupCodes(userId int, codes []string) error {
	hashes, err := hashBackupCodes(codes)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		return replaceBackupCodeHashesTx(tx, userId, hashes)
	})
}

func hashBackupCodes(codes []string) ([]string, error) {
	hashes := make([]string, 0, len(codes))
	for _, code := range codes {
		hashedCode, err := common.HashBackupCode(code)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, hashedCode)
	}
	return hashes, nil
}

func replaceBackupCodeHashesTx(tx *gorm.DB, userID int, hashes []string) error {
	if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&TwoFABackupCode{}).Error; err != nil {
		return err
	}
	for _, hashedCode := range hashes {
		backupCode := TwoFABackupCode{
			UserId:   userID,
			CodeHash: hashedCode,
			IsUsed:   false,
		}
		if err := tx.Create(&backupCode).Error; err != nil {
			return err
		}
	}
	return nil
}

func lockTwoFACredentialUserTx(tx *gorm.DB, userID int) error {
	var user User
	return withRowLock(tx).Select("id").Where("id = ?", userID).First(&user).Error
}

// ValidateBackupCode 验证并使用备用码
func ValidateBackupCode(userId int, code string) (bool, error) {
	if !common.ValidateBackupCode(code) {
		return false, errors.New("验证码或备用码不正确")
	}

	normalizedCode := common.NormalizeBackupCode(code)

	var backupCodes []TwoFABackupCode
	if err := DB.Where("user_id = ? AND is_used = ?", userId, false).Find(&backupCodes).Error; err != nil {
		return false, err
	}
	matchedID := 0
	matchedHash := ""
	for _, backupCode := range backupCodes {
		if common.ValidatePasswordAndHash(normalizedCode, backupCode.CodeHash) {
			matchedID = backupCode.Id
			matchedHash = backupCode.CodeHash
			break
		}
	}
	if matchedID == 0 {
		return false, nil
	}

	validated := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockTwoFACredentialUserTx(tx, userId); err != nil {
			return err
		}
		now := time.Now()
		result := tx.Model(&TwoFABackupCode{}).
			Where("id = ? AND user_id = ? AND code_hash = ? AND is_used = ?", matchedID, userId, matchedHash, false).
			Updates(map[string]interface{}{"is_used": true, "used_at": &now})
		if result.Error != nil {
			return result.Error
		}
		validated = result.RowsAffected == 1
		return nil
	})
	return validated, err
}

// GetUnusedBackupCodeCount 获取未使用的备用码数量
func GetUnusedBackupCodeCount(userId int) (int, error) {
	var count int64
	err := DB.Model(&TwoFABackupCode{}).Where("user_id = ? AND is_used = ?", userId, false).Count(&count).Error
	return int(count), err
}

// DisableTwoFA 禁用用户的2FA
func DisableTwoFA(userId int) error {
	twoFA, err := GetTwoFAByUserId(userId)
	if err != nil {
		return err
	}
	if twoFA == nil {
		return ErrTwoFANotEnabled
	}

	// 删除2FA设置和备用码
	return twoFA.Delete()
}

func DisableTwoFAAndBumpSessionGeneration(userID int) (int64, error) {
	var generation int64
	var cacheTask CacheInvalidationTask
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockTwoFACredentialUserTx(tx, userID); err != nil {
			return err
		}
		var twoFA TwoFA
		if err := tx.Where("user_id = ? AND is_enabled = ?", userID, true).First(&twoFA).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTwoFANotEnabled
			}
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&TwoFABackupCode{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Delete(&twoFA).Error; err != nil {
			return err
		}
		var err error
		generation, cacheTask, err = bumpUserSessionGenerationTx(tx, userID)
		return err
	})
	if err != nil {
		return 0, err
	}
	dispatchStagedCacheInvalidation(cacheTask)
	return generation, nil
}

// EnableTwoFA 启用2FA
func (t *TwoFA) Enable() error {
	t.IsEnabled = true
	t.FailedAttempts = 0
	t.LockedUntil = nil
	return t.Update()
}

func (t *TwoFA) EnableAndBumpSessionGeneration() (int64, error) {
	if t == nil || t.Id == 0 || t.UserId <= 0 {
		return 0, errors.New("2FA记录无效")
	}
	var generation int64
	var cacheTask CacheInvalidationTask
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockTwoFACredentialUserTx(tx, t.UserId); err != nil {
			return err
		}
		result := tx.Model(&TwoFA{}).
			Where("id = ? AND user_id = ? AND is_enabled = ?", t.Id, t.UserId, false).
			Updates(map[string]interface{}{
				"is_enabled":      true,
				"failed_attempts": 0,
				"locked_until":    nil,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("2FA状态已变化，请重试")
		}
		var err error
		generation, cacheTask, err = bumpUserSessionGenerationTx(tx, t.UserId)
		return err
	})
	if err != nil {
		return 0, err
	}
	t.IsEnabled = true
	t.FailedAttempts = 0
	t.LockedUntil = nil
	dispatchStagedCacheInvalidation(cacheTask)
	return generation, nil
}

func ReplaceBackupCodesAndBumpSessionGeneration(userID int, codes []string) (int64, error) {
	hashes, err := hashBackupCodes(codes)
	if err != nil {
		return 0, err
	}
	var generation int64
	var cacheTask CacheInvalidationTask
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockTwoFACredentialUserTx(tx, userID); err != nil {
			return err
		}
		var enabledCount int64
		if err := tx.Model(&TwoFA{}).
			Where("user_id = ? AND is_enabled = ?", userID, true).
			Count(&enabledCount).Error; err != nil {
			return err
		}
		if enabledCount != 1 {
			return ErrTwoFANotEnabled
		}
		if err := replaceBackupCodeHashesTx(tx, userID, hashes); err != nil {
			return err
		}
		var err error
		generation, cacheTask, err = bumpUserSessionGenerationTx(tx, userID)
		return err
	})
	if err != nil {
		return 0, err
	}
	dispatchStagedCacheInvalidation(cacheTask)
	return generation, nil
}

// ValidateTOTPAndUpdateUsage 验证TOTP并更新使用记录
func (t *TwoFA) ValidateTOTPAndUpdateUsage(code string) (bool, error) {
	// 检查是否被锁定
	if t.IsLocked() {
		return false, fmt.Errorf("账户已被锁定，请在%v后重试", t.LockedUntil.Format("2006-01-02 15:04:05"))
	}

	// 验证TOTP码
	if !common.ValidateTOTPCode(t.Secret, code) {
		// 增加失败次数
		if err := t.IncrementFailedAttempts(); err != nil {
			common.SysLog("更新2FA失败次数失败: " + err.Error())
		}
		return false, nil
	}

	// 验证成功，重置失败次数并更新最后使用时间
	now := time.Now()
	t.FailedAttempts = 0
	t.LockedUntil = nil
	t.LastUsedAt = &now

	if err := t.Update(); err != nil {
		common.SysLog("更新2FA使用记录失败: " + err.Error())
	}

	return true, nil
}

// ValidateBackupCodeAndUpdateUsage 验证备用码并更新使用记录
func (t *TwoFA) ValidateBackupCodeAndUpdateUsage(code string) (bool, error) {
	// 检查是否被锁定
	if t.IsLocked() {
		return false, fmt.Errorf("账户已被锁定，请在%v后重试", t.LockedUntil.Format("2006-01-02 15:04:05"))
	}

	// 验证备用码
	valid, err := ValidateBackupCode(t.UserId, code)
	if err != nil {
		return false, err
	}

	if !valid {
		// 增加失败次数
		if err := t.IncrementFailedAttempts(); err != nil {
			common.SysLog("更新2FA失败次数失败: " + err.Error())
		}
		return false, nil
	}

	// 验证成功，重置失败次数并更新最后使用时间
	now := time.Now()
	t.FailedAttempts = 0
	t.LockedUntil = nil
	t.LastUsedAt = &now

	if err := t.Update(); err != nil {
		common.SysLog("更新2FA使用记录失败: " + err.Error())
	}

	return true, nil
}

// GetTwoFAStats 获取2FA统计信息（管理员使用）
func GetTwoFAStats() (map[string]interface{}, error) {
	var totalUsers, enabledUsers int64

	// 总用户数
	if err := DB.Model(&User{}).Count(&totalUsers).Error; err != nil {
		return nil, err
	}

	// 启用2FA的用户数
	if err := DB.Model(&TwoFA{}).Where("is_enabled = ?", true).Count(&enabledUsers).Error; err != nil {
		return nil, err
	}

	enabledRate := float64(0)
	if totalUsers > 0 {
		enabledRate = float64(enabledUsers) / float64(totalUsers) * 100
	}

	return map[string]interface{}{
		"total_users":   totalUsers,
		"enabled_users": enabledUsers,
		"enabled_rate":  fmt.Sprintf("%.1f%%", enabledRate),
	}, nil
}
