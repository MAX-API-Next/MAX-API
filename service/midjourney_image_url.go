package service

import (
	"crypto/hmac"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
)

const MidjourneyImageURLTTL = 15 * time.Minute

func midjourneyImageSignaturePayload(mjID string, userID int, expiresAt int64) string {
	return fmt.Sprintf("midjourney-image\n%s\n%d\n%d", mjID, userID, expiresAt)
}

func SignMidjourneyImageURL(mjID string, userID int, expiresAt int64) string {
	return common.GenerateHMAC(midjourneyImageSignaturePayload(mjID, userID, expiresAt))
}

func ValidateMidjourneyImageURL(mjID string, userID int, expiresAt int64, signature string, now time.Time) bool {
	if strings.TrimSpace(mjID) == "" || userID <= 0 || expiresAt <= now.Unix() || signature == "" {
		return false
	}
	expected := SignMidjourneyImageURL(mjID, userID, expiresAt)
	return hmac.Equal([]byte(expected), []byte(signature))
}

func BuildMidjourneyImageURL(serverAddress, mjID string, userID int, now time.Time) string {
	expiresAt := now.Add(MidjourneyImageURLTTL).Unix()
	query := url.Values{}
	query.Set("uid", strconv.Itoa(userID))
	query.Set("expires", strconv.FormatInt(expiresAt, 10))
	query.Set("signature", SignMidjourneyImageURL(mjID, userID, expiresAt))
	return strings.TrimRight(serverAddress, "/") + "/mj/image/" + url.PathEscape(mjID) + "?" + query.Encode()
}
