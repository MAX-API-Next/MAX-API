// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/plugins"
	"github.com/stretchr/testify/require"
)

func klingExecutor(t *testing.T) (*Host, *Executor) {
	t.Helper()
	host := NewHost()
	_, err := host.Install(officialKling(t))
	require.NoError(t, err)
	e, err := host.BindExecutor(Actor{UserID: 11, TokenID: 22, ChannelID: 33, RequestID: "original-request"}, "kling", "kling-v2-5-turbo", "kling-v2-5-turbo", "text_to_video")
	require.NoError(t, err)
	return host, e
}

func localBroker(t *testing.T, e *Executor, server *httptest.Server) (ChannelInput, *HTTPBroker) {
	t.Helper()
	channel := ChannelInput{ID: 33, BaseURL: server.URL, APIKey: "synthetic-only", UpstreamKind: "vendor"}
	b, err := e.NewHTTPBroker(channel, server.Client(), func(raw string) error {
		if len(raw) < len(server.URL) || raw[:len(server.URL)] != server.URL {
			return ErrRequestPolicy
		}
		return nil
	})
	require.NoError(t, err)
	return channel, b
}

func TestOfficialDriverNativeProtocolAndHistoricalPoll(t *testing.T) {
	host, e := klingExecutor(t)
	var posts, gets atomic.Int32
	var actual atomic.Value
	actual.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/videos/text2video":
			posts.Add(1)
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			if common.Unmarshal(raw, &body) != nil || body["model_name"] != "kling-v2-5-turbo" || fmt.Sprint(body["duration"]) != "5" || r.Header.Get("Authorization") != "Bearer synthetic-only" {
				t.Error("wrong bound model, duration or credential")
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"task_id":"supplier-1"}}`)
		case "GET /v1/videos/text2video/supplier-1":
			gets.Add(1)
			_, _ = io.WriteString(w, `{"code":0,"data":{"task_id":"supplier-1","task_status":"succeed"`+actual.Load().(string)+`}}`)
		default:
			t.Error("unexpected supplier operation")
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	channel, b := localBroker(t, e, server)
	native, err := e.DecodeNative(t.Context(), DecodeInput{Method: "POST", Path: "/kling/v1/videos/text2video", Body: []byte(`{"model_name":"kling-v2-5-turbo","duration":5,"mode":"std","prompt":"local"}`)})
	require.NoError(t, err)
	protocol, err := e.DecodeProtocol(t.Context(), "openai_video", "", DecodeInput{Method: "POST", Path: "/v1/videos", Body: []byte(`{"model":"kling-v2-5-turbo","seconds":5,"mode":"std","prompt":"local"}`)})
	require.NoError(t, err)
	for _, intent := range []SubmitIntent{native, protocol} {
		prepared, err := e.Prepare(t.Context(), channel, intent.RequestBody)
		require.NoError(t, err)
		// Independent published consumption: 5 seconds * .3 credits/second.
		require.Equal(t, 1.5, prepared.Facts()["units"])
		facts := prepared.Facts()
		facts["units"] = 999
		require.Equal(t, 1.5, prepared.Facts()["units"], "caller cannot mutate prepared estimate")
		submitted, err := prepared.Submit(t.Context(), b, func(binding ExecutionBinding) error { require.Equal(t, e.Binding(), binding); return nil })
		require.NoError(t, err)
		require.Equal(t, "supplier-1", submitted.Task.UpstreamTaskID)
		_, err = prepared.Submit(t.Context(), b, func(ExecutionBinding) error { t.Error("same prepared operation authorized twice"); return nil })
		require.ErrorIs(t, err, ErrRequestNotAuthorized)
		var wire []byte
		wire, err = common.Marshal(submitted.Task)
		require.NoError(t, err)
		require.NotContains(t, string(wire), "synthetic-only", "execution task must not persist channel keys")
		var restored ExecutionTask
		require.NoError(t, common.Unmarshal(wire, &restored))
		for _, sample := range []struct {
			field string
			want  any
		}{{`,"final_unit_deduction":"0.6"`, 0.6}, {`,"final_unit_deduction":"0"`, float64(0)}, {"", nil}} {
			actual.Store(sample.field)
			polled, err := e.Poll(t.Context(), b, channel, restored, func(ExecutionBinding) error { return nil })
			require.NoError(t, err)
			require.Equal(t, "SUCCESS", polled.Result.Status)
			if sample.want == nil {
				require.Nil(t, polled.ActualFacts)
			} else {
				require.Equal(t, sample.want, polled.ActualFacts["units"])
			}
		}
		// A new host with history only has no active route but can query this task.
		restarted := NewHost()
		_, err = restarted.Retain(officialKling(t))
		require.NoError(t, err)
		old, err := restarted.RestoreExecutor(restored.Binding, restored.Binding.Actor)
		require.NoError(t, err)
		_, err = old.Prepare(t.Context(), channel, intent.RequestBody)
		require.ErrorIs(t, err, ErrBindingRejected)
		_, err = old.Poll(t.Context(), b, channel, restored, func(ExecutionBinding) error { return nil })
		require.NoError(t, err)
		require.NoError(t, restarted.Revoke("kling", "1.2.0"))
		_, err = old.Poll(t.Context(), b, channel, restored, func(ExecutionBinding) error { return nil })
		require.ErrorIs(t, err, ErrBindingRejected)
	}
	require.EqualValues(t, 2, posts.Load())
	require.EqualValues(t, 8, gets.Load())
	bad := e.Binding()
	bad.Actor.UserID++
	_, err = host.RestoreExecutor(bad, e.Binding().Actor)
	require.ErrorIs(t, err, ErrBindingRejected)
	bad = e.Binding()
	bad.PackageDigest = "other"
	_, err = host.RestoreExecutor(bad, bad.Actor)
	require.ErrorIs(t, err, ErrBindingRejected)
	_, err = e.Prepare(t.Context(), ChannelInput{ID: 99, BaseURL: server.URL, UpstreamKind: "vendor"}, native.RequestBody)
	require.ErrorIs(t, err, ErrBindingRejected)
}

func TestExecutionRejectsDecodedIdentityOrUnsupportedProtocol(t *testing.T) {
	_, e := klingExecutor(t)
	_, err := e.DecodeNative(t.Context(), DecodeInput{Method: "POST", Path: "/v1/videos/text2video", Body: []byte(`{"model_name":"other","duration":5}`)})
	require.ErrorIs(t, err, ErrBindingRejected)
	_, err = e.DecodeProtocol(t.Context(), "openai_responses", "unsupported", DecodeInput{Body: []byte(`{}`)})
	require.ErrorIs(t, err, ErrBindingRejected)
	_, err = e.DecodeProtocol(t.Context(), "openai_image", "", DecodeInput{Body: []byte(`{}`)})
	require.ErrorIs(t, err, ErrBindingRejected)
	_, err = e.DecodeProtocol(t.Context(), "openai_video", "", DecodeInput{Body: []byte(`{"input_reference":"image","seconds":5}`)})
	require.ErrorIs(t, err, ErrBindingRejected, "decoder action cannot switch a frozen text task into an image task")
}

func TestConcurrentPreparedSendCallsAuthorizationOnce(t *testing.T) {
	_, e := klingExecutor(t)
	var posts, grants atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_, _ = io.WriteString(w, `{"code":0,"data":{"task_id":"supplier-1"}}`)
	}))
	defer server.Close()
	channel, b := localBroker(t, e, server)
	prepared, err := e.Prepare(t.Context(), channel, []byte(`{"duration":5}`))
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, _ = prepared.Submit(t.Context(), b, func(ExecutionBinding) error { grants.Add(1); return nil })
		})
	}
	wg.Wait()
	require.EqualValues(t, 1, grants.Load())
	require.EqualValues(t, 1, posts.Load())
}

func syntheticExecutable(t *testing.T, extra string) *Package {
	t.Helper()
	source := `export const meta={apiVersion:1,key:"synthetic",name:"Synthetic",version:"1.0.0",models:["m"],usageSchema:{units:{type:"number",unit:"credit"}}}; export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit",method:"POST",model:ctx.model,rewriteModel:ctx.upstreamModel,headers:{"Content-Type":"application/json"},body:{z:1,a:0}}} export function parseSubmitResponse(ctx,r){return {taskId:r.body.id}} export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/query",method:"GET"}} export function parseTaskResult(ctx,body){return body}` + extra
	sum := sha256.Sum256([]byte(source))
	p, err := LoadJavaScript(bytes.NewBufferString(source), fmt.Sprintf("%x", sum))
	require.NoError(t, err)
	return p
}

func TestAcceptedSubmissionParseFailureIsUnknownAndCannotReplay(t *testing.T) {
	host := NewHost()
	_, err := host.Install(syntheticExecutable(t, ""))
	require.NoError(t, err)
	e, err := host.BindExecutor(Actor{UserID: 11, TokenID: 22, ChannelID: 33, RequestID: "request"}, "synthetic", "m", "m", "")
	require.NoError(t, err)
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		raw, _ := io.ReadAll(r.Body)
		if string(raw) != `{"z":1,"a":0}` {
			t.Error("signed JSON order changed")
		}
		_, _ = io.WriteString(w, `{"accepted":true}`)
	}))
	defer server.Close()
	channel, b := localBroker(t, e, server)
	prepared, err := e.Prepare(context.Background(), channel, []byte(`{}`))
	require.NoError(t, err)
	_, err = prepared.Submit(t.Context(), b, func(ExecutionBinding) error { return nil })
	require.ErrorIs(t, err, ErrSubmissionUnknown)
	_, err = prepared.Submit(t.Context(), b, func(ExecutionBinding) error { t.Error("replayed grant"); return nil })
	require.ErrorIs(t, err, ErrRequestNotAuthorized)
	require.EqualValues(t, 1, posts.Load())
}

func TestPrepareKeepsLogicalModelSeparateFromUpstreamRewrite(t *testing.T) {
	host := NewHost()
	_, err := host.Install(syntheticExecutable(t, ""))
	require.NoError(t, err)
	e, err := host.BindExecutor(Actor{UserID: 11, TokenID: 22, ChannelID: 33, RequestID: "request"}, "synthetic", "client-alias", "m", "")
	require.NoError(t, err)
	descriptor, err := e.PrepareSubmit(t.Context(), ChannelInput{ID: 33, BaseURL: "https://provider.invalid", UpstreamKind: "vendor"}, []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, "client-alias", descriptor.Model)
	require.Equal(t, "m", descriptor.RewriteModel)
	require.Equal(t, "client-alias", e.Binding().OriginalModel)
	require.Equal(t, "m", e.Binding().UpstreamModel)
}

func TestRevokeDuringAuthorizationDoesNotDispatch(t *testing.T) {
	host, e := klingExecutor(t)
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { posts.Add(1) }))
	defer server.Close()
	channel, b := localBroker(t, e, server)
	prepared, err := e.Prepare(t.Context(), channel, []byte(`{"duration":5}`))
	require.NoError(t, err)
	_, err = prepared.Submit(t.Context(), b, func(ExecutionBinding) error { return host.Revoke("kling", "1.2.0") })
	require.ErrorIs(t, err, ErrRequestNotAuthorized)
	require.Zero(t, posts.Load())
}

func TestPollRejectsForeignSupplierTaskAndInvalidUsage(t *testing.T) {
	host := NewHost()
	_, err := host.Install(syntheticExecutable(t, `export function extractUsageOnComplete(){return {units:-1}}`))
	require.NoError(t, err)
	e, err := host.BindExecutor(Actor{UserID: 11, TokenID: 22, ChannelID: 33, RequestID: "request"}, "synthetic", "m", "m", "")
	require.NoError(t, err)
	var foreign atomic.Bool
	foreign.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := "supplier-1"
		if foreign.Load() {
			id = "other-task"
		}
		_, _ = io.WriteString(w, `{"taskId":"`+id+`","status":"SUCCESS"}`)
	}))
	defer server.Close()
	channel, b := localBroker(t, e, server)
	task := ExecutionTask{Binding: e.Binding(), UpstreamTaskID: "supplier-1"}
	_, err = e.Poll(t.Context(), b, channel, task, func(ExecutionBinding) error { return nil })
	require.ErrorIs(t, err, ErrBindingRejected)
	foreign.Store(false)
	_, err = e.Poll(t.Context(), b, channel, task, func(ExecutionBinding) error { return nil })
	require.ErrorIs(t, err, ErrPluginHook)
}

func TestKnownSupplierIDSurvivesInvalidSubmitFacts(t *testing.T) {
	host := NewHost()
	_, err := host.Install(syntheticExecutable(t, `export function extractUsageOnSubmit(){return {units:-1}}`))
	require.NoError(t, err)
	e, err := host.BindExecutor(Actor{UserID: 11, TokenID: 22, ChannelID: 33, RequestID: "request"}, "synthetic", "m", "m", "")
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"id":"accepted-1"}`) }))
	defer server.Close()
	channel, b := localBroker(t, e, server)
	prepared, err := e.Prepare(t.Context(), channel, []byte(`{}`))
	require.NoError(t, err)
	result, err := prepared.Submit(t.Context(), b, func(ExecutionBinding) error { return nil })
	require.ErrorIs(t, err, ErrSubmittedResultInvalid)
	require.NotNil(t, result)
	require.Equal(t, "accepted-1", result.Task.UpstreamTaskID)
	require.Nil(t, result.OnSubmitFacts, "invalid evidence must not become zero or estimated usage")
}

func TestOfficialModelProfilesRetainCompatibleAuxiliaryFacts(t *testing.T) {
	for _, sample := range []struct {
		key, version, pin, model, request, keyToKeep string
		value                                        any
	}{
		{"hailuo", "1.4.0", "f5fed7c4c76d8f8d1a03da22f8a87fa61a28fdba577b613c045ec48e6d48b7d9", "MiniMax-Hailuo-2.3", `{"duration":6,"resolution":"768P","prompt":"test"}`, "input_images", float64(0)},
		{"doubao", "1.5.0", "8b3c9de6b03dc4ca4aff2e2848f60563aa9f07d020449211903de056c85dbfa6", "doubao-seedance-1-0-pro-250528", `{"seconds":5,"metadata":{"resolution":"480p"},"prompt":"test"}`, "video_input", "none"},
	} {
		t.Run(sample.key, func(t *testing.T) {
			source, err := plugins.TaskSources.ReadFile("tasks/" + sample.key + "/" + sample.version + "/plugin.js")
			require.NoError(t, err)
			p, err := LoadJavaScript(bytes.NewReader(source), sample.pin)
			require.NoError(t, err)
			schema, err := p.ModelUsageSchema(sample.model)
			require.NoError(t, err)
			require.NotContains(t, schema, sample.keyToKeep, "test must exercise an auxiliary fact outside the selected model profile")
			host := NewHost()
			_, err = host.Install(p)
			require.NoError(t, err)
			e, err := host.BindExecutor(Actor{UserID: 11, TokenID: 22, ChannelID: 33, RequestID: "request"}, sample.key, sample.model, sample.model, "text_to_video")
			require.NoError(t, err)
			prepared, err := e.Prepare(t.Context(), ChannelInput{ID: 33, BaseURL: "https://provider.invalid", APIKey: "synthetic-only", UpstreamKind: "vendor"}, []byte(sample.request))
			require.NoError(t, err)
			require.Equal(t, sample.value, prepared.Facts()[sample.keyToKeep])
		})
	}
}

func TestRestoredExecutionCannotSelectAnotherDeclaredModelsSchema(t *testing.T) {
	host, _ := klingExecutor(t)
	e, err := host.BindExecutor(Actor{UserID: 11, TokenID: 22, ChannelID: 33, RequestID: "request"}, "kling", "kling-v3", "kling-v2-5-turbo", "text_to_video")
	require.NoError(t, err)
	changed := e.Binding()
	changed.DeclaredModel = "kling-v3"
	_, err = host.RestoreExecutor(changed, changed.Actor)
	require.ErrorIs(t, err, ErrBindingRejected)
}

func TestPollPreservesTaskLocalDataStateAndRawEvidence(t *testing.T) {
	p := syntheticExecutable(t, "")
	host := NewHost()
	_, err := host.Install(p)
	require.NoError(t, err)
	e, err := host.BindExecutor(Actor{UserID: 11, TokenID: 22, ChannelID: 33, RequestID: "request"}, "synthetic", "m", "m", "")
	require.NoError(t, err)
	const upstream = `{"taskId":"supplier-1","status":"IN_PROGRESS","data":{"task_local":true},"state":{"extension_seconds":2}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, upstream) }))
	defer server.Close()
	channel, b := localBroker(t, e, server)
	result, err := e.Poll(t.Context(), b, channel, ExecutionTask{Binding: e.Binding(), UpstreamTaskID: "supplier-1"}, func(ExecutionBinding) error { return nil })
	require.NoError(t, err)
	require.JSONEq(t, `{"task_local":true}`, string(result.Data))
	require.JSONEq(t, `{"extension_seconds":2}`, string(result.Result.State))
	require.Equal(t, upstream, string(result.UpstreamBody))
	require.Nil(t, result.ActualFacts)
}
