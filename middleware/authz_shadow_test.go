package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAuthzShadowPreservesLegacyRequestAndRecordsDecision(t *testing.T) {
	previous := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.AuthzUserOverride{}))
	model.DB = db
	t.Cleanup(func() { model.DB = previous })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("id", 22)
		c.Set("role", common.RoleAdminUser)
		c.Set("username", "admin")
		c.Next()
	})
	r.GET("/channels", AuthzShadow(authz.Permission{Resource: authz.ResourceChannel, Action: authz.ActionRead}), func(c *gin.Context) {
		c.JSON(200, gin.H{"allowed": c.GetBool("authz_shadow_allowed"), "source": c.GetString("authz_shadow_source")})
	})
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest("GET", "/channels", nil))
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"allowed":true`)
}
