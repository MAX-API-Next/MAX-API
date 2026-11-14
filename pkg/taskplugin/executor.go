// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
)

const ExecutionSDK = "max-api/task-plugin-sdk@1"

// ExecutionBinding pins code and routing identity, never a price or a funding
// permission. Persist this with the original task, not a polling HTTP request.
type ExecutionBinding struct {
	Version       int    `json:"version"`
	SDK           string `json:"sdk"`
	PluginKey     string `json:"plugin_key"`
	PluginVersion string `json:"plugin_version"`
	PackageDigest string `json:"package_digest"`
	OriginalModel string `json:"original_model"`
	UpstreamModel string `json:"upstream_model"`
	DeclaredModel string `json:"declared_model"`
	Action        string `json:"action"`
	Actor         Actor  `json:"actor"`
}

type Executor struct {
	host    *Host
	binding ExecutionBinding
	actor   Actor
}

func (host *Host) BindExecutor(actor Actor, key, original, mapped, action string) (*Executor, error) {
	if host == nil || !validActor(actor) || strings.TrimSpace(original) != original || original == "" || mapped == "" || strings.TrimSpace(mapped) != mapped || strings.TrimSpace(action) != action {
		return nil, ErrBindingRejected
	}
	state := host.state.Load()
	if state == nil {
		return nil, ErrBindingRejected
	}
	p := state.active[key]
	if !p.Executable() || state.revoked[packageIdentity(key, p.manifest.Version)] {
		return nil, ErrBindingRejected
	}
	declared := mapped
	if !declaresModel(p.manifest, declared) {
		declared = original
	}
	if !declaresModel(p.manifest, declared) {
		return nil, ErrBindingRejected
	}
	binding := ExecutionBinding{Version: 1, SDK: ExecutionSDK, PluginKey: key, PluginVersion: p.manifest.Version, PackageDigest: p.digest, OriginalModel: original, UpstreamModel: mapped, DeclaredModel: declared, Action: action, Actor: actor}
	return &Executor{host: host, binding: binding, actor: actor}, nil
}

func (host *Host) RestoreExecutor(binding ExecutionBinding, actor Actor) (*Executor, error) {
	e := &Executor{host: host, binding: binding, actor: actor}
	if _, err := e.current(); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Executor) Binding() ExecutionBinding { return e.binding }

func (e *Executor) current() (*Package, error) {
	if e == nil || e.host == nil || !validActor(e.actor) || e.binding.Actor != e.actor || e.binding.Version != 1 || e.binding.SDK != ExecutionSDK || e.binding.OriginalModel == "" || e.binding.UpstreamModel == "" || strings.TrimSpace(e.binding.OriginalModel) != e.binding.OriginalModel || strings.TrimSpace(e.binding.UpstreamModel) != e.binding.UpstreamModel || strings.TrimSpace(e.binding.Action) != e.binding.Action {
		return nil, ErrBindingRejected
	}
	s := e.host.state.Load()
	if s == nil {
		return nil, ErrBindingRejected
	}
	id := packageIdentity(e.binding.PluginKey, e.binding.PluginVersion)
	p := s.history[id]
	if !p.Executable() || s.revoked[id] || p.digest != e.binding.PackageDigest || !declaresModel(p.manifest, e.binding.DeclaredModel) {
		return nil, ErrBindingRejected
	}
	declared := e.binding.UpstreamModel
	if !declaresModel(p.manifest, declared) {
		declared = e.binding.OriginalModel
	}
	if e.binding.DeclaredModel != declared {
		return nil, ErrBindingRejected
	}
	return p, nil
}

func (e *Executor) Check() error { _, err := e.current(); return err }

// CheckSubmit additionally requires the pinned version to still be active.
// Disabling or upgrading a driver stops new submissions, but does not lose an
// old task's query code. Revocation stops both paths via current().
func (e *Executor) CheckSubmit() error {
	p, err := e.current()
	if err != nil {
		return err
	}
	if e.host.state.Load().active[e.binding.PluginKey] != p {
		return ErrBindingRejected
	}
	return nil
}

func (e *Executor) call(ctx context.Context, hook string, args ...any) (json.RawMessage, error) {
	p, err := e.current()
	if err != nil {
		return nil, err
	}
	raw, err := p.Call(ctx, hook, args...)
	if err != nil {
		return nil, err
	}
	if err := e.Check(); err != nil {
		return nil, err
	}
	return raw, nil
}

// ChannelInput is supplied by the host's selected-channel/key lease. It must
// never be decoded from client JSON, nor sourced from another channel on retry.
type ChannelInput struct {
	ID                                               int
	BaseURL, APIKey, AuthHeader, UpstreamKind, Other string
	Config, Credential                               json.RawMessage
}

func (e *Executor) driverContext(channel ChannelInput, body json.RawMessage) (map[string]any, error) {
	if err := e.Check(); err != nil {
		return nil, err
	}
	if channel.ID != e.actor.ChannelID || channel.BaseURL == "" || (channel.UpstreamKind != "vendor" && channel.UpstreamKind != "new_api") {
		return nil, ErrBindingRejected
	}
	if len(body) > MaxPackageBytes {
		return nil, ErrPluginLimit
	}
	var value any
	if len(body) > 0 && common.Unmarshal(body, &value) != nil {
		return nil, ErrPluginHook
	}
	// Keep raw JSON (and its member order) across the engine argument boundary.
	config := channel.Config
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}
	p, err := e.current()
	if err != nil {
		return nil, err
	}
	if len(p.manifest.Upstreams) > 0 && !slices.Contains(p.manifest.Upstreams, channel.UpstreamKind) {
		return nil, ErrBindingRejected
	}
	return map[string]any{"requestBody": body, "requestHeaders": map[string]string{"Content-Type": "application/json", "Accept": "application/json"}, "model": e.binding.OriginalModel, "upstreamModel": e.binding.UpstreamModel, "action": e.binding.Action, "baseUrl": channel.BaseURL, "apiKey": channel.APIKey, "authHeader": channel.AuthHeader, "upstream": map[string]any{"kind": channel.UpstreamKind}, "config": config, "credential": channel.Credential, "channelOther": channel.Other, "files": []any{}, "usagePurpose": "facts"}, nil
}

type RequestDescriptor struct {
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	Headers        map[string]string `json:"headers"`
	Body           json.RawMessage   `json:"body,omitempty"`
	ResponseType   string            `json:"responseType,omitempty"`
	BodyType       string            `json:"bodyType,omitempty"`
	Credentialless bool              `json:"credentialless,omitempty"`
	Action         string            `json:"action,omitempty"`
	Model          string            `json:"model,omitempty"`
	RewriteModel   string            `json:"rewriteModel,omitempty"`
	Parts          json.RawMessage   `json:"parts,omitempty"`
}

func (e *Executor) PrepareSubmit(ctx context.Context, channel ChannelInput, body json.RawMessage) (RequestDescriptor, error) {
	if err := e.CheckSubmit(); err != nil {
		return RequestDescriptor{}, err
	}
	input, err := e.driverContext(channel, body)
	if err != nil {
		return RequestDescriptor{}, err
	}
	raw, err := e.call(ctx, "buildSubmitRequest", input)
	if err != nil {
		return RequestDescriptor{}, err
	}
	var out RequestDescriptor
	if common.Unmarshal(raw, &out) != nil {
		return out, ErrPluginHook
	}
	// API v1 model is the logical/billing model. rewriteModel is the actual
	// upstream model; confusing them breaks otherwise valid channel aliases.
	if out.Action != "" && out.Action != e.binding.Action || out.Model != "" && out.Model != e.binding.OriginalModel || out.RewriteModel != "" && out.RewriteModel != e.binding.UpstreamModel {
		return RequestDescriptor{}, ErrBindingRejected
	}
	return out, nil
}

// DecodeInput deliberately has no credentials, mapped model, actor or arbitrary
// JS context. Those values are assigned by the selected-channel host binding.
// File, multipart and origin-task inputs need their own ownership gate first.
type DecodeInput struct {
	Method, Path string
	Query        map[string][]string
	Body         json.RawMessage
}

type SubmitIntent struct {
	Kind          string          `json:"kind"`
	Model         string          `json:"model"`
	Action        string          `json:"action"`
	RequestBody   json.RawMessage `json:"requestBody"`
	OriginTaskIDs []string        `json:"originTaskIds"`
}

func (e *Executor) decodeContext(input DecodeInput) (map[string]any, error) {
	if len(input.Body) == 0 || len(input.Body) > MaxPackageBytes {
		return nil, ErrPluginHook
	}
	var obj map[string]json.RawMessage
	if common.Unmarshal(input.Body, &obj) != nil || obj == nil {
		return nil, ErrPluginHook
	}
	return map[string]any{"method": input.Method, "path": input.Path, "query": input.Query, "params": map[string]string{}, "requestHeaders": map[string]string{"Content-Type": "application/json", "Accept": "application/json"}, "body": map[string]any{"kind": "json", "value": input.Body}}, nil
}

func (e *Executor) decodeIntent(ctx context.Context, hook string, input map[string]any, action string) (SubmitIntent, error) {
	raw, err := e.call(ctx, hook, input)
	if err != nil {
		return SubmitIntent{}, err
	}
	var intent SubmitIntent
	if common.Unmarshal(raw, &intent) != nil || intent.Kind != "submit" || intent.Model != e.binding.OriginalModel || len(intent.RequestBody) == 0 || string(intent.RequestBody) == "null" {
		return SubmitIntent{}, ErrBindingRejected
	}
	if intent.Action == "" {
		intent.Action = action
	}
	if intent.Action != e.binding.Action {
		return SubmitIntent{}, ErrBindingRejected
	}
	if len(intent.OriginTaskIDs) > 0 {
		return SubmitIntent{}, ErrPluginSDK
	}
	return intent, nil
}

func (e *Executor) DecodeProtocol(ctx context.Context, protocol, mode string, request DecodeInput) (SubmitIntent, error) {
	p, err := e.current()
	if err != nil {
		return SubmitIntent{}, err
	}
	allowed := false
	for _, claim := range p.manifest.Protocols {
		if claim.Name != protocol {
			continue
		}
		if len(claim.Models) > 0 && !slices.Contains(claim.Models, e.binding.DeclaredModel) {
			continue
		}
		if protocol == "openai_responses" && !slices.Contains(claim.Supports, mode) {
			continue
		}
		allowed = true
	}
	if !allowed {
		return SubmitIntent{}, ErrBindingRejected
	}
	input, err := e.decodeContext(request)
	if err != nil {
		return SubmitIntent{}, err
	}
	input["protocol"], input["operation"], input["model"], input["upstreamModel"], input["stream"] = protocol, "submit", e.binding.OriginalModel, e.binding.UpstreamModel, mode == "stream"
	return e.decodeIntent(ctx, "protocols."+protocol+".decodeRequest", input, "")
}

func (e *Executor) DecodeNative(ctx context.Context, request DecodeInput) (SubmitIntent, error) {
	p, err := e.current()
	if err != nil {
		return SubmitIntent{}, err
	}
	input, err := e.decodeContext(request)
	if err != nil {
		return SubmitIntent{}, err
	}
	path := request.Path
	// Native routes are also exposed under /{plugin-key}, as in the SDK.
	prefix := "/" + p.manifest.Key
	for _, route := range p.manifest.Routes {
		candidate := path
		if !routeMatches(route.Path, candidate) && strings.HasPrefix(candidate, prefix+"/") {
			candidate = strings.TrimPrefix(candidate, prefix)
		}
		if route.Type != "submit" || route.Method != request.Method || !routeMatches(route.Path, candidate) || !routeAllowsModel(route, e.binding.DeclaredModel) {
			continue
		}
		params := map[string]string{}
		for i, part := range strings.Split(route.Path, "/") {
			if strings.HasPrefix(part, ":") {
				value, err := url.PathUnescape(strings.Split(candidate, "/")[i])
				if err != nil {
					return SubmitIntent{}, ErrBindingRejected
				}
				params[part[1:]] = value
			}
		}
		input["params"] = params
		return e.decodeIntent(ctx, "native."+route.Decode, input, route.Action)
	}
	return SubmitIntent{}, ErrBindingRejected
}

func (e *Executor) PrepareQuery(ctx context.Context, channel ChannelInput, taskID string, data, state json.RawMessage) (RequestDescriptor, error) {
	if taskID == "" || len(taskID) > 1024 || strings.ContainsAny(taskID, "\r\n\x00") {
		return RequestDescriptor{}, ErrBindingRejected
	}
	input, err := e.driverContext(channel, nil)
	if err != nil {
		return RequestDescriptor{}, err
	}
	input["taskId"], input["data"], input["state"] = taskID, data, state
	raw, err := e.call(ctx, "buildQueryRequest", input)
	if err != nil {
		return RequestDescriptor{}, err
	}
	var out RequestDescriptor
	if common.Unmarshal(raw, &out) != nil {
		return out, ErrPluginHook
	}
	return out, nil
}
