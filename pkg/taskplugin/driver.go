// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

// PreparedSubmission freezes the descriptor and estimated facts before the
// independent host authorization/reservation callback. It is single-send, has
// no persistence authority and must never be serialized with channel secrets.
type PreparedSubmission struct {
	executor   *Executor
	channel    ChannelInput
	body       json.RawMessage
	descriptor RequestDescriptor
	facts      types.TaskPricingFacts
	sent       atomic.Bool
}

// ExecutionTask contains private supplier state, not a client task view. The
// host must persist it against the original task/owner before starting polling.
// A response here is not proof that that persistence or a funds write happened.
type ExecutionTask struct {
	Binding        ExecutionBinding `json:"binding"`
	UpstreamTaskID string           `json:"upstream_task_id"`
	Data           json.RawMessage  `json:"data"`
	State          json.RawMessage  `json:"state"`
}

type SubmitResult struct {
	Task          ExecutionTask
	Immediate     *NormalizedResult
	OnSubmitFacts types.TaskPricingFacts
}

type NormalizedResult struct {
	TaskID           string   `json:"taskId,omitempty"`
	Status           string   `json:"status"`
	Progress         string   `json:"progress,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	URL              string   `json:"url,omitempty"`
	RemoteURL        string   `json:"remoteUrl,omitempty"`
	CompletionTokens *float64 `json:"completionTokens,omitempty"`
	TotalTokens      *float64 `json:"totalTokens,omitempty"`
}

type PollResult struct {
	Result NormalizedResult
	Data   json.RawMessage
	// Nil means no final facts were supplied, whereas {"units":0} is an
	// explicit zero. Estimated facts are not merged or replaced here.
	ActualFacts types.TaskPricingFacts
}

func (e *Executor) NewHTTPBroker(channel ChannelInput, client *http.Client, validateURL func(string) error) (*HTTPBroker, error) {
	if _, err := e.driverContext(channel, nil); err != nil {
		return nil, err
	}
	p, err := e.current()
	if err != nil {
		return nil, err
	}
	return NewHTTPBroker(channel.BaseURL, p.manifest.AllowedHosts, client, validateURL)
}

func (e *Executor) Prepare(ctx context.Context, channel ChannelInput, body json.RawMessage) (*PreparedSubmission, error) {
	body = bytes.Clone(body)
	channel.Config = bytes.Clone(channel.Config)
	channel.Credential = bytes.Clone(channel.Credential)
	descriptor, err := e.PrepareSubmit(ctx, channel, body)
	if err != nil {
		return nil, err
	}
	input, err := e.driverContext(channel, body)
	if err != nil {
		return nil, err
	}
	facts, err := e.facts(ctx, "extractUsage", input)
	if err != nil {
		return nil, err
	}
	return &PreparedSubmission{executor: e, channel: channel, body: body, descriptor: descriptor, facts: facts}, nil
}

func (p *PreparedSubmission) Facts() types.TaskPricingFacts {
	out := types.TaskPricingFacts{}
	if p == nil {
		return nil
	}
	if p.facts == nil {
		return nil
	}
	for k, v := range p.facts {
		out[k] = v
	}
	return out
}

func (e *Executor) checkBroker(b *HTTPBroker, channel ChannelInput) error {
	p, err := e.current()
	if err != nil {
		return err
	}
	base, err := parsePluginURL(channel.BaseURL)
	if err != nil || b == nil || pluginOrigin(base) != pluginOrigin(b.base) || base.Path != b.base.Path || base.RawQuery != b.base.RawQuery || !slices.Equal(p.manifest.AllowedHosts, b.allowed) {
		return ErrRequestPolicy
	}
	return nil
}

// Submit never retries. Every error after dispatch that prevents obtaining a
// valid supplier task identity is unknown, including a JS parse failure or a
// revocation while the response was in flight. Do not turn it into a refund and
// resubmit: the host's durable pending/unknown operation must reconcile it.
func (p *PreparedSubmission) Submit(ctx context.Context, b *HTTPBroker, authorize func(ExecutionBinding) error) (*SubmitResult, error) {
	if p == nil || authorize == nil {
		return nil, ErrRequestNotAuthorized
	}
	e := p.executor
	if err := e.checkBroker(b, p.channel); err != nil {
		return nil, err
	}
	response, err := b.Do(ctx, SubmitRequest, p.descriptor, func() error {
		if err := e.CheckSubmit(); err != nil {
			return err
		}
		// Claim before invoking authorization so two callers cannot reserve twice.
		// Even a denied/unknown authorization is not repeated by this object.
		if !p.sent.CompareAndSwap(false, true) {
			return ErrRequestNotAuthorized
		}
		if err := authorize(e.binding); err != nil {
			return err
		}
		// Recheck after the callback: reservation may have raced an admin revoke.
		if err := e.CheckSubmit(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, ErrSubmissionUnknown
	}
	input, err := e.driverContext(p.channel, p.body)
	if err != nil {
		return nil, ErrSubmissionUnknown
	}
	raw, err := e.call(ctx, "parseSubmitResponse", input, map[string]any{"statusCode": response.StatusCode, "headers": response.Headers, "body": json.RawMessage(response.Body)})
	if err != nil {
		return nil, ErrSubmissionUnknown
	}
	var parsed struct {
		TaskID    string            `json:"taskId"`
		Data      json.RawMessage   `json:"taskData"`
		State     json.RawMessage   `json:"state"`
		Immediate *NormalizedResult `json:"immediate"`
	}
	if common.Unmarshal(raw, &parsed) != nil || !validUpstreamTaskID(parsed.TaskID) {
		return nil, ErrSubmissionUnknown
	}
	data := parsed.Data
	if len(data) == 0 {
		data = bytes.Clone(response.Body)
	}
	submitted := &SubmitResult{Task: ExecutionTask{Binding: e.binding, UpstreamTaskID: parsed.TaskID, Data: data, State: parsed.State}}
	if parsed.Immediate != nil && !validNormalizedResult(*parsed.Immediate, parsed.TaskID) {
		return submitted, ErrSubmittedResultInvalid
	}
	actual, err := e.facts(ctx, "extractUsageOnSubmit", input, data)
	if err != nil {
		return submitted, ErrSubmittedResultInvalid
	}
	submitted.Immediate, submitted.OnSubmitFacts = parsed.Immediate, actual
	return submitted, nil
}

func (e *Executor) Poll(ctx context.Context, b *HTTPBroker, channel ChannelInput, task ExecutionTask, authorize func(ExecutionBinding) error) (*PollResult, error) {
	if authorize == nil {
		return nil, ErrRequestNotAuthorized
	}
	if err := e.Check(); err != nil {
		return nil, err
	}
	if task.Binding != e.binding || !validUpstreamTaskID(task.UpstreamTaskID) {
		return nil, ErrBindingRejected
	}
	if err := e.checkBroker(b, channel); err != nil {
		return nil, err
	}
	descriptor, err := e.PrepareQuery(ctx, channel, task.UpstreamTaskID, task.Data, task.State)
	if err != nil {
		return nil, err
	}
	response, err := b.Do(ctx, QueryRequest, descriptor, func() error {
		if err := e.Check(); err != nil {
			return err
		}
		if err := authorize(e.binding); err != nil {
			return err
		}
		return e.Check()
	})
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, ErrResponseUnavailable
	}
	input, err := e.driverContext(channel, nil)
	if err != nil {
		return nil, err
	}
	input["taskId"], input["data"], input["state"] = task.UpstreamTaskID, task.Data, task.State
	headers := map[string]string{}
	for k, v := range response.Headers {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	raw, err := e.call(ctx, "parseTaskResult", input, json.RawMessage(response.Body), map[string]any{"status": response.StatusCode, "headers": headers})
	if err != nil {
		return nil, err
	}
	var result NormalizedResult
	if common.Unmarshal(raw, &result) != nil || !validNormalizedResult(result, task.UpstreamTaskID) {
		return nil, ErrBindingRejected
	}
	var facts types.TaskPricingFacts
	if result.Status == "SUCCESS" {
		facts, err = e.facts(ctx, "extractUsageOnComplete", input, json.RawMessage(raw), json.RawMessage(response.Body))
		if err != nil {
			return nil, err
		}
	}
	return &PollResult{Result: result, Data: bytes.Clone(response.Body), ActualFacts: facts}, nil
}

func validUpstreamTaskID(id string) bool {
	if id == "" || len(id) > 1024 || !isASCII(id) || strings.ContainsAny(id, "?#%\\") || strings.IndexFunc(id, func(r rune) bool { return r <= 32 || r == 127 }) >= 0 {
		return false
	}
	for _, part := range strings.Split(id, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validNormalizedResult(result NormalizedResult, id string) bool {
	if result.TaskID != "" && result.TaskID != id {
		return false
	}
	if !slices.Contains([]string{"NOT_START", "SUBMITTED", "QUEUED", "IN_PROGRESS", "SUCCESS", "FAILURE", "UNKNOWN"}, result.Status) {
		return false
	}
	for _, value := range []*float64{result.CompletionTokens, result.TotalTokens} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > float64(1<<53-1)) {
			return false
		}
	}
	return true
}

func (e *Executor) facts(ctx context.Context, hook string, args ...any) (types.TaskPricingFacts, error) {
	p, err := e.current()
	if err != nil {
		return nil, err
	}
	if !slices.Contains(p.program.module.Exports(), hook) {
		return nil, nil
	}
	raw, err := e.call(ctx, hook, args...)
	if err != nil {
		return nil, err
	}
	var facts types.TaskPricingFacts
	if common.Unmarshal(raw, &facts) != nil {
		return nil, ErrPluginHook
	}
	schema := p.manifest.UsageSchema
	for _, profile := range p.manifest.UsageProfiles {
		if slices.Contains(profile.Models, e.binding.DeclaredModel) {
			schema = profile.Schema
			break
		}
	}
	return NormalizeDriverFacts(schema, facts, hook == "extractUsageOnComplete")
}
