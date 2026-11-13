package controller

import (
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func sessionInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		id, err := strconv.Atoi(v)
		return id, err == nil
	default:
		return 0, false
	}
}

func sessionUserID(value any) (int, bool) {
	id, ok := sessionInt(value)
	return id, ok && id > 0
}

func accountBindingSession(c *gin.Context) (int, int64, bool) {
	session := sessions.Default(c)
	id, ok := sessionUserID(session.Get("id"))
	if !ok {
		return 0, 0, false
	}
	var generation int64
	switch value := session.Get("session_generation").(type) {
	case nil: // Legacy cookies are valid only while the stored generation is 0.
	case int64:
		generation = value
	case int:
		generation = int64(value)
	default:
		return 0, 0, false
	}
	return id, generation, generation >= 0
}
