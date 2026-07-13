package common

import (
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type verificationValue struct {
	code    string
	time    time.Time
	claimID string
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
)

var verificationMutex sync.Mutex
var verificationMap map[string]verificationValue
var verificationMapMaxSize = 10
var VerificationValidMinutes = 10

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	return code[:length]
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap[purpose+key] = verificationValue{
		code: code,
		time: time.Now(),
	}
	if len(verificationMap) > verificationMapMaxSize {
		removeExpiredPairs()
	}
}

func VerifyCodeWithKey(key string, code string, purpose string) bool {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	value, okay := verificationMap[purpose+key]
	now := time.Now()
	if !okay || value.claimID != "" || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 {
		return false
	}
	return code == value.code
}

// VerifyCodeWithKeyAndRun reserves a code while fn runs. A successful fn
// consumes the code; an error or panic releases the reservation for retry.
func VerifyCodeWithKeyAndRun(key string, code string, purpose string, fn func() error) (verified bool, err error) {
	mapKey := purpose + key
	claimID := uuid.NewString()

	verificationMutex.Lock()
	value, okay := verificationMap[mapKey]
	now := time.Now()
	if !okay || value.claimID != "" || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 || code != value.code {
		verificationMutex.Unlock()
		return false, nil
	}
	value.claimID = claimID
	verificationMap[mapKey] = value
	verificationMutex.Unlock()

	committed := false
	defer func() {
		verificationMutex.Lock()
		current, exists := verificationMap[mapKey]
		if exists && current.claimID == claimID {
			if committed {
				delete(verificationMap, mapKey)
			} else {
				current.claimID = ""
				verificationMap[mapKey] = current
			}
		}
		verificationMutex.Unlock()
	}()

	err = fn()
	committed = err == nil
	return true, err
}

func VerifyAndDeleteCodeWithKey(key string, code string, purpose string) bool {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	mapKey := purpose + key
	value, okay := verificationMap[mapKey]
	now := time.Now()
	if !okay || value.claimID != "" || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 || code != value.code {
		return false
	}
	delete(verificationMap, mapKey)
	return true
}

func DeleteKey(key string, purpose string) {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, purpose+key)
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeExpiredPairs() {
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
