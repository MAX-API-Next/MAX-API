package common

import "testing"

func TestVerifyAndDeleteCodeWithKeyConsumesCodeOnce(t *testing.T) {
	key := "consume-once@example.com"
	code := "123456"
	RegisterVerificationCodeWithKey(key, code, EmailVerificationPurpose)

	if !VerifyAndDeleteCodeWithKey(key, code, EmailVerificationPurpose) {
		t.Fatal("expected first verification to succeed")
	}
	if VerifyCodeWithKey(key, code, EmailVerificationPurpose) {
		t.Fatal("expected code to be consumed")
	}
	if VerifyAndDeleteCodeWithKey(key, code, EmailVerificationPurpose) {
		t.Fatal("expected second verification to fail")
	}
}
