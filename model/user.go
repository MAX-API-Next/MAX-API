package model

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	UserNameMaxLength          = 20
	userOAuthIdentityMaxLength = 512
)

const userOAuthIdentityLockName = "maxapi:user-oauth-identity"

var userOAuthIdentityLockMu sync.Mutex

type UserUpdateField string

const (
	UserUpdateFieldUsername        UserUpdateField = "username"
	UserUpdateFieldDisplayName     UserUpdateField = "display_name"
	UserUpdateFieldRole            UserUpdateField = "role"
	UserUpdateFieldStatus          UserUpdateField = "status"
	UserUpdateFieldEmail           UserUpdateField = "email"
	UserUpdateFieldGitHubId        UserUpdateField = "github_id"
	UserUpdateFieldDiscordId       UserUpdateField = "discord_id"
	UserUpdateFieldOidcId          UserUpdateField = "oidc_id"
	UserUpdateFieldWeChatId        UserUpdateField = "wechat_id"
	UserUpdateFieldTelegramId      UserUpdateField = "telegram_id"
	UserUpdateFieldAccessToken     UserUpdateField = "access_token"
	UserUpdateFieldGroup           UserUpdateField = "group"
	UserUpdateFieldAffCode         UserUpdateField = "aff_code"
	UserUpdateFieldAffCount        UserUpdateField = "aff_count"
	UserUpdateFieldAffQuota        UserUpdateField = "aff_quota"
	UserUpdateFieldAffHistoryQuota UserUpdateField = "aff_history"
	UserUpdateFieldInviterId       UserUpdateField = "inviter_id"
	UserUpdateFieldLinuxDOId       UserUpdateField = "linux_do_id"
	UserUpdateFieldSetting         UserUpdateField = "setting"
	UserUpdateFieldRemark          UserUpdateField = "remark"
	UserUpdateFieldStripeCustomer  UserUpdateField = "stripe_customer"
	UserUpdateFieldLastLoginAt     UserUpdateField = "last_login_at"
)

// User if you add sensitive fields, don't forget to clean them in setupLogin function.
// Otherwise, the sensitive information will be saved on local storage in plain text!
type User struct {
	Id               int            `json:"id"`
	Username         string         `json:"username" gorm:"unique;index" validate:"max=20"`
	Password         string         `json:"password" gorm:"not null;" validate:"min=8,max=20"`
	OriginalPassword string         `json:"original_password" gorm:"-:all"` // this field is only for Password change verification, don't save it to database!
	DisplayName      string         `json:"display_name" gorm:"index" validate:"max=20"`
	Role             int            `json:"role" gorm:"type:int;default:1"`   // admin, common
	Status           int            `json:"status" gorm:"type:int;default:1"` // enabled, disabled
	Email            string         `json:"email" gorm:"index" validate:"max=50"`
	NormalizedEmail  string         `json:"-" gorm:"column:normalized_email;size:50;index"`
	GitHubId         string         `json:"github_id" gorm:"column:github_id;size:512;index" validate:"max=512"`
	DiscordId        string         `json:"discord_id" gorm:"column:discord_id;size:512;index" validate:"max=512"`
	OidcId           string         `json:"oidc_id" gorm:"column:oidc_id;size:512;index" validate:"max=512"`
	WeChatId         string         `json:"wechat_id" gorm:"column:wechat_id;size:512;index" validate:"max=512"`
	TelegramId       string         `json:"telegram_id" gorm:"column:telegram_id;size:512;index" validate:"max=512"`
	VerificationCode string         `json:"verification_code" gorm:"-:all"`                         // this field is only for Email verification, don't save it to database!
	AccessToken      *string        `json:"-" gorm:"type:char(32);column:access_token;uniqueIndex"` // this token is for system management
	Quota            int64          `json:"quota" gorm:"type:bigint;default:0"`
	UsedQuota        int64          `json:"used_quota" gorm:"type:bigint;default:0;column:used_quota"` // used quota
	RequestCount     int            `json:"request_count" gorm:"type:int;default:0;"`                  // request number
	Group            string         `json:"group" gorm:"type:varchar(64);default:'default'"`
	AffCode          string         `json:"aff_code" gorm:"type:varchar(32);column:aff_code;uniqueIndex"`
	AffCount         int            `json:"aff_count" gorm:"type:int;default:0;column:aff_count"`
	AffQuota         int64          `json:"aff_quota" gorm:"type:bigint;default:0;column:aff_quota"`           // 邀请剩余额度
	AffHistoryQuota  int64          `json:"aff_history_quota" gorm:"type:bigint;default:0;column:aff_history"` // 邀请历史额度
	InviterId        int            `json:"inviter_id" gorm:"type:int;column:inviter_id;index"`
	DeletedAt        gorm.DeletedAt `gorm:"index"`
	LinuxDOId        string         `json:"linux_do_id" gorm:"column:linux_do_id;size:512;index" validate:"max=512"`
	Setting          string         `json:"setting" gorm:"type:text;column:setting"`
	Remark           string         `json:"remark,omitempty" gorm:"type:varchar(255)" validate:"max=255"`
	StripeCustomer   string         `json:"stripe_customer" gorm:"type:varchar(64);column:stripe_customer;index"`
	CreatedAt        int64          `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	LastLoginAt      int64          `json:"last_login_at" gorm:"default:0;column:last_login_at"`
}

func (user *User) BeforeSave(_ *gorm.DB) error {
	user.NormalizedEmail = NormalizeEmail(user.Email)
	return user.ValidateOAuthIdentityLengths()
}

func (user *User) normalizeEmailFields() {
	user.Email = NormalizeEmail(user.Email)
	user.NormalizedEmail = user.Email
}

func (user *User) ToBaseUser() *UserBase {
	cache := &UserBase{
		Id:       user.Id,
		Group:    user.Group,
		Quota:    user.Quota,
		Role:     user.Role,
		Status:   user.Status,
		Username: user.Username,
		Setting:  user.Setting,
		Email:    user.Email,
	}
	return cache
}

func (user *User) GetAccessToken() string {
	if user.AccessToken == nil {
		return ""
	}
	return *user.AccessToken
}

func (user *User) SetAccessToken(token string) {
	user.AccessToken = &token
}

func (user *User) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{}
	if user.Setting != "" {
		err := common.Unmarshal([]byte(user.Setting), &setting)
		if err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	return setting
}

func (user *User) SetSetting(setting dto.UserSetting) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog("failed to marshal setting: " + err.Error())
		return
	}
	user.Setting = string(settingBytes)
}

func UpdateUserSetting(userId int, setting dto.UserSetting) error {
	if userId == 0 {
		return errors.New("id 为空！")
	}
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		return err
	}
	settingValue := string(settingBytes)
	var cacheTask CacheInvalidationTask
	err = DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&User{}).Where("id = ?", userId).Update("setting", settingValue)
		if err := ensureUserUpdateMatchedTx(tx, result, userId, errors.New("用户不存在")); err != nil {
			return err
		}
		var err error
		cacheTask, err = stageUserCacheInvalidationTx(tx, userId, false)
		return err
	})
	if err != nil {
		return err
	}
	dispatchStagedCacheInvalidation(cacheTask)
	return nil
}

// 根据用户角色生成默认的边栏配置
func generateDefaultSidebarConfigForRole(userRole int) string {
	defaultConfig := map[string]interface{}{}

	// 聊天区域 - 所有用户都可以访问
	defaultConfig["chat"] = map[string]interface{}{
		"enabled":    true,
		"playground": true,
		"chat":       true,
	}

	// 控制台区域 - 所有用户都可以访问
	defaultConfig["console"] = map[string]interface{}{
		"enabled":    true,
		"detail":     true,
		"token":      true,
		"log":        true,
		"midjourney": true,
		"task":       true,
	}

	// 个人中心区域 - 所有用户都可以访问
	defaultConfig["personal"] = map[string]interface{}{
		"enabled":  true,
		"topup":    true,
		"personal": true,
	}

	// 管理员区域 - 根据角色决定
	if userRole == common.RoleAdminUser {
		// 管理员可以访问管理员区域，但不能访问系统设置
		defaultConfig["admin"] = map[string]interface{}{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    false, // 管理员不能访问系统设置
		}
	} else if userRole == common.RoleRootUser {
		// 超级管理员可以访问所有功能
		defaultConfig["admin"] = map[string]interface{}{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    true,
		}
	}
	// 普通用户不包含admin区域

	// 转换为JSON字符串
	configBytes, err := common.Marshal(defaultConfig)
	if err != nil {
		common.SysLog("生成默认边栏配置失败: " + err.Error())
		return ""
	}

	return string(configBytes)
}

// CheckUserExistOrDeleted check if user exist or deleted, if not exist, return false, nil, if deleted or exist, return true, nil
func CheckUserExistOrDeleted(username string, email string) (bool, error) {
	var user User

	// err := DB.Unscoped().First(&user, "username = ? or email = ?", username, email).Error
	// check email if empty
	var err error
	email = NormalizeEmail(email)
	if email == "" {
		err = DB.Unscoped().First(&user, "username = ?", username).Error
	} else {
		err = DB.Unscoped().First(&user, "username = ? or normalized_email = ?", username, email).Error
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// not exist, return false, nil
			return false, nil
		}
		// other error, return false, err
		return false, err
	}
	// exist, return true, nil
	return true, nil
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func normalizedEmailLockName(email string) string {
	return "maxapi:user-email:" + common.Sha1([]byte(NormalizeEmail(email)))
}

func withUserOAuthIdentityMutationLock(db *gorm.DB, fn func(db *gorm.DB) error) error {
	if db == nil {
		db = DB
	}
	switch {
	case common.UsingPostgreSQL:
		return db.Connection(func(conn *gorm.DB) error {
			if err := conn.Exec("SELECT pg_advisory_lock(hashtext(?))", userOAuthIdentityLockName).Error; err != nil {
				return err
			}
			released := false
			defer func() {
				if !released {
					_ = releasePostgreSQLAdvisoryLock(conn, userOAuthIdentityLockName)
				}
			}()
			err := fn(conn)
			releaseErr := releasePostgreSQLAdvisoryLock(conn, userOAuthIdentityLockName)
			return finishUserOAuthIdentityLock(err, releaseErr, &released)
		})
	case common.UsingMySQL:
		return db.Connection(func(conn *gorm.DB) error {
			acquired, err := acquireMySQLNamedLock(conn, userOAuthIdentityLockName)
			if err != nil {
				return err
			}
			if !acquired {
				return errors.New("failed to acquire user OAuth identity lock")
			}
			released := false
			defer func() {
				if !released {
					_ = releaseMySQLNamedLock(conn, userOAuthIdentityLockName)
				}
			}()
			err = fn(conn)
			releaseErr := releaseMySQLNamedLock(conn, userOAuthIdentityLockName)
			return finishUserOAuthIdentityLock(err, releaseErr, &released)
		})
	default:
		userOAuthIdentityLockMu.Lock()
		defer userOAuthIdentityLockMu.Unlock()
		return fn(db)
	}
}

func finishUserOAuthIdentityLock(callbackErr, releaseErr error, released *bool) error {
	if releaseErr == nil && released != nil {
		*released = true
	}
	if callbackErr != nil && releaseErr != nil {
		return errors.Join(callbackErr, releaseErr)
	}
	if callbackErr != nil {
		return callbackErr
	}
	return releaseErr
}

func releasePostgreSQLAdvisoryLock(db *gorm.DB, lockName string) error {
	var released bool
	if err := db.Raw("SELECT pg_advisory_unlock(hashtext(?))", lockName).Row().Scan(&released); err != nil {
		return err
	}
	if !released {
		return errors.New("failed to release user OAuth identity lock")
	}
	return nil
}

func emailQuery(tx *gorm.DB, email string) *gorm.DB {
	if tx == nil {
		tx = DB
	}
	return tx.Unscoped().Model(&User{}).Where("normalized_email = ?", NormalizeEmail(email))
}

func CountUsersByEmail(email string) (int64, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return 0, nil
	}
	var count int64
	err := emailQuery(DB, email).Count(&count).Error
	return count, err
}

func IsEmailAvailable(email string, excludeUserID int) (bool, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return true, nil
	}
	query := emailQuery(DB, email)
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count == 0, nil
}

func EnsureEmailAvailable(email string, excludeUserID int) error {
	available, err := IsEmailAvailable(email, excludeUserID)
	if err != nil {
		return err
	}
	if !available {
		return ErrEmailAlreadyTaken
	}
	return nil
}

func (user *User) ValidateOAuthIdentityLengths() error {
	values := map[string]string{
		"github_id":   user.GitHubId,
		"discord_id":  user.DiscordId,
		"oidc_id":     user.OidcId,
		"wechat_id":   user.WeChatId,
		"telegram_id": user.TelegramId,
		"linux_do_id": user.LinuxDOId,
	}
	for column, value := range values {
		if err := validateOAuthIdentityLength(column, value); err != nil {
			return err
		}
	}
	return nil
}

func validateOAuthIdentityLength(column, value string) error {
	if utf8.RuneCountInString(value) > userOAuthIdentityMaxLength {
		return fmt.Errorf("users.%s exceeds %d characters", column, userOAuthIdentityMaxLength)
	}
	return nil
}

// withNormalizedEmailLock serializes concurrent writers targeting the same
// normalized email inside tx. SQLite's single-writer model already serializes
// the write path, so it needs no explicit lock here.
func withNormalizedEmailLock(tx *gorm.DB, email string, fn func(tx *gorm.DB) error) error {
	email = NormalizeEmail(email)
	if email == "" {
		return fn(tx)
	}
	switch {
	case common.UsingPostgreSQL:
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", email).Error; err != nil {
			return err
		}
		return fn(tx)
	case common.UsingMySQL:
		lockName := normalizedEmailLockName(email)
		acquired, err := acquireMySQLNamedLock(tx, lockName)
		if err != nil {
			return err
		}
		if !acquired {
			return errors.New("failed to acquire user email lock")
		}
		released := false
		defer func() {
			if !released {
				_ = releaseMySQLNamedLock(tx, lockName)
			}
		}()
		err = fn(tx)
		releaseErr := releaseMySQLNamedLock(tx, lockName)
		released = true
		if releaseErr != nil && err == nil {
			return releaseErr
		}
		return err
	default:
		return fn(tx)
	}
}

// WithNormalizedEmailWriteTx runs fn in a transaction while serializing writers
// for the same normalized email. MySQL named locks are connection-scoped, so the
// lock must be acquired on a pinned connection and released only after the
// transaction has committed or rolled back.
func normalizedEmailWriteTxOnConn(db *gorm.DB, email string, fn func(tx *gorm.DB) error) error {
	email = NormalizeEmail(email)
	if !common.UsingMySQL {
		return db.Transaction(func(tx *gorm.DB) error {
			return withNormalizedEmailLock(tx, email, fn)
		})
	}
	if email == "" {
		return db.Transaction(fn)
	}

	lockName := normalizedEmailLockName(email)
	acquired, err := acquireMySQLNamedLock(db, lockName)
	if err != nil {
		return err
	}
	if !acquired {
		return errors.New("failed to acquire user email lock")
	}

	released := false
	defer func() {
		if !released {
			_ = releaseMySQLNamedLock(db, lockName)
		}
	}()

	err = db.Transaction(fn)
	releaseErr := releaseMySQLNamedLock(db, lockName)
	released = true
	if err != nil {
		return err
	}
	return releaseErr
}

func WithNormalizedEmailWriteTx(email string, fn func(tx *gorm.DB) error) error {
	if !common.UsingMySQL {
		return normalizedEmailWriteTxOnConn(DB, email, fn)
	}
	return DB.Connection(func(conn *gorm.DB) error {
		return normalizedEmailWriteTxOnConn(conn, email, fn)
	})
}

func WithUserOAuthIdentityWriteTx(email string, fn func(tx *gorm.DB) error) error {
	return withUserOAuthIdentityMutationLock(DB, func(conn *gorm.DB) error {
		return normalizedEmailWriteTxOnConn(conn, email, fn)
	})
}

// Use database/sql row scanning so sql.NullInt64 is not parsed as a GORM model.
func scanNullableInt64(row *sql.Row) (sql.NullInt64, error) {
	var value sql.NullInt64
	err := row.Scan(&value)
	return value, err
}

func acquireMySQLNamedLock(tx *gorm.DB, lockName string) (bool, error) {
	acquired, err := scanNullableInt64(tx.Raw("SELECT GET_LOCK(?, ?)", lockName, 10).Row())
	if err != nil {
		return false, err
	}
	return isMySQLNamedLockSuccess(acquired), nil
}

func releaseMySQLNamedLock(tx *gorm.DB, lockName string) error {
	released, err := scanNullableInt64(tx.Raw("SELECT RELEASE_LOCK(?)", lockName).Row())
	if err != nil {
		return err
	}
	if !isMySQLNamedLockSuccess(released) {
		return errors.New("failed to release user email lock")
	}
	return nil
}

func isMySQLNamedLockSuccess(value sql.NullInt64) bool {
	return value.Valid && value.Int64 == 1
}

func GetMaxUserId() int {
	var user User
	DB.Unscoped().Last(&user)
	return user.Id
}

func GetAllUsers(pageInfo *common.PageInfo) (users []*User, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get total count within transaction
	err = tx.Unscoped().Model(&User{}).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated users within same transaction
	err = tx.Unscoped().Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Omit("password").Find(&users).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

const (
	UserQuotaStatusNegative = "negative"
	UserQuotaStatusZero     = "zero"
	UserQuotaStatusPositive = "positive"
)

func buildSearchUsersQuery(tx *gorm.DB, keyword string, group string, role *int, status *int, quotaStatus string) *gorm.DB {
	query := tx.Unscoped().Model(&User{})

	keyword = strings.TrimSpace(keyword)
	if keyword != "" {
		// 构建搜索条件
		likeCondition := "username LIKE ? OR email LIKE ? OR display_name LIKE ?"
		likeArgs := []interface{}{"%" + keyword + "%", "%" + keyword + "%", "%" + keyword + "%"}

		// 尝试将关键字转换为整数ID
		keywordInt, err := strconv.Atoi(keyword)
		if err == nil {
			// 如果是数字，同时搜索ID和其他字段
			likeCondition = "id = ? OR " + likeCondition
			likeArgs = append([]interface{}{keywordInt}, likeArgs...)
		}

		query = query.Where("("+likeCondition+")", likeArgs...)
	}
	if group != "" {
		query = query.Where(commonGroupCol+" = ?", group)
	}
	if role != nil {
		query = query.Where("role = ?", *role)
	}
	if status != nil {
		if *status == -1 {
			query = query.Where("deleted_at IS NOT NULL")
		} else {
			query = query.Where("deleted_at IS NULL").Where("status = ?", *status)
		}
	}
	switch strings.ToLower(strings.TrimSpace(quotaStatus)) {
	case UserQuotaStatusNegative:
		query = query.Where("quota < 0")
	case UserQuotaStatusZero:
		query = query.Where("quota = 0")
	case UserQuotaStatusPositive:
		query = query.Where("quota > 0")
	}

	return query
}

func SearchUsers(keyword string, group string, role *int, status *int, quotaStatus string, startIdx int, num int) ([]*User, int64, error) {
	var users []*User
	var total int64
	var err error

	// 开始事务
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 构建基础查询
	query := buildSearchUsersQuery(tx, keyword, group, role, status, quotaStatus)

	// 获取总数
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 获取分页数据
	err = query.Omit("password").Order("id desc").Limit(num).Offset(startIdx).Find(&users).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 提交事务
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

func GetUserById(id int, selectAll bool) (*User, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	user := User{Id: id}
	var err error = nil
	if selectAll {
		err = DB.First(&user, "id = ?", id).Error
	} else {
		err = DB.Omit("password").First(&user, "id = ?", id).Error
	}
	return &user, err
}

func GetUserIdByAffCode(affCode string) (int, error) {
	if affCode == "" {
		return 0, errors.New("affCode 为空！")
	}
	var user User
	err := DB.Select("id").First(&user, "aff_code = ?", affCode).Error
	return user.Id, err
}

func DeleteUserById(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空！")
	}
	user := User{Id: id}
	return user.Delete()
}

func HardDeleteUserById(id int) error {
	if id == 0 {
		return errors.New("id 为空！")
	}
	user := User{Id: id}
	return user.HardDelete()
}

func inviteUser(inviterId int) (err error) {
	result := DB.Model(&User{}).Where("id = ?", inviterId).Updates(map[string]interface{}{
		"aff_count":   gorm.Expr("aff_count + 1"),
		"aff_quota":   gorm.Expr("aff_quota + ?", common.QuotaForInviter),
		"aff_history": gorm.Expr("aff_history + ?", common.QuotaForInviter),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: id=%d", ErrUserNotFound, inviterId)
	}
	return nil
}

func (user *User) TransferAffQuotaToQuota(quota int64) error {
	// 检查quota是否小于最小额度
	if float64(quota) < common.QuotaPerUnit {
		return fmt.Errorf("转移额度最小为%s！", logger.LogQuota(int(common.QuotaPerUnit)))
	}

	// 开始数据库事务
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer tx.Rollback() // 确保在函数退出时事务能回滚

	// 加锁查询用户以确保数据一致性
	err := withRowLock(tx).First(user, user.Id).Error
	if err != nil {
		return err
	}

	// 再次检查用户的AffQuota是否足够
	if user.AffQuota < quota {
		return errors.New("邀请额度不足！")
	}

	// 更新用户额度
	result := tx.Model(&User{}).Where("id = ? AND aff_quota >= ?", user.Id, quota).
		Updates(map[string]interface{}{
			"aff_quota": gorm.Expr("aff_quota - ?", quota),
			"quota":     gorm.Expr("quota + ?", quota),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("邀请额度不足！")
	}
	user.AffQuota -= quota
	user.Quota += quota

	// 提交事务
	return tx.Commit().Error
}

func (user *User) prepareForInsert(tx *gorm.DB) error {
	user.normalizeEmailFields()
	if err := ensureEmailAvailableWithTx(tx, user.Email, 0); err != nil {
		return err
	}
	if user.Password == "" {
		return nil
	}
	var err error
	user.Password, err = common.Password2Hash(user.Password)
	return err
}

func ensureEmailAvailableWithTx(tx *gorm.DB, email string, excludeUserID int) error {
	email = NormalizeEmail(email)
	if email == "" {
		return nil
	}
	query := emailQuery(tx, email)
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrEmailAlreadyTaken
	}
	return nil
}

// BindEmailToUser atomically checks email availability and assigns it to the
// user, preventing concurrent binds from sharing the same normalized address.
func BindEmailToUser(user *User, email string) error {
	email = NormalizeEmail(email)
	var cacheTask CacheInvalidationTask
	if err := WithNormalizedEmailWriteTx(email, func(tx *gorm.DB) error {
		if err := ensureEmailAvailableWithTx(tx, email, user.Id); err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
			"email":            email,
			"normalized_email": email,
		}).Error; err != nil {
			return err
		}
		user.Email = email
		user.NormalizedEmail = email
		if err := tx.First(user, user.Id).Error; err != nil {
			return err
		}
		var err error
		cacheTask, err = stageUserCacheInvalidationTx(tx, user.Id, false)
		return err
	}); err != nil {
		return err
	}
	dispatchStagedCacheInvalidation(cacheTask)
	return nil
}

func (user *User) Insert(inviterId int) error {
	if err := WithNormalizedEmailWriteTx(user.Email, func(tx *gorm.DB) error {
		if err := user.prepareForInsert(tx); err != nil {
			return err
		}
		user.Quota = int64(common.QuotaForNewUser)
		user.AffCode = common.GetRandomString(4)

		// 初始化用户设置，包括默认的边栏配置
		if user.Setting == "" {
			defaultSetting := dto.UserSetting{}
			// 这里暂时不设置SidebarModules，因为需要在用户创建后根据角色设置
			user.SetSetting(defaultSetting)
		}

		return tx.Create(user).Error
	}); err != nil {
		return err
	}

	// 用户创建成功后，根据角色初始化边栏配置
	// 需要重新获取用户以确保有正确的ID和Role
	var createdUser User
	if err := DB.Where("username = ?", user.Username).First(&createdUser).Error; err == nil {
		// 生成基于角色的默认边栏配置
		defaultSidebarConfig := generateDefaultSidebarConfigForRole(createdUser.Role)
		if defaultSidebarConfig != "" {
			currentSetting := createdUser.GetSetting()
			currentSetting.SidebarModules = defaultSidebarConfig
			createdUser.SetSetting(currentSetting)
			createdUser.UpdateFields(false, UserUpdateFieldSetting)
			common.SysLog(fmt.Sprintf("为新用户 %s (角色: %d) 初始化边栏配置", createdUser.Username, createdUser.Role))
		}
	}

	if common.QuotaForNewUser > 0 {
		RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("新用户注册赠送 %s", logger.LogQuota(common.QuotaForNewUser)))
	}
	if inviterId != 0 && operation_setting.IsPaymentComplianceConfirmed() {
		if common.QuotaForInvitee > 0 {
			_ = IncreaseUserQuota(user.Id, common.QuotaForInvitee, true)
			RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("使用邀请码赠送 %s", logger.LogQuota(common.QuotaForInvitee)))
		}
		if common.QuotaForInviter > 0 {
			//_ = IncreaseUserQuota(inviterId, common.QuotaForInviter)
			RecordLog(inviterId, LogTypeSystem, fmt.Sprintf("邀请用户赠送 %s", logger.LogQuota(common.QuotaForInviter)))
			_ = inviteUser(inviterId)
		}
	}
	return nil
}

// InsertWithTx inserts a new user within an existing transaction.
// Callers that own the outer transaction should use WithNormalizedEmailWriteTx
// so MySQL's connection-scoped email lock covers the final commit.
// This is used for OAuth registration where user creation and binding need to be atomic.
// Post-creation tasks (sidebar config, logs, inviter rewards) are handled after the transaction commits.
func (user *User) InsertWithTx(tx *gorm.DB, inviterId int) error {
	return withNormalizedEmailLock(tx, user.Email, func(tx *gorm.DB) error {
		if err := user.prepareForInsert(tx); err != nil {
			return err
		}
		user.Quota = int64(common.QuotaForNewUser)
		user.AffCode = common.GetRandomString(4)

		// 初始化用户设置
		if user.Setting == "" {
			defaultSetting := dto.UserSetting{}
			user.SetSetting(defaultSetting)
		}

		return tx.Create(user).Error
	})
}

// FinalizeOAuthUserCreation performs post-transaction tasks for OAuth user creation.
// This should be called after the transaction commits successfully.
func (user *User) FinalizeOAuthUserCreation(inviterId int) {
	// 用户创建成功后，根据角色初始化边栏配置
	var createdUser User
	if err := DB.Where("id = ?", user.Id).First(&createdUser).Error; err == nil {
		defaultSidebarConfig := generateDefaultSidebarConfigForRole(createdUser.Role)
		if defaultSidebarConfig != "" {
			currentSetting := createdUser.GetSetting()
			currentSetting.SidebarModules = defaultSidebarConfig
			createdUser.SetSetting(currentSetting)
			createdUser.UpdateFields(false, UserUpdateFieldSetting)
			common.SysLog(fmt.Sprintf("为新用户 %s (角色: %d) 初始化边栏配置", createdUser.Username, createdUser.Role))
		}
	}

	if common.QuotaForNewUser > 0 {
		RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("新用户注册赠送 %s", logger.LogQuota(common.QuotaForNewUser)))
	}
	if inviterId != 0 && operation_setting.IsPaymentComplianceConfirmed() {
		if common.QuotaForInvitee > 0 {
			_ = IncreaseUserQuota(user.Id, common.QuotaForInvitee, true)
			RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("使用邀请码赠送 %s", logger.LogQuota(common.QuotaForInvitee)))
		}
		if common.QuotaForInviter > 0 {
			RecordLog(inviterId, LogTypeSystem, fmt.Sprintf("邀请用户赠送 %s", logger.LogQuota(common.QuotaForInviter)))
			_ = inviteUser(inviterId)
		}
	}
}

func (user *User) Update(updatePassword bool) error {
	var cacheTask CacheInvalidationTask
	if err := withUserOAuthIdentityMutationLock(DB, func(db *gorm.DB) error {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := user.updateWithTx(tx, updatePassword, nil); err != nil {
				return err
			}
			var err error
			cacheTask, err = stageUserCacheInvalidationTx(tx, user.Id, false)
			return err
		})
	}); err != nil {
		return err
	}
	dispatchStagedCacheInvalidation(cacheTask)
	return nil
}

func (user *User) UpdateFields(updatePassword bool, fields ...UserUpdateField) error {
	var cacheTask CacheInvalidationTask
	update := func(db *gorm.DB) error {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := user.updateWithTx(tx, updatePassword, fields); err != nil {
				return err
			}
			var err error
			cacheTask, err = stageUserCacheInvalidationTx(tx, user.Id, false)
			return err
		})
	}
	var err error
	if isAccessTokenOnlyUserUpdate(updatePassword, fields) {
		err = update(DB)
	} else {
		err = withUserOAuthIdentityMutationLock(DB, update)
	}
	if err != nil {
		return err
	}
	dispatchStagedCacheInvalidation(cacheTask)
	return nil
}

func (user *User) UpdateWithTx(tx *gorm.DB, updatePassword bool) error {
	return user.updateWithTx(tx, updatePassword, nil)
}

func (user *User) UpdateFieldsWithTx(tx *gorm.DB, updatePassword bool, fields ...UserUpdateField) error {
	return user.updateWithTx(tx, updatePassword, fields)
}

func isAccessTokenOnlyUserUpdate(updatePassword bool, fields []UserUpdateField) bool {
	return !updatePassword && len(fields) == 1 && fields[0] == UserUpdateFieldAccessToken
}

func (user *User) updateWithTx(tx *gorm.DB, updatePassword bool, fields []UserUpdateField) error {
	var err error
	if updatePassword {
		user.Password, err = common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}
	newUser := *user
	current := User{}
	if err = tx.First(&current, user.Id).Error; err != nil {
		return err
	}
	updates := buildUserUpdateValues(current, newUser, updatePassword, fields...)
	if err = validateOAuthIdentityUpdateValues(updates); err != nil {
		return err
	}
	result := tx.Model(&current).Updates(updates)
	if err = ensureUserUpdateMatchedTx(tx, result, user.Id, errors.New("用户不存在")); err != nil {
		return err
	}
	return tx.First(user, user.Id).Error
}

func validateOAuthIdentityUpdateValues(updates map[string]interface{}) error {
	for _, column := range []string{"github_id", "discord_id", "oidc_id", "wechat_id", "telegram_id", "linux_do_id"} {
		value, ok := updates[column]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if err := validateOAuthIdentityLength(column, text); err != nil {
			return err
		}
	}
	return nil
}

func buildUserUpdateValues(current User, newUser User, updatePassword bool, fields ...UserUpdateField) map[string]interface{} {
	updates := map[string]interface{}{}

	if isAccessTokenOnlyUserUpdate(updatePassword, fields) {
		updates["access_token"] = newUser.AccessToken
		return updates
	}

	if len(fields) > 0 {
		for _, field := range fields {
			applyUserUpdateField(updates, newUser, field)
		}
	} else {
		applyNonZeroUserUpdateValues(updates, newUser)
	}
	if updatePassword {
		updates["password"] = newUser.Password
	}

	copyUnspecifiedUserUpdateValues(updates, current)
	return updates
}

func applyNonZeroUserUpdateValues(updates map[string]interface{}, newUser User) {
	if newUser.Username != "" {
		updates["username"] = newUser.Username
	}
	if newUser.DisplayName != "" {
		updates["display_name"] = newUser.DisplayName
	}
	if newUser.Role != 0 {
		updates["role"] = newUser.Role
	}
	if newUser.Status != 0 {
		updates["status"] = newUser.Status
	}
	if newUser.Email != "" {
		email := NormalizeEmail(newUser.Email)
		updates["email"] = email
		updates["normalized_email"] = email
	}
	if newUser.GitHubId != "" {
		updates["github_id"] = newUser.GitHubId
	}
	if newUser.DiscordId != "" {
		updates["discord_id"] = newUser.DiscordId
	}
	if newUser.OidcId != "" {
		updates["oidc_id"] = newUser.OidcId
	}
	if newUser.WeChatId != "" {
		updates["wechat_id"] = newUser.WeChatId
	}
	if newUser.TelegramId != "" {
		updates["telegram_id"] = newUser.TelegramId
	}
	if newUser.Group != "" {
		updates["group"] = newUser.Group
	}
	if newUser.AffCode != "" {
		updates["aff_code"] = newUser.AffCode
	}
	if newUser.AffCount != 0 {
		updates["aff_count"] = newUser.AffCount
	}
	if newUser.AffQuota != 0 {
		updates["aff_quota"] = newUser.AffQuota
	}
	if newUser.AffHistoryQuota != 0 {
		updates["aff_history"] = newUser.AffHistoryQuota
	}
	if newUser.InviterId != 0 {
		updates["inviter_id"] = newUser.InviterId
	}
	if newUser.LinuxDOId != "" {
		updates["linux_do_id"] = newUser.LinuxDOId
	}
	if newUser.Setting != "" {
		updates["setting"] = newUser.Setting
	}
	if newUser.Remark != "" {
		updates["remark"] = newUser.Remark
	}
	if newUser.StripeCustomer != "" {
		updates["stripe_customer"] = newUser.StripeCustomer
	}
	if newUser.LastLoginAt != 0 {
		updates["last_login_at"] = newUser.LastLoginAt
	}
}

func applyUserUpdateField(updates map[string]interface{}, newUser User, field UserUpdateField) {
	switch field {
	case UserUpdateFieldUsername:
		updates["username"] = newUser.Username
	case UserUpdateFieldDisplayName:
		updates["display_name"] = newUser.DisplayName
	case UserUpdateFieldRole:
		updates["role"] = newUser.Role
	case UserUpdateFieldStatus:
		updates["status"] = newUser.Status
	case UserUpdateFieldEmail:
		email := NormalizeEmail(newUser.Email)
		updates["email"] = email
		updates["normalized_email"] = email
	case UserUpdateFieldGitHubId:
		updates["github_id"] = newUser.GitHubId
	case UserUpdateFieldDiscordId:
		updates["discord_id"] = newUser.DiscordId
	case UserUpdateFieldOidcId:
		updates["oidc_id"] = newUser.OidcId
	case UserUpdateFieldWeChatId:
		updates["wechat_id"] = newUser.WeChatId
	case UserUpdateFieldTelegramId:
		updates["telegram_id"] = newUser.TelegramId
	case UserUpdateFieldAccessToken:
		updates["access_token"] = newUser.AccessToken
	case UserUpdateFieldGroup:
		updates["group"] = newUser.Group
	case UserUpdateFieldAffCode:
		updates["aff_code"] = newUser.AffCode
	case UserUpdateFieldAffCount:
		updates["aff_count"] = newUser.AffCount
	case UserUpdateFieldAffQuota:
		updates["aff_quota"] = newUser.AffQuota
	case UserUpdateFieldAffHistoryQuota:
		updates["aff_history"] = newUser.AffHistoryQuota
	case UserUpdateFieldInviterId:
		updates["inviter_id"] = newUser.InviterId
	case UserUpdateFieldLinuxDOId:
		updates["linux_do_id"] = newUser.LinuxDOId
	case UserUpdateFieldSetting:
		updates["setting"] = newUser.Setting
	case UserUpdateFieldRemark:
		updates["remark"] = newUser.Remark
	case UserUpdateFieldStripeCustomer:
		updates["stripe_customer"] = newUser.StripeCustomer
	case UserUpdateFieldLastLoginAt:
		updates["last_login_at"] = newUser.LastLoginAt
	}
}

func copyUnspecifiedUserUpdateValues(updates map[string]interface{}, current User) {
	defaults := map[string]interface{}{
		"role":             current.Role,
		"status":           current.Status,
		"email":            current.Email,
		"normalized_email": NormalizeEmail(current.Email),
		"github_id":        current.GitHubId,
		"discord_id":       current.DiscordId,
		"oidc_id":          current.OidcId,
		"wechat_id":        current.WeChatId,
		"telegram_id":      current.TelegramId,
		"group":            current.Group,
		"aff_code":         current.AffCode,
		"aff_count":        current.AffCount,
		"aff_quota":        current.AffQuota,
		"aff_history":      current.AffHistoryQuota,
		"inviter_id":       current.InviterId,
		"linux_do_id":      current.LinuxDOId,
		"setting":          current.Setting,
		"remark":           current.Remark,
		"stripe_customer":  current.StripeCustomer,
		"last_login_at":    current.LastLoginAt,
	}
	for key, value := range defaults {
		if _, ok := updates[key]; !ok {
			updates[key] = value
		}
	}
}

func (user *User) Edit(updatePassword bool) error {
	return user.UpdateFields(
		updatePassword,
		UserUpdateFieldUsername,
		UserUpdateFieldDisplayName,
		UserUpdateFieldGroup,
		UserUpdateFieldRemark,
	)
}

func (user *User) ClearBinding(bindingType string) error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}

	var field UserUpdateField
	switch bindingType {
	case "email":
		user.Email = ""
		field = UserUpdateFieldEmail
	case "github":
		user.GitHubId = ""
		field = UserUpdateFieldGitHubId
	case "discord":
		user.DiscordId = ""
		field = UserUpdateFieldDiscordId
	case "oidc":
		user.OidcId = ""
		field = UserUpdateFieldOidcId
	case "wechat":
		user.WeChatId = ""
		field = UserUpdateFieldWeChatId
	case "telegram":
		user.TelegramId = ""
		field = UserUpdateFieldTelegramId
	case "linuxdo":
		user.LinuxDOId = ""
		field = UserUpdateFieldLinuxDOId
	default:
		return errors.New("invalid binding type")
	}
	return user.UpdateFields(false, field)
}

func (user *User) Delete() error {
	return user.deleteWithCacheInvalidation(false)
}

func (user *User) HardDelete() error {
	return user.deleteWithCacheInvalidation(true)
}

func (user *User) deleteWithCacheInvalidation(hardDelete bool) error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}
	var tasks []CacheInvalidationTask
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := user.ensureCanDeleteTx(tx); err != nil {
			return err
		}
		userTask, err := stageUserCacheInvalidationTx(tx, user.Id, true)
		if err != nil {
			return err
		}
		tokenTasks, err := stageUserTokenCacheInvalidationsTx(tx, user.Id, false)
		if err != nil {
			return err
		}
		tasks = append(tasks, userTask)
		tasks = append(tasks, tokenTasks...)
		deleteDB := tx
		if hardDelete {
			deleteDB = tx.Unscoped()
		}
		return deleteDB.Delete(user).Error
	})
	if err != nil {
		return err
	}
	dispatchStagedCacheInvalidations(tasks)
	return nil
}

func (user *User) ensureCanDeleteTx(tx *gorm.DB) error {
	if user.Role == common.RoleRootUser {
		return errors.New("cannot delete root user")
	}
	if user.Role != 0 {
		return nil
	}
	var existing User
	if err := tx.Unscoped().Select("role").First(&existing, "id = ?", user.Id).Error; err != nil {
		return err
	}
	if existing.Role == common.RoleRootUser {
		return errors.New("cannot delete root user")
	}
	user.Role = existing.Role
	return nil
}

// ValidateAndFill check password & user status
func (user *User) ValidateAndFill() (err error) {
	// When querying with struct, GORM will only query with non-zero fields,
	// that means if your field's value is 0, '', false or other zero values,
	// it won't be used to build query conditions
	password := user.Password
	username := strings.TrimSpace(user.Username)
	if username == "" || password == "" {
		return ErrUserEmptyCredentials
	}
	// find by username or email
	err = DB.Where("username = ? OR email = ?", username, username).First(user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidCredentials
		}
		return fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	if user.Password == "" {
		return ErrInvalidCredentials
	}
	okay := common.ValidatePasswordAndHash(password, user.Password)
	if !okay || user.Status != common.UserStatusEnabled {
		return ErrInvalidCredentials
	}
	return nil
}

func (user *User) FillUserById() error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}
	return DB.Where(User{Id: user.Id}).First(user).Error
}

func (user *User) FillUserByEmail() error {
	if user.Email == "" {
		return errors.New("email 为空！")
	}
	return DB.Where(User{Email: user.Email}).First(user).Error
}

func (user *User) FillUserByGitHubId() error {
	if user.GitHubId == "" {
		return errors.New("GitHub id 为空！")
	}
	return fillOAuthUser(user, "github_id", user.GitHubId)
}

// UpdateGitHubId updates the user's GitHub ID (used for migration from login to numeric ID)
func (user *User) UpdateGitHubId(newGitHubId string) error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}
	if err := validateOAuthIdentityLength("github_id", newGitHubId); err != nil {
		return err
	}
	user.GitHubId = newGitHubId
	return user.UpdateFields(false, UserUpdateFieldGitHubId)
}

func (user *User) FillUserByDiscordId() error {
	if user.DiscordId == "" {
		return errors.New("discord id 为空！")
	}
	return fillOAuthUser(user, "discord_id", user.DiscordId)
}

func (user *User) FillUserByOidcId() error {
	if user.OidcId == "" {
		return errors.New("oidc id 为空！")
	}
	return fillOAuthUser(user, "oidc_id", user.OidcId)
}

func (user *User) FillUserByWeChatId() error {
	if user.WeChatId == "" {
		return errors.New("WeChat id 为空！")
	}
	return fillOAuthUser(user, "wechat_id", user.WeChatId)
}

func (user *User) FillUserByTelegramId() error {
	if user.TelegramId == "" {
		return errors.New("Telegram id 为空！")
	}
	err := fillOAuthUser(user, "telegram_id", user.TelegramId)
	if errors.Is(err, ErrUserDeleted) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("该 Telegram 账户未绑定")
	}
	return err
}

func fillOAuthUser(user *User, field string, value interface{}) error {
	err := DB.Where(field+" = ?", value).First(user).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	var deleted User
	unscopedErr := DB.Unscoped().Where(field+" = ?", value).First(&deleted).Error
	if unscopedErr == nil && deleted.DeletedAt.Valid {
		return ErrUserDeleted
	}
	if unscopedErr != nil && !errors.Is(unscopedErr, gorm.ErrRecordNotFound) {
		return unscopedErr
	}
	return err
}

func IsEmailAlreadyTaken(email string) bool {
	count, err := CountUsersByEmail(email)
	return err == nil && count > 0
}

func GetUniqueUserByEmail(email string) (*User, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, ErrEmailNotFound
	}
	var users []User
	if err := DB.Where("normalized_email = ?", email).Limit(2).Find(&users).Error; err != nil {
		return nil, err
	}
	switch len(users) {
	case 0:
		return nil, ErrEmailNotFound
	case 1:
		return &users[0], nil
	default:
		return nil, ErrEmailAmbiguous
	}
}

func IsWeChatIdAlreadyTaken(wechatId string) (bool, error) {
	result := DB.Unscoped().Where("wechat_id = ?", wechatId).Limit(1).Find(&User{})
	return result.RowsAffected > 0, result.Error
}

func IsGitHubIdAlreadyTaken(githubId string) (bool, error) {
	result := DB.Unscoped().Where("github_id = ?", githubId).Limit(1).Find(&User{})
	return result.RowsAffected > 0, result.Error
}

func IsDiscordIdAlreadyTaken(discordId string) (bool, error) {
	result := DB.Unscoped().Where("discord_id = ?", discordId).Limit(1).Find(&User{})
	return result.RowsAffected > 0, result.Error
}

func IsOidcIdAlreadyTaken(oidcId string) (bool, error) {
	result := DB.Unscoped().Where("oidc_id = ?", oidcId).Limit(1).Find(&User{})
	return result.RowsAffected > 0, result.Error
}

func IsTelegramIdAlreadyTaken(telegramId string) (bool, error) {
	result := DB.Unscoped().Where("telegram_id = ?", telegramId).Limit(1).Find(&User{})
	return result.RowsAffected > 0, result.Error
}

func ResetUserPasswordByEmail(email string, password string) error {
	if email == "" || password == "" {
		return errors.New("邮箱地址或密码为空！")
	}
	user, err := GetUniqueUserByEmail(email)
	if err != nil {
		return err
	}
	hashedPassword, err := common.Password2Hash(password)
	if err != nil {
		return err
	}
	err = DB.Model(&User{}).Where("id = ?", user.Id).Update("password", hashedPassword).Error
	return err
}

func IsAdmin(userId int) bool {
	if userId == 0 {
		return false
	}
	var user User
	err := DB.Where("id = ?", userId).Select("role").Find(&user).Error
	if err != nil {
		common.SysLog("no such user " + err.Error())
		return false
	}
	return user.Role >= common.RoleAdminUser
}

func ValidateAccessToken(token string) (*User, error) {
	if token == "" {
		return nil, nil
	}
	token = strings.Replace(token, "Bearer ", "", 1)
	user := &User{}
	err := DB.Where("access_token = ?", token).First(user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	return user, nil
}

// GetUserQuota gets quota from Redis first, falls back to DB if needed
func GetUserQuota(id int, fromDB bool) (quota int64, err error) {
	if !fromDB && common.RedisEnabled {
		quota, err := getUserQuotaCache(id)
		if err == nil {
			return quota, nil
		}
		// Don't return error - fall through to DB
	}
	var cacheVersion int64
	cacheVersionValid := false
	if common.RedisEnabled {
		cacheVersion, err = common.RedisGetCacheVersion(getUserCacheVersionKey(id))
		cacheVersionValid = err == nil
	}
	err = DB.Model(&User{}).Where("id = ?", id).Select("quota").Find(&quota).Error
	if err != nil {
		return 0, err
	}
	if cacheVersionValid {
		quotaSnapshot := quota
		gopool.Go(func() {
			if err := updateUserQuotaCacheIfVersion(id, quotaSnapshot, cacheVersion); err != nil {
				common.SysLog("failed to update user quota cache: " + err.Error())
			}
		})
	}

	return quota, nil
}

func GetUserUsedQuota(id int) (quota int64, err error) {
	err = DB.Model(&User{}).Where("id = ?", id).Select("used_quota").Find(&quota).Error
	return quota, err
}

func GetUserEmail(id int) (email string, err error) {
	err = DB.Model(&User{}).Where("id = ?", id).Select("email").Find(&email).Error
	return email, err
}

// GetUserGroup gets group from Redis first, falls back to DB if needed
func GetUserGroup(id int, fromDB bool) (group string, err error) {
	var cacheVersion int64
	cacheVersionValid := false
	defer func() {
		// Update Redis cache asynchronously on successful DB read
		if shouldUpdateRedis(fromDB, err) && cacheVersionValid {
			gopool.Go(func() {
				if err := updateUserCacheFieldIfVersion(id, "Group", group, cacheVersion); err != nil {
					common.SysLog("failed to update user group cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		group, err := getUserGroupCache(id)
		if err == nil {
			return group, nil
		}
		// Don't return error - fall through to DB
	}
	fromDB = true
	if common.RedisEnabled {
		version, versionErr := common.RedisGetCacheVersion(getUserCacheVersionKey(id))
		if versionErr == nil {
			cacheVersion = version
			cacheVersionValid = true
		}
	}
	err = DB.Model(&User{}).Where("id = ?", id).Select(commonGroupCol).Find(&group).Error
	if err != nil {
		return "", err
	}

	return group, nil
}

// GetUserSetting gets setting from Redis first, falls back to DB if needed
func GetUserSetting(id int, fromDB bool) (settingMap dto.UserSetting, err error) {
	var setting string
	var cacheVersion int64
	cacheVersionValid := false
	defer func() {
		// Update Redis cache asynchronously on successful DB read
		if shouldUpdateRedis(fromDB, err) && cacheVersionValid {
			gopool.Go(func() {
				if err := updateUserCacheFieldIfVersion(id, "Setting", setting, cacheVersion); err != nil {
					common.SysLog("failed to update user setting cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		setting, err := getUserSettingCache(id)
		if err == nil {
			return setting, nil
		}
		// Don't return error - fall through to DB
	}
	fromDB = true
	if common.RedisEnabled {
		version, versionErr := common.RedisGetCacheVersion(getUserCacheVersionKey(id))
		if versionErr == nil {
			cacheVersion = version
			cacheVersionValid = true
		}
	}
	// can be nil setting
	var safeSetting sql.NullString
	err = DB.Model(&User{}).Where("id = ?", id).Select("setting").Find(&safeSetting).Error
	if err != nil {
		return settingMap, err
	}
	if safeSetting.Valid {
		setting = safeSetting.String
	} else {
		setting = ""
	}
	userBase := &UserBase{
		Setting: setting,
	}
	return userBase.GetSetting(), nil
}

type quotaDeltaInteger interface {
	~int | ~int64
}

// The final boolean is retained for caller compatibility. Accounting mutations
// always write the database directly so conditional deductions remain atomic.
func IncreaseUserQuota[T quotaDeltaInteger](id int, quota T, _ bool) (err error) {
	delta := int64(quota)
	if delta < 0 {
		return errors.New("quota 不能为负数！")
	}
	if delta == 0 {
		return nil
	}
	if err := increaseUserQuota(id, delta); err != nil {
		return err
	}
	if cacheErr := invalidateUserQuotaCache(id); cacheErr != nil {
		common.SysLog("failed to invalidate user quota cache: " + cacheErr.Error())
		enqueueUserCacheInvalidationRetry(id, cacheErr)
	}
	return nil
}

func increaseUserQuota(id int, quota int64) (err error) {
	if quota == 0 {
		return nil
	}
	result := DB.Model(&User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota + ?", quota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: id=%d", ErrUserNotFound, id)
	}
	return nil
}

// See IncreaseUserQuota for why the compatibility flag is ignored.
func DecreaseUserQuota[T quotaDeltaInteger](id int, quota T, _ bool) (err error) {
	delta := int64(quota)
	if delta < 0 {
		return errors.New("quota 不能为负数！")
	}
	if delta == 0 {
		return nil
	}
	if err := decreaseUserQuota(id, delta); err != nil {
		return err
	}
	if cacheErr := invalidateUserQuotaCache(id); cacheErr != nil {
		common.SysLog("failed to invalidate user quota cache: " + cacheErr.Error())
		enqueueUserCacheInvalidationRetry(id, cacheErr)
	}
	return nil
}

func decreaseUserQuota(id int, quota int64) (err error) {
	if quota == 0 {
		return nil
	}
	result := DB.Model(&User{}).Where("id = ? AND quota >= ?", id, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: id=%d, need=%d", ErrUserQuotaInsufficient, id, quota)
	}
	return nil
}

func invalidateUserQuotaCache(id int) error {
	if !common.RedisEnabled {
		return nil
	}
	return invalidateUserCache(id)
}

func DeltaUpdateUserQuota[T quotaDeltaInteger](id int, delta T) (err error) {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		return IncreaseUserQuota(id, delta, false)
	} else {
		return DecreaseUserQuota(id, -delta, false)
	}
}

//func GetRootUserEmail() (email string) {
//	DB.Model(&User{}).Where("role = ?", common.RoleRootUser).Select("email").Find(&email)
//	return email
//}

func GetRootUser() (user *User) {
	DB.Where("role = ?", common.RoleRootUser).First(&user)
	return user
}

func UpdateUserLastLoginAt(id int) {
	if err := DB.Model(&User{}).Where("id = ?", id).Update("last_login_at", common.GetTimestamp()).Error; err != nil {
		common.SysLog("failed to update user last_login_at: " + err.Error())
	}
}

func UpdateUserUsedQuotaAndRequestCount(id int, quota int) {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUsedQuota, id, int64(quota))
		addNewRecord(BatchUpdateTypeRequestCount, id, 1)
		return
	}
	updateUserUsedQuotaAndRequestCount(id, quota, 1)
}

func updateUserUsedQuotaAndRequestCount(id int, quota int, count int) {
	err := DB.Model(&User{}).Where("id = ?", id).Updates(
		map[string]interface{}{
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"request_count": gorm.Expr("request_count + ?", count),
		},
	).Error
	if err != nil {
		common.SysLog("failed to update user used quota and request count: " + err.Error())
		return
	}

	//// 更新缓存
	//if err := invalidateUserCache(id); err != nil {
	//	common.SysError("failed to invalidate user cache: " + err.Error())
	//}
}

func updateUserQuotaUsedQuotaAndRequestCount(id int, quota int64, usedQuota int64, requestCount int64) {
	if quota == 0 && usedQuota == 0 && requestCount == 0 {
		return
	}

	err := DB.Model(&User{}).Where("id = ?", id).Updates(
		map[string]interface{}{
			"quota":         gorm.Expr("quota + ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", usedQuota),
			"request_count": gorm.Expr("request_count + ?", requestCount),
		},
	).Error
	if err != nil {
		common.SysLog("failed to batch update user quota, used quota and request count: " + err.Error())
	}
}

func updateUserUsedQuota(id int, quota int) {
	err := DB.Model(&User{}).Where("id = ?", id).Updates(
		map[string]interface{}{
			"used_quota": gorm.Expr("used_quota + ?", quota),
		},
	).Error
	if err != nil {
		common.SysLog("failed to update user used quota: " + err.Error())
	}
}

func updateUserRequestCount(id int, count int) {
	err := DB.Model(&User{}).Where("id = ?", id).Update("request_count", gorm.Expr("request_count + ?", count)).Error
	if err != nil {
		common.SysLog("failed to update user request count: " + err.Error())
	}
}

// GetUsernameById gets username from Redis first, falls back to DB if needed
func GetUsernameById(id int, fromDB bool) (username string, err error) {
	var cacheVersion int64
	cacheVersionValid := false
	defer func() {
		// Update Redis cache asynchronously on successful DB read
		if shouldUpdateRedis(fromDB, err) && cacheVersionValid {
			gopool.Go(func() {
				if err := updateUserCacheFieldIfVersion(id, "Username", username, cacheVersion); err != nil {
					common.SysLog("failed to update user name cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		username, err := getUserNameCache(id)
		if err == nil {
			return username, nil
		}
		// Don't return error - fall through to DB
	}
	fromDB = true
	if common.RedisEnabled {
		version, versionErr := common.RedisGetCacheVersion(getUserCacheVersionKey(id))
		if versionErr == nil {
			cacheVersion = version
			cacheVersionValid = true
		}
	}
	err = DB.Model(&User{}).Where("id = ?", id).Select("username").Find(&username).Error
	if err != nil {
		return "", err
	}

	return username, nil
}

func IsLinuxDOIdAlreadyTaken(linuxDOId string) (bool, error) {
	result := DB.Unscoped().Where("linux_do_id = ?", linuxDOId).Limit(1).Find(&User{})
	return result.RowsAffected > 0, result.Error
}

func (user *User) FillUserByLinuxDOId() error {
	if user.LinuxDOId == "" {
		return errors.New("linux do id is empty")
	}
	return fillOAuthUser(user, "linux_do_id", user.LinuxDOId)
}

func RootUserExists() bool {
	var user User
	err := DB.Where("role = ?", common.RoleRootUser).First(&user).Error
	if err != nil {
		return false
	}
	return true
}
