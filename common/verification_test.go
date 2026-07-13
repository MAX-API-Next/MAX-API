package common

import (
	"errors"
	"sync/atomic"
	"testing"
)

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

func TestVerifyCodeWithKeyAndRunRestoresCodeAfterFailure(t *testing.T) {
	key := "rollback@example.com"
	code := "rollback-code"
	wantErr := errors.New("password update failed")
	RegisterVerificationCodeWithKey(key, code, PasswordResetPurpose)
	t.Cleanup(func() { DeleteKey(key, PasswordResetPurpose) })

	verified, err := VerifyCodeWithKeyAndRun(key, code, PasswordResetPurpose, func() error {
		return wantErr
	})

	if !verified {
		t.Fatal("expected code verification to succeed")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected callback error %v, got %v", wantErr, err)
	}
	if !VerifyCodeWithKey(key, code, PasswordResetPurpose) {
		t.Fatal("expected code to remain valid after callback failure")
	}
}

func TestVerifyCodeWithKeyAndRunRestoresCodeAfterPanic(t *testing.T) {
	key := "panic@example.com"
	code := "panic-code"
	RegisterVerificationCodeWithKey(key, code, PasswordResetPurpose)
	t.Cleanup(func() { DeleteKey(key, PasswordResetPurpose) })

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("expected callback panic")
			}
		}()
		_, _ = VerifyCodeWithKeyAndRun(key, code, PasswordResetPurpose, func() error {
			panic("password update panic")
		})
	}()

	if !VerifyCodeWithKey(key, code, PasswordResetPurpose) {
		t.Fatal("expected code to remain valid after callback panic")
	}
}

func TestVerifyCodeWithKeyAndRunAllowsOnlyOneConcurrentClaim(t *testing.T) {
	key := "concurrent@example.com"
	code := "concurrent-code"
	RegisterVerificationCodeWithKey(key, code, PasswordResetPurpose)
	t.Cleanup(func() { DeleteKey(key, PasswordResetPurpose) })

	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan struct{})
	var callbackCount atomic.Int32
	var firstVerified bool
	var firstErr error
	go func() {
		defer close(firstDone)
		firstVerified, firstErr = VerifyCodeWithKeyAndRun(key, code, PasswordResetPurpose, func() error {
			callbackCount.Add(1)
			close(started)
			<-release
			return nil
		})
	}()

	<-started
	secondVerified, secondErr := VerifyCodeWithKeyAndRun(key, code, PasswordResetPurpose, func() error {
		callbackCount.Add(1)
		return nil
	})
	close(release)
	<-firstDone

	if !firstVerified || firstErr != nil {
		t.Fatalf("expected first claim to succeed, verified=%t err=%v", firstVerified, firstErr)
	}
	if secondVerified || secondErr != nil {
		t.Fatalf("expected concurrent claim to be rejected, verified=%t err=%v", secondVerified, secondErr)
	}
	if got := callbackCount.Load(); got != 1 {
		t.Fatalf("expected one callback execution, got %d", got)
	}
	if VerifyCodeWithKey(key, code, PasswordResetPurpose) {
		t.Fatal("expected successful claim to consume the code")
	}
}
