package model

import (
	"errors"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"gorm.io/gorm"
)

var ErrUserSessionInvalid = errors.New("user session is no longer valid")

// Recheck at the commit boundary, not only against middleware/cache state.
func sessionUserWithDB(db *gorm.DB, userID int, generation int64, lock bool) (*User, error) {
	if db == nil || userID <= 0 || generation < 0 {
		return nil, ErrUserSessionInvalid
	}
	query := db
	if lock {
		query = withRowLock(query)
	}
	var user User
	if err := query.First(&user, userID).Error; err != nil {
		return nil, err
	}
	if user.Status != common.UserStatusEnabled || user.SessionGeneration != generation {
		return nil, ErrUserSessionInvalid
	}
	return &user, nil
}

func BindEmailToUserWithSession(userID int, generation int64, email string) error {
	return bindEmailToUser(&User{Id: userID}, email, &generation)
}

func BindWeChatToUserWithSession(userID int, generation int64, wechatID string) error {
	var cacheTask CacheInvalidationTask
	err := withUserOAuthIdentityMutationLock(DB, func(db *gorm.DB) error {
		return db.Transaction(func(tx *gorm.DB) error {
			user, err := sessionUserWithDB(tx, userID, generation, true)
			if err != nil {
				return err
			}
			user.WeChatId = wechatID
			cacheTask, err = user.UpdateFieldsWithTxAndCache(tx, false, UserUpdateFieldWeChatId)
			return err
		})
	})
	if err == nil {
		dispatchStagedCacheInvalidation(cacheTask)
	}
	return err
}

const TwoFALoginChallengeTTL = 5 * time.Minute

type twoFALoginPayload struct {
	Generation *int64 `json:"session_generation"`
}

func CreateTwoFALoginChallenge(user *User, sessionID string) (string, error) {
	if user == nil || sessionID == "" {
		return "", ErrAuthFlowInvalid
	}
	if _, err := sessionUserWithDB(DB, user.Id, user.SessionGeneration, false); err != nil {
		return "", err
	}
	payload, err := common.Marshal(twoFALoginPayload{Generation: &user.SessionGeneration})
	if err != nil {
		return "", err
	}
	token, _, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeTwoFALogin, UserId: user.Id, SessionId: sessionID,
		Payload: string(payload), ExpiresAt: time.Now().Add(TwoFALoginChallengeTTL),
	})
	return token, err
}

func twoFALoginUser(db *gorm.DB, flow *AuthFlow, lock bool) (*User, error) {
	var payload twoFALoginPayload
	if common.UnmarshalJsonStr(flow.Payload, &payload) != nil || payload.Generation == nil {
		return nil, ErrAuthFlowInvalid
	}
	return sessionUserWithDB(db, flow.UserId, *payload.Generation, lock)
}

func GetTwoFALoginChallenge(token, sessionID string) (*User, error) {
	if sessionID == "" {
		return nil, ErrAuthFlowInvalid
	}
	flow, err := GetAuthFlow(token, AuthFlowMatch{Purpose: AuthFlowPurposeTwoFALogin, SessionId: sessionID})
	if err != nil {
		return nil, err
	}
	return twoFALoginUser(DB, flow, false)
}

// Called only after the second factor succeeds. Consume once and recheck the
// frozen password-auth generation in the same transaction. A later revocation
// invalidates the returned generation; it must never be upgraded at setupLogin.
func CompleteTwoFALoginChallenge(token, sessionID string) (*User, error) {
	if sessionID == "" {
		return nil, ErrAuthFlowInvalid
	}
	var user *User
	_, err := ConsumeAuthFlowWithAction(token, AuthFlowMatch{Purpose: AuthFlowPurposeTwoFALogin, SessionId: sessionID}, func(tx *gorm.DB, flow *AuthFlow) error {
		var err error
		user, err = twoFALoginUser(tx, flow, true)
		return err
	})
	return user, err
}
