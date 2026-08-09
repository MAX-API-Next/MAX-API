package oauth

import (
	"fmt"
	"net/http"
	"time"

	"github.com/MAX-API-Next/MAX-API/service"
)

func newOAuthHTTPClient(endpoint string, timeout time.Duration) (*http.Client, error) {
	if err := service.ValidateSSRFProtectedFetchURL(endpoint); err != nil {
		return nil, fmt.Errorf("OAuth endpoint rejected: %w", err)
	}
	base := service.GetSSRFProtectedHttpClient()
	client := *base
	client.Timeout = timeout
	return &client, nil
}
