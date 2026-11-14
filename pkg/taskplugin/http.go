// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Host policy independently implemented from the published Task Plugin API v1.
package taskplugin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"golang.org/x/net/http/httpguts"
)

var (
	ErrRequestPolicy          = errors.New("task plugin request rejected by host policy")
	ErrRequestNotAuthorized   = errors.New("task plugin send authorization unavailable")
	ErrSubmissionUnknown      = errors.New("task plugin submission outcome unknown; do not resubmit")
	ErrSubmittedResultInvalid = errors.New("task plugin task submitted but result evidence is invalid; retain task identity")
	ErrResponseUnavailable    = errors.New("task plugin upstream response unavailable")
)

type RequestPurpose string

const (
	SubmitRequest   RequestPurpose = "submit"
	QueryRequest    RequestPurpose = "query"
	ArtifactRequest RequestPurpose = "artifact"
)

type UpstreamResponse struct {
	StatusCode int                 `json:"statusCode"`
	Headers    map[string][]string `json:"headers"`
	Body       []byte              `json:"-"`
}

// HTTPBroker has no financial authority. Its client must be created by the
// host's protected DNS/proxy transport. Every send requires an independent
// host callback; an executable package or quote never supplies that callback.
type HTTPBroker struct {
	base        *url.URL
	allowed     []string
	client      *http.Client
	validateURL func(string) error
}

func NewHTTPBroker(baseURL string, allowed []string, client *http.Client, validateURL func(string) error) (*HTTPBroker, error) {
	base, err := parsePluginURL(baseURL)
	if err != nil || client == nil || validateURL == nil || validateAllowedHosts(allowed) != nil {
		return nil, ErrRequestPolicy
	}
	copyClient := *client
	copyClient.Jar = nil
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copyClient.Timeout == 0 || copyClient.Timeout > 15*time.Second {
		copyClient.Timeout = 15 * time.Second
	}
	return &HTTPBroker{base: base, allowed: append([]string(nil), allowed...), client: &copyClient, validateURL: validateURL}, nil
}

func parsePluginURL(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || len(value) > 65536 || !isASCII(value) || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, ErrRequestPolicy
	}
	return u, nil
}

func pluginOrigin(u *url.URL) string {
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return strings.ToLower(u.Scheme) + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

func (b *HTTPBroker) allowedURL(raw string, credentialless bool) (*url.URL, error) {
	u, err := parsePluginURL(raw)
	if err != nil {
		return nil, err
	}
	if b.base.Scheme == "https" && u.Scheme != "https" {
		return nil, ErrRequestPolicy
	}
	if credentialless || pluginOrigin(u) == pluginOrigin(b.base) {
		return u, nil
	}
	for _, host := range b.allowed {
		candidate, err := parsePluginURL(u.Scheme + "://" + host)
		if err == nil && pluginOrigin(u) == pluginOrigin(candidate) {
			return u, nil
		}
	}
	return nil, ErrRequestPolicy
}

func safePluginHeaders(input map[string]string, credentialless bool) (http.Header, error) {
	if len(input) > 64 {
		return nil, ErrRequestPolicy
	}
	out := http.Header{}
	seen := map[string]bool{}
	for name, value := range input {
		canonical := http.CanonicalHeaderKey(name)
		if !httpguts.ValidHeaderFieldName(name) || !httpguts.ValidHeaderFieldValue(value) || len(value) > 8192 || seen[canonical] {
			return nil, ErrRequestPolicy
		}
		seen[canonical] = true
		switch strings.ToLower(name) {
		case "host", "content-length", "accept-encoding", "connection", "proxy-connection", "keep-alive", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
			return nil, ErrRequestPolicy
		}
		if credentialless {
			switch canonical {
			case "Accept", "Range", "If-Range", "If-None-Match", "If-Modified-Since":
			default:
				continue
			}
		}
		out.Set(name, value)
	}
	return out, nil
}

func (b *HTTPBroker) validate(purpose RequestPurpose, descriptor RequestDescriptor) (string, *url.URL, http.Header, error) {
	if b == nil {
		return "", nil, nil, ErrRequestNotAuthorized
	}
	method := strings.ToUpper(strings.TrimSpace(descriptor.Method))
	if method == "" {
		method = "GET"
		if purpose == SubmitRequest {
			method = "POST"
		}
	}
	if purpose == SubmitRequest {
		if method != "POST" {
			return "", nil, nil, ErrRequestPolicy
		}
	} else if purpose == QueryRequest {
		// API v1 query hooks also use POST for Suno and Vertex long-running
		// operation lookups. The same host authorization and origin policy apply.
		if method != "GET" && method != "HEAD" && method != "POST" {
			return "", nil, nil, ErrRequestPolicy
		}
	} else if purpose == ArtifactRequest {
		if method != "GET" && method != "HEAD" {
			return "", nil, nil, ErrRequestPolicy
		}
	} else {
		return "", nil, nil, ErrRequestPolicy
	}
	if descriptor.ResponseType != "" && descriptor.ResponseType != "json" || descriptor.BodyType != "" && descriptor.BodyType != "json" || len(descriptor.Parts) > 0 {
		return "", nil, nil, ErrPluginSDK
	}
	if len(descriptor.Body) > MaxPackageBytes {
		return "", nil, nil, ErrRequestPolicy
	}
	if err := validateDescriptorBody(descriptor.Body); err != nil {
		return "", nil, nil, err
	}
	credentialless := purpose == ArtifactRequest && descriptor.Credentialless
	u, err := b.allowedURL(descriptor.URL, credentialless)
	if err != nil {
		return "", nil, nil, err
	}
	headers, err := safePluginHeaders(descriptor.Headers, credentialless)
	if err != nil {
		return "", nil, nil, err
	}
	if b.validateURL(u.String()) != nil {
		return "", nil, nil, ErrRequestPolicy
	}
	return method, u, headers, nil
}

func validateDescriptorBody(body []byte) error {
	if len(body) == 0 {
		return nil
	}
	var value any
	if common.Unmarshal(body, &value) != nil {
		return ErrRequestPolicy
	}
	// A top-level JSON string is serialized wire JSON (for signed requests).
	// Validate its structure too; send the original decoded bytes unchanged.
	if text, ok := value.(string); ok {
		if common.Unmarshal([]byte(text), &value) != nil {
			return ErrRequestPolicy
		}
	}
	var nodes int
	var walk func(any, int) error
	walk = func(value any, depth int) error {
		nodes++
		if depth > 32 || nodes > 32768 {
			return ErrPluginLimit
		}
		switch v := value.(type) {
		case map[string]any:
			if _, exists := v["__fileRef"]; exists {
				return ErrPluginSDK
			}
			for _, member := range v {
				if err := walk(member, depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, member := range v {
				if err := walk(member, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(value, 0)
}

func (b *HTTPBroker) Do(ctx context.Context, purpose RequestPurpose, descriptor RequestDescriptor, beforeSend func() error) (*UpstreamResponse, error) {
	if ctx == nil || b == nil || beforeSend == nil {
		return nil, ErrRequestNotAuthorized
	}
	method, u, headers, err := b.validate(purpose, descriptor)
	if err != nil {
		return nil, err
	}
	body := descriptor.Body
	if bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
		body = nil
	}
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '"' {
		// API v1 string descriptors contain already serialized/signed text.
		// Sending the JSON string's quotes would change both payload and signature.
		var text string
		if common.Unmarshal(body, &text) != nil {
			return nil, ErrRequestPolicy
		}
		body = []byte(text)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, ErrRequestPolicy
	}
	req.Header = headers
	// No transparent replay by net/http, even when a plugin sets an idempotency
	// header or the POST body is empty. Only the durable host may decide retries.
	req.GetBody = nil
	if purpose == SubmitRequest && req.Body == http.NoBody {
		req.Body = io.NopCloser(bytes.NewReader(nil))
		req.ContentLength = -1
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if beforeSend() != nil {
		return nil, ErrRequestNotAuthorized
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	response, err := b.client.Do(req)
	if err != nil {
		if purpose == SubmitRequest {
			return nil, ErrSubmissionUnknown
		}
		return nil, ErrResponseUnavailable
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, MaxPackageBytes+1))
	if err != nil || len(responseBody) > MaxPackageBytes {
		if purpose == SubmitRequest {
			return nil, ErrSubmissionUnknown
		}
		return nil, ErrResponseUnavailable
	}
	return &UpstreamResponse{StatusCode: response.StatusCode, Headers: response.Header.Clone(), Body: responseBody}, nil
}
