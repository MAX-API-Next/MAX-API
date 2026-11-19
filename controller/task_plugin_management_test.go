// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

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
