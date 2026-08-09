package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type turnstileCheckResponse struct {
	Success bool `json:"success"`
}

const turnstileTokenHeader = "X-Turnstile-Token"

var turnstileVerificationTimeout = 10 * time.Second
var turnstileVerificationEndpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
var turnstileHTTPClient = &http.Client{Timeout: turnstileVerificationTimeout}

func getTurnstileToken(c *gin.Context) string {
	if token := strings.TrimSpace(c.GetHeader(turnstileTokenHeader)); token != "" {
		return token
	}
	return strings.TrimSpace(c.Query("turnstile"))
}

func verifyTurnstile(ctx context.Context, response string, remoteIP string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, turnstileVerificationTimeout)
	defer cancel()
	form := url.Values{
		"secret":   {common.TurnstileSecretKey},
		"response": {response},
		"remoteip": {remoteIP},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, turnstileVerificationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rawRes, err := turnstileHTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer rawRes.Body.Close()
	if rawRes.StatusCode < http.StatusOK || rawRes.StatusCode >= http.StatusMultipleChoices {
		return false, fmt.Errorf("turnstile verification returned HTTP %d", rawRes.StatusCode)
	}

	var res turnstileCheckResponse
	if err := common.DecodeJson(rawRes.Body, &res); err != nil {
		return false, err
	}
	return res.Success, nil
}

func TurnstileCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.TurnstileCheckEnabled {
			session := sessions.Default(c)
			turnstileChecked := session.Get("turnstile")
			if turnstileChecked != nil {
				c.Next()
				return
			}
			response := getTurnstileToken(c)
			if response == "" {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "Turnstile token 为空",
				})
				c.Abort()
				return
			}
			verified, err := verifyTurnstile(c.Request.Context(), response, c.ClientIP())
			if err != nil {
				common.SysLog(err.Error())
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				c.Abort()
				return
			}
			if !verified {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "Turnstile 校验失败，请刷新重试！",
				})
				c.Abort()
				return
			}
			session.Set("turnstile", true)
			err = session.Save()
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"message": "无法保存会话信息，请重试",
					"success": false,
				})
				return
			}
		}
		c.Next()
	}
}
