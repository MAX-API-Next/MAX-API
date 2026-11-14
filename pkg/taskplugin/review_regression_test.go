// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
)

func reviewedSource(t *testing.T, source string) *Package {
	t.Helper()
	p, err := LoadJavaScript(bytes.NewBufferString(source), fmt.Sprintf("%x", sha256.Sum256([]byte(source))))
	require.NoError(t, err)
	return p
}

func TestNativeDecodingUsesRegistryPrecedenceAndMethodNormalization(t *testing.T) {
	for _, method := range []string{"POST", " post "} {
		t.Run(method, func(t *testing.T) {
			p := reviewedSource(t, `export const meta={apiVersion:1,key:"routes",name:"Routes",version:"1.0.0",models:["m"],routes:[{method:"`+method+`",path:"/jobs/:kind",type:"submit",action:"generic",decode:"generic",render:"render"},{method:"`+method+`",path:"/jobs/create",type:"submit",action:"literal",decode:"literal",render:"render"}]}; export const native={generic(){return {kind:"submit",model:"m",action:"generic",requestBody:{selected:"generic"}}},literal(){return {kind:"submit",model:"m",action:"literal",requestBody:{selected:"literal"}}},render(){return {}}};export function buildSubmitRequest(){return {}}export function parseSubmitResponse(){return {}}export function buildQueryRequest(){return {}}export function parseTaskResult(){return {}}`)
			host := NewHost()
			_, err := host.Install(p)
			require.NoError(t, err)
			e, err := host.BindExecutor(Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "r"}, "routes", "m", "m", "literal")
			require.NoError(t, err)
			for _, path := range []string{"/jobs/create", "/routes/jobs/create"} {
				intent, err := e.DecodeNative(t.Context(), DecodeInput{Method: "POST", Path: path, Body: []byte(`{}`)})
				require.NoError(t, err)
				require.JSONEq(t, `{"selected":"literal"}`, string(intent.RequestBody))
			}
		})
	}
}

func TestExecutableOptionalEmptyArraysSurvivePersistence(t *testing.T) {
	p := reviewedSource(t, `export const meta={apiVersion:1,key:"empty",name:"Empty",version:"1.0.0",models:["m"],channelTypes:[],usageProfiles:[],protocols:[{name:"openai_video",models:[]}]};export const protocols={openai_video:{decodeRequest(){return {}},render(){return {}}}};export function buildSubmitRequest(){return {}}export function parseSubmitResponse(){return {}}export function buildQueryRequest(){return {}}export function parseTaskResult(){return {}}`)
	_, err := LoadPackage(bytes.NewReader(p.Document()), p.Digest())
	require.NoError(t, err)
	var document PackageDocument
	require.NoError(t, common.Unmarshal(p.Document(), &document))
	document.Manifest.ChannelTypes = []int{99}
	raw, err := common.Marshal(document)
	require.NoError(t, err)
	_, err = LoadPackage(bytes.NewReader(raw), fmt.Sprintf("%x", sha256.Sum256(raw)))
	require.Error(t, err, "canonical comparison must still reject changed metadata")
}

func TestRetainingReloadedActiveIdentityAllowsNewSubmission(t *testing.T) {
	p1 := syntheticExecutable(t, "")
	p2, err := LoadPackage(bytes.NewReader(p1.Document()), p1.Digest())
	require.NoError(t, err)
	host := NewHost()
	_, err = host.Install(p1)
	require.NoError(t, err)
	e, err := host.BindExecutor(Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "r"}, "synthetic", "m", "m", "")
	require.NoError(t, err)
	_, err = host.Retain(p2)
	require.NoError(t, err)
	require.NoError(t, e.CheckSubmit(), "reloading identical immutable bytes cannot disable an active version")
	require.NoError(t, host.Revoke("synthetic", p1.Manifest().Version))
	require.ErrorIs(t, e.CheckSubmit(), ErrBindingRejected)
}
