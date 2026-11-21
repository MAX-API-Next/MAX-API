// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
)

func TestTaskPluginChannelConfigIsDerivedFromRetainedSource(t *testing.T) {
	p := syntheticExecutable(t, `; meta.channelConfig = {type:"object", properties:{region:{type:"string",default:"global"}}}; meta.keyHint = "API credential";`)
	document := p.Document()
	require.NotContains(t, string(document), `"channelConfig":`, "channel metadata must stay outside the persisted manifest")
	var envelope PackageDocument
	require.NoError(t, common.Unmarshal(document, &envelope))
	require.Empty(t, envelope.Manifest.ChannelConfig)
	require.Empty(t, envelope.Manifest.KeyHint)
	restored, err := LoadPackage(bytes.NewReader(document), p.Digest())
	require.NoError(t, err)
	require.Equal(t, p.Digest(), restored.Digest())
	require.Equal(t, "API credential", restored.Manifest().KeyHint)
	value, err := restored.Manifest().ResolveChannelConfig(nil, "vendor")
	require.NoError(t, err)
	require.JSONEq(t, `{"region":"global"}`, string(value))
}

func TestTaskPluginChannelConfigSchema(t *testing.T) {
	manifest := Manifest{
		Upstreams: []string{"vendor"},
		ChannelConfig: json.RawMessage(`{
		  "type":"object",
		  "required":["region"],
		  "properties":{
		    "region":{"type":"string","enum":["global","us"]},
		    "retries":{"type":"integer","minimum":0,"maximum":3,"default":2},
		    "flag":{"type":"boolean","default":true}
		  }
		}`),
	}
	result, err := manifest.ResolveChannelConfig(json.RawMessage(`{"region":"global","retries":0,"flag":false}`), "vendor")
	require.NoError(t, err)
	require.JSONEq(t, `{"region":"global","retries":0,"flag":false}`, string(result))
	result, err = manifest.ResolveChannelConfig(json.RawMessage(`{"region":"us"}`), "vendor")
	require.NoError(t, err)
	require.JSONEq(t, `{"region":"us","retries":2,"flag":true}`, string(result))
	for _, raw := range []string{
		`{}`, `null`, `[]`, `{"region":"eu"}`, `{"region":"us","retries":1.5}`,
		`{"region":"us","retries":4}`, `{"region":"us","flag":"false"}`,
		`{"region":"us","unknown":true}`,
	} {
		_, err := manifest.ResolveChannelConfig(json.RawMessage(raw), "vendor")
		require.ErrorIs(t, err, ErrBindingRejected)
	}
	_, err = manifest.ResolveChannelConfig(json.RawMessage(`{"region":"us"}`), "new_api")
	require.ErrorIs(t, err, ErrBindingRejected)
	manifest.ChannelConfig = json.RawMessage(`{"type":"object","properties":{"secret":{"type":"string","format":"password"}}}`)
	_, err = manifest.ChannelSchema()
	require.ErrorIs(t, err, ErrPluginSDK)
}
