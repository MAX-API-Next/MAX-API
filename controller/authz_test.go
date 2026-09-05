package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGetPermissionCatalogReturnsResourcesAndRoleBaselines(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	GetPermissionCatalog(c)
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"resources"`)
	assert.Contains(t, recorder.Body.String(), `"channel"`)
	assert.Contains(t, recorder.Body.String(), `"roles"`)
	assert.Contains(t, recorder.Body.String(), `"superuser":true`)
}
