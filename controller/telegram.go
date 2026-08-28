package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/i18n"
	"github.com/MAX-API-Next/MAX-API/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	telegramAuthorizationTTL = 5 * time.Minute
	telegramClockSkew        = time.Minute
	telegramBindFlowTTL      = 5 * time.Minute
)

var telegramLoginFlowMatch = model.AuthFlowMatch{
	Purpose:  model.AuthFlowPurposeOAuth,
	Provider: "telegram",
	Intent:   model.AuthFlowIntentLogin,
}

type telegramAuthPayload struct {
	ID                  int64  `json:"id"`
	FirstName           string `json:"first_name,omitempty"`
	LastName            string `json:"last_name,omitempty"`
	Username            string `json:"username,omitempty"`
	PhotoURL            string `json:"photo_url,omitempty"`
	AuthDate            int64  `json:"auth_date"`
	Hash                string `json:"hash"`
	State               string `json:"state"`
	authorizationValues url.Values
}

func (p *telegramAuthPayload) UnmarshalJSON(data []byte) error {
	type plainTelegramAuthPayload telegramAuthPayload
	var decoded plainTelegramAuthPayload
	if err := common.Unmarshal(data, &decoded); err != nil {
		return err
	}

	var rawFields map[string]json.RawMessage
	if err := common.Unmarshal(data, &rawFields); err != nil {
		return err
	}
	authorizationValues := make(url.Values, len(rawFields))
	for key, rawValue := range rawFields {
		if key == "state" {
			continue
		}
		switch common.GetJsonType(rawValue) {
		case "string", "number", "boolean":
			authorizationValues.Set(key, common.JsonRawMessageToString(rawValue))
		default:
			return fmt.Errorf("Telegram authorization field %q must be a scalar value", key)
		}
	}

	*p = telegramAuthPayload(decoded)
	p.authorizationValues = authorizationValues
	return nil
}

func (p telegramAuthPayload) values() url.Values {
	values := make(url.Values, len(p.authorizationValues))
	for key, entries := range p.authorizationValues {
		values[key] = append([]string(nil), entries...)
	}
	return values
}

func GenerateTelegramBindState(c *gin.Context) {
	if !common.TelegramOAuthEnabled {
		common.ApiErrorMsg(c, "管理员未开启 Telegram 登录")
		return
	}
	userID := c.GetInt("id")
	state, _, err := model.CreateAuthFlow(model.AuthFlowCreate{
		Purpose:   model.AuthFlowPurposeOAuth,
		Provider:  "telegram",
		Intent:    model.AuthFlowIntentBind,
		UserId:    userID,
		ExpiresAt: time.Now().Add(telegramBindFlowTTL),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	session := sessions.Default(c)
	session.Set(oauthStateSessionKey, state)
	if err := session.Save(); err != nil {
		if _, consumeErr := model.ConsumeAuthFlow(state, model.AuthFlowMatch{
			Purpose:  model.AuthFlowPurposeOAuth,
			Provider: "telegram",
			Intent:   model.AuthFlowIntentBind,
			UserId:   userID,
		}); consumeErr != nil {
			common.SysError("failed to invalidate Telegram bind flow after session save failure: " + consumeErr.Error())
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"state": state})
}

func TelegramBind(c *gin.Context) {
	if !common.TelegramOAuthEnabled {
		common.ApiErrorMsg(c, "管理员未开启 Telegram 登录")
		return
	}
	var payload telegramAuthPayload
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !checkTelegramAuthorization(payload.values(), common.TelegramBotToken) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "无效或已过期的 Telegram 授权"})
		return
	}
	if !oauthSessionStateMatches(c, payload.State) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
		return
	}
	userID := c.GetInt("id")
	generation, err := model.BindTelegramIdentityWithAuthFlow(userID, strconv.FormatInt(payload.ID, 10), payload.State)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrTelegramIdAlreadyTaken):
			common.ApiErrorMsg(c, "该 Telegram 账户已被绑定")
		case errors.Is(err, model.ErrAuthFlowInvalid), errors.Is(err, model.ErrAuthFlowExpired), errors.Is(err, model.ErrAuthFlowConsumed):
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
		default:
			common.ApiError(c, err)
		}
		return
	}
	clearOAuthSessionState(c)
	preserveCurrentSessionAfterCommittedSecurityChange(c, userID, generation, "binding Telegram")
	recordUserSecurityAudit(c, userID, "user.telegram_bind", nil)
	common.ApiSuccess(c, nil)
}

func TelegramLogin(c *gin.Context) {
	if !common.TelegramOAuthEnabled {
		common.ApiErrorMsg(c, "管理员未开启 Telegram 登录")
		return
	}
	params := c.Request.URL.Query()
	if !checkTelegramAuthorization(params, common.TelegramBotToken) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "无效或已过期的 Telegram 授权"})
		return
	}
	state := params.Get("state")
	if !oauthSessionStateMatches(c, state) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
		return
	}
	user := model.User{TelegramId: params.Get("id")}
	if err := user.FillUserByTelegramId(); handleOAuthUserLookupError(c, err) {
		return
	}
	if _, err := model.ConsumeAuthFlow(state, telegramLoginFlowMatch); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
		return
	}
	clearOAuthSessionState(c)
	setupLogin(&user, c)
}

func checkTelegramAuthorization(params url.Values, token string) bool {
	return checkTelegramAuthorizationAt(params, token, time.Now())
}

func checkTelegramAuthorizationAt(params url.Values, token string, now time.Time) bool {
	if strings.TrimSpace(token) == "" || params.Get("id") == "" || params.Get("hash") == "" {
		return false
	}
	authDate, err := strconv.ParseInt(params.Get("auth_date"), 10, 64)
	if err != nil {
		return false
	}
	authorizedAt := time.Unix(authDate, 0)
	if authorizedAt.After(now.Add(telegramClockSkew)) || now.Sub(authorizedAt) > telegramAuthorizationTTL {
		return false
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		if key != "hash" && key != "state" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		values := params[key]
		if len(values) != 1 {
			return false
		}
		lines = append(lines, key+"="+values[0])
	}
	secret := sha256.Sum256([]byte(token))
	mac := hmac.New(sha256.New, secret[:])
	_, _ = mac.Write([]byte(strings.Join(lines, "\n")))
	provided, err := hex.DecodeString(params.Get("hash"))
	return err == nil && hmac.Equal(provided, mac.Sum(nil))
}
