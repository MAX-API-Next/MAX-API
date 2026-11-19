// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTaskPluginManagementRejectsDuplicateMembers(t *testing.T) {
	for _, raw := range []string{
		`{"expected_sha256":"mismatch","expected_sha256":""}`,
		`{"expected_sha256":"","expected_sha256":""}`,
		`{"document":"first","document":"second"}`,
		`{"document":"first","docu\u006dent":"second"}`,
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(raw))
		_, err := taskPluginAdminFields(c, map[string]string{"document": "string", "expected_sha256": "string"})
		require.ErrorIs(t, err, taskplugin.ErrCatalogInvalid, raw)
	}
	for _, raw := range []string{`{"document":"source"}`, `{"document":"source","expected_sha256":""}`} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(raw))
		_, err := taskPluginAdminFields(c, map[string]string{"document": "string", "expected_sha256": "string"})
		require.NoError(t, err, raw)
	}
}

func TestTaskPluginDeletionOutcomeResponse(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "not-committed"}[known], func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			err := errors.Join(taskplugin.ErrCatalogUnavailable, errors.New("private-driver-detail"))
			if known {
				err = errors.Join(taskplugin.ErrCatalogNotCommitted, context.DeadlineExceeded)
			}
			taskPluginAdminError(c, err)
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			var response map[string]any
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, false, response["success"])
			if known {
				require.Equal(t, "catalog_operation_not_committed", response["code"])
				require.Equal(t, false, response["committed"])
			} else {
				require.NotContains(t, response, "committed")
			}
			require.NotContains(t, recorder.Body.String(), "private-driver-detail")
		})
	}
}
