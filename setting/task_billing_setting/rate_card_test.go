package task_billing_setting

import (
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

func withQuotaPerUnit(t *testing.T, value float64) {
	t.Helper()
	old := common.QuotaPerUnit
	common.QuotaPerUnit = value
	t.Cleanup(func() {
		common.QuotaPerUnit = old
	})
}

func TestCalculateKlingV3OmniNoVideoWithAudio(t *testing.T) {
	withQuotaPerUnit(t, 1000)

	input := types.TaskBillingInput{
		Model: "kling/kling-v3-omni-video-generation",
	}
	input.SetNumber("duration", 5)
	input.SetField("quality", "pro")
	input.SetField("has_video_input", "false")
	input.SetField("has_audio", "true")

	got, err := Calculate(input, 1)
	if err != nil {
		t.Fatalf("Calculate returned error: %v", err)
	}
	if got == nil {
		t.Fatal("Calculate returned nil")
	}
	if got.RowID != "pro_no_video_audio" {
		t.Fatalf("row = %q, want pro_no_video_audio", got.RowID)
	}
	if got.UnitPrice != 1.0 || got.TotalPrice != 5.0 || got.Quota != 5000 {
		t.Fatalf("unexpected billing result: %+v", got)
	}
}

func TestCalculateKlingV3OmniRejectsUnpricedVideoAudio(t *testing.T) {
	withQuotaPerUnit(t, 1000)

	input := types.TaskBillingInput{
		Model: "kling/kling-v3-omni-video-generation",
	}
	input.SetNumber("duration", 5)
	input.SetField("quality", "pro")
	input.SetField("has_video_input", "true")
	input.SetField("has_audio", "true")

	_, err := Calculate(input, 1)
	if err == nil {
		t.Fatal("expected an error for unconfigured video+audio price row")
	}
	if !strings.Contains(err.Error(), "no configured price row") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCalculateUnknownModelFallsBack(t *testing.T) {
	input := types.TaskBillingInput{Model: "unknown-video-model"}
	input.SetNumber("duration", 5)

	got, err := Calculate(input, 1)
	if err != nil {
		t.Fatalf("Calculate returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("Calculate returned %+v, want nil", got)
	}
}
