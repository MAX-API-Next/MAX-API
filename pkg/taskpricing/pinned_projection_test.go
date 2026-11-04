// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

type pinnedProjectionFixture struct {
	Commit   string
	Packages []struct {
		Key          string
		SourceSHA256 string `json:"source_sha256"`
		SourceFile   string `json:"source_file"`
		Manifest     taskplugin.Manifest
	}
	Cases []struct {
		ID, Key, Model, Stage string
		Submit                types.TaskPricingFacts
		HookFacts             types.TaskPricingFacts `json:"hook_facts"`
		Raw                   json.RawMessage
		Expected              types.TaskPricingFacts
	}
}

func readPinnedProjection(t *testing.T) pinnedProjectionFixture {
	t.Helper()
	wire, err := os.ReadFile("testdata/pinned_plugin_projection.json")
	require.NoError(t, err)
	var fixture pinnedProjectionFixture
	require.NoError(t, common.Unmarshal(wire, &fixture))
	require.Equal(t, "d0abbf6e0d33552a41c402d9b1fadbaff27cf40d", fixture.Commit)
	return fixture
}

func pinnedProjectionPackage(t *testing.T, m taskplugin.Manifest, source string) *taskplugin.Package {
	t.Helper()
	wire, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 2, Manifest: m, Source: source})
	require.NoError(t, err)
	p, err := taskplugin.LoadPackage(bytes.NewReader(wire), fmt.Sprintf("%x", sha256.Sum256(wire)))
	require.NoError(t, err)
	return p
}

// These phase/presence decisions are manually reviewed host candidates, not
// inferred from plugin examples or installed as production policy.
func pinnedProjectionPolicy(t *testing.T, p *taskplugin.Package, model string) PhaseApproval {
	t.Helper()
	schema, err := p.ModelUsageSchema(model)
	require.NoError(t, err)
	c := types.TaskFactContract{Version: 1, UsageSchema: map[string]types.TaskUsageField{}, FieldPolicy: map[string]types.TaskFactPolicy{}}
	for key, f := range schema {
		c.UsageSchema[key] = types.TaskUsageField{Type: f.Type, Unit: f.Unit, Enum: f.Enum}
	}
	bound := func(n float64) *float64 { return &n }
	projection := &FactProjection{}
	switch model {
	case "MiniMax-H3":
		c.FieldPolicy = map[string]types.TaskFactPolicy{
			"seconds":             {Phase: "actual", Required: true, Integer: true, Min: bound(4), Max: bound(15)},
			"input_images":        {Phase: "actual", Required: true, Integer: true, Min: bound(0), Max: bound(9)},
			"input_video_seconds": {Phase: "actual", Required: true, Min: bound(0), Max: bound(15)},
			"resolution":          {Phase: "request", Required: true}}
		projection.StatusPath, projection.SuccessStates = "task.status", []string{"Success"}
		projection.ActualPaths = map[string][]string{"seconds": {"task.usage.output_seconds"}, "input_images": {"task.usage.input_image_count"}, "input_video_seconds": {"task.usage.input_seconds"}}
	case "MiniMax-Hailuo-2.3":
		c.FieldPolicy = map[string]types.TaskFactPolicy{"seconds": {Phase: "request", Required: true, Integer: true, Min: bound(6), Max: bound(10)}, "resolution": {Phase: "request", Required: true}}
		projection.SubmitExtras = types.TaskPricingFacts{"input_images": 0, "input_video_seconds": 0}
	case "kling-v3":
		c.FieldPolicy = map[string]types.TaskFactPolicy{"units": {Phase: "actual", Required: true, Min: bound(0)}}
		projection.StatusPath, projection.SuccessStates = "data.task_status", []string{"succeed"}
		projection.ActualPaths = map[string][]string{"units": {"data.final_unit_deduction"}}
	case "doubao-seedance-1-5-pro-251215":
		c.FieldPolicy = map[string]types.TaskFactPolicy{"tokens": {Phase: "actual", Required: true, Integer: true, Min: bound(1)}, "resolution": {Phase: "request", Required: true}, "generate_audio": {Phase: "request", Required: true}}
		projection.SubmitExtras = types.TaskPricingFacts{"video_input": "none"}
		projection.StatusPath, projection.SuccessStates = "status", []string{"succeeded"}
		projection.ActualPaths = map[string][]string{"tokens": {"usage.completion_tokens", "usage.total_tokens"}}
	default:
		t.Fatal("no reviewed candidate for model", model)
	}
	return PhaseApproval{PluginKey: p.Manifest().Key, Version: p.Manifest().Version, PackageDigest: p.Digest(), Model: model, Action: "generate", Contract: c, Projection: projection, Evidence: "test-only fixed upstream snapshot and raw hook fixtures; NOT provider cost approval"}
}

func TestPinnedPluginProjectionRejectsLossyHooksAndKeepsModelFacts(t *testing.T) {
	f := readPinnedProjection(t)
	packages := map[string]*taskplugin.Package{}
	for _, p := range f.Packages {
		packages[p.Key] = pinnedProjectionPackage(t, p.Manifest, "synthetic source identity: "+p.SourceSHA256)
	}
	for _, tc := range f.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			p := packages[tc.Key]
			r, err := NewPhaseRegistry([]PhaseApproval{pinnedProjectionPolicy(t, p, tc.Model)})
			require.NoError(t, err)
			a, err := r.Resolve(p, tc.Model, "generate")
			require.NoError(t, err)
			_, err = r.Resolve(p, "unreviewed-model", "generate")
			require.Error(t, err)
			var out ProjectedFacts
			if tc.Stage == "submit" {
				out, err = a.ProjectSubmit(tc.HookFacts)
			} else {
				out, err = a.ProjectFinal(tc.Submit, tc.HookFacts, tc.Raw)
			}
			if tc.Expected == nil {
				require.Error(t, err)
				// This stable observation ID has output_seconds=16 but its real
				// upstream hook omits seconds; it proves missing-fact rejection.
				if tc.ID == "h3-final-over" {
					require.ErrorContains(t, err, "missing required task fact \"seconds\"")
				}
				require.Nil(t, out.Facts)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.Expected, out.Facts, "expected facts are hand-authored, not produced by the implementation")
			if tc.ID == "kling-final" || tc.ID == "kling-zero" || tc.ID == "seedance-final" {
				expression, expected := `u("units") * 0.1`, 375
				if tc.ID == "kling-zero" {
					expected = 0
				}
				if tc.ID == "seedance-final" {
					expression, expected = `u("tokens") / 1000000.0 * (u("generate_audio") ? 5.0 : 3.0)`, 900
				}
				quote, err := billingexpr.QuoteTaskExpression(billingexpr.TaskQuoteInput{Profile: NewAPITask, Expression: expression, Usage: out.Facts, PricingAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), QuotaPerUnit: 1000, GroupRatio: 1.5})
				require.NoError(t, err)
				require.Equal(t, expected, quote.QuotaAfterGroup) // 2.5*.1*1500=375; .2*3*1500=900; zero=0
				_, err = QuoteExposure(ExposureInput{Profile: NewAPITask, Expression: expression, Contract: a.Contract, Submit: tc.Submit, PricingAt: time.Now(), QuotaPerUnit: 1000, GroupRatio: 1.5})
				require.ErrorIs(t, err, ErrExposureUnproven, "no invented maximum for real units/tokens")
			}
		})
	}
}

// Optional live-byte check: fixed source must be exported separately from the
// pinned Git commit. Go retains it as opaque data; Node hook execution is an
// independent isolated evidence step, not this test's oracle or a JS sandbox.
func TestPinnedPluginProjectionSourceBytes(t *testing.T) {
	dir := os.Getenv("TEST_PINNED_PLUGIN_SOURCE_DIR")
	if dir == "" {
		t.Skip("requires separately exported pinned upstream JS source")
	}
	for _, p := range readPinnedProjection(t).Packages {
		source, err := os.ReadFile(filepath.Join(dir, p.SourceFile))
		require.NoError(t, err)
		require.Equal(t, p.SourceSHA256, fmt.Sprintf("%x", sha256.Sum256(source)))
		loaded := pinnedProjectionPackage(t, p.Manifest, string(source))
		require.NotEqual(t, p.SourceSHA256, loaded.Digest(), "source SHA and MAX document SHA prove different bytes")
	}
}

// These are synthetic host inputs, not outputs claimed for the pinned JS hook.
// Keep the original hook observations byte-identical and exercise the host's
// reviewed 15-second bound independently with matching raw completion evidence.
func TestPinnedPluginProjectionH3ActualBounds(t *testing.T) {
	var p *taskplugin.Package
	for _, item := range readPinnedProjection(t).Packages {
		if item.Key == "hailuo" {
			p = pinnedProjectionPackage(t, item.Manifest, "synthetic host boundary")
		}
	}
	require.NotNil(t, p)
	registry, err := NewPhaseRegistry([]PhaseApproval{pinnedProjectionPolicy(t, p, "MiniMax-H3")})
	require.NoError(t, err)
	approval, err := registry.Resolve(p, "MiniMax-H3", "generate")
	require.NoError(t, err)
	for _, tc := range []struct {
		name     string
		seconds  int
		rejected bool
	}{{"maximum-15", 15, false}, {"over-16", 16, true}} {
		t.Run(tc.name, func(t *testing.T) {
			submit := types.TaskPricingFacts{"seconds": 5.0, "resolution": "768P", "input_images": 0.0, "input_video_seconds": 0.0}
			facts := types.TaskPricingFacts{"seconds": float64(tc.seconds), "resolution": "768P", "input_images": 0.0, "input_video_seconds": 0.0}
			raw := []byte(fmt.Sprintf("{\"task\":{\"status\":\"Success\",\"usage\":{\"output_seconds\":%d,\"input_image_count\":0,\"input_seconds\":0}}}", tc.seconds))
			out, err := approval.ProjectFinal(submit, facts, raw)
			if tc.rejected {
				require.ErrorContains(t, err, "invalid numeric task fact \"seconds\"")
				require.Nil(t, out.Facts)
				return
			}
			require.NoError(t, err)
			require.Equal(t, types.TaskPricingFacts{"seconds": 15.0, "resolution": "768P", "input_images": 0.0, "input_video_seconds": 0.0}, out.Facts)
		})
	}
}
