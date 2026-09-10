package taskusage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestBuildEnvelopeDistinguishesAbsentAndExplicitZero(t *testing.T) {
	contract := DoubaoVideoContract()

	absent, err := BuildEnvelope(types.TaskUsageProducerKindGoAdapter, contract, types.TaskUsageSourceProviderResponse, nil)
	require.NoError(t, err)
	require.Equal(t, types.TaskUsagePresenceNotPresent, absent.Presence)
	require.Equal(t, types.TaskUsageCompletenessMissing, absent.Completeness)
	require.NotEmpty(t, absent.EvidenceDigest)

	zero := int64(0)
	explicitZero, err := BuildEnvelope(types.TaskUsageProducerKindGoAdapter, contract, types.TaskUsageSourceProviderResponse, &types.TaskUsage{
		CompletionTokens: &zero,
		TotalTokens:      &zero,
		Source:           types.TaskUsageSourceProviderResponse,
		Completeness:     types.TaskUsageCompletenessComplete,
	})
	require.NoError(t, err)
	require.Equal(t, types.TaskUsagePresencePresentZero, explicitZero.Presence)
	require.NotEqual(t, absent.EvidenceDigest, explicitZero.EvidenceDigest)
	require.NotNil(t, explicitZero.Usage.CompletionTokens)
	require.Zero(t, *explicitZero.Usage.CompletionTokens)
	require.NoError(t, ValidateEnvelope(contract, explicitZero))
}

func TestEvidenceDigestIsCanonicalAcrossProducerKindsAndFieldOrder(t *testing.T) {
	contract := MiniMaxH3Contract()
	output := int64(5_000)
	video := int64(0)
	audio := int64(0)
	images := int64(1)
	usage := &types.TaskUsage{
		OutputDurationMs:     &output,
		InputVideoDurationMs: &video,
		InputAudioDurationMs: &audio,
		InputImageCount:      &images,
		Source:               types.TaskUsageSourceProviderResponse,
		Completeness:         types.TaskUsageCompletenessComplete,
	}

	goEnvelope, err := BuildEnvelope(types.TaskUsageProducerKindGoAdapter, contract, types.TaskUsageSourceProviderResponse, usage)
	require.NoError(t, err)
	pluginEnvelope, err := BuildEnvelope(types.TaskUsageProducerKindTaskPlugin, contract, types.TaskUsageSourceProviderResponse, usage)
	require.NoError(t, err)
	require.Equal(t, goEnvelope.EvidenceDigest, pluginEnvelope.EvidenceDigest)
	require.Equal(t, goEnvelope.ContractDigest, pluginEnvelope.ContractDigest)
	require.NotEqual(t, goEnvelope.ProducerKind, pluginEnvelope.ProducerKind)

	reordered := contract
	reordered.Fields = append([]types.TaskUsageFieldContract(nil), contract.Fields...)
	for left, right := 0, len(reordered.Fields)-1; left < right; left, right = left+1, right-1 {
		reordered.Fields[left], reordered.Fields[right] = reordered.Fields[right], reordered.Fields[left]
	}
	digest, err := ContractDigest(reordered)
	require.NoError(t, err)
	require.Equal(t, goEnvelope.ContractDigest, digest)
}

func TestValidateEnvelopeRejectsIdentityAndDigestTampering(t *testing.T) {
	zero := int64(0)
	envelope, err := BuildEnvelope(types.TaskUsageProducerKindGoAdapter, DoubaoVideoContract(), types.TaskUsageSourceProviderResponse, &types.TaskUsage{
		CompletionTokens: &zero,
		TotalTokens:      &zero,
		Source:           types.TaskUsageSourceProviderResponse,
		Completeness:     types.TaskUsageCompletenessComplete,
	})
	require.NoError(t, err)

	tampered := types.CloneTaskUsageEnvelope(envelope)
	tampered.SourceID = "client-supplied-source"
	require.Error(t, ValidateEnvelope(DoubaoVideoContract(), tampered))

	tampered = types.CloneTaskUsageEnvelope(envelope)
	*tampered.Usage.TotalTokens = 1
	require.Error(t, ValidateEnvelope(DoubaoVideoContract(), tampered))
}

func TestResolveContractUsesFrozenIdentity(t *testing.T) {
	contract := MiniMaxH3Contract()
	digest, err := ContractDigest(contract)
	require.NoError(t, err)

	resolved, err := ResolveContract(contract.SourceID, contract.SchemaVersion, digest)
	require.NoError(t, err)
	require.Equal(t, contract, resolved)

	_, err = ResolveContract(contract.SourceID, contract.SchemaVersion, "tampered")
	require.ErrorContains(t, err, "digest")
	_, err = ResolveContract("unknown-source", 1, digest)
	require.ErrorContains(t, err, "unsupported")
}

func TestTaskPluginFixtureMatchesGoAdapterCanonicalFacts(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "task-plugin-minimax-h3-v1.json"))
	require.NoError(t, err)
	var fixture struct {
		ProducerKind  string           `json:"producer_kind"`
		SourceID      string           `json:"source_id"`
		SchemaVersion int              `json:"schema_version"`
		Stage         string           `json:"stage"`
		Usage         *types.TaskUsage `json:"usage"`
	}
	require.NoError(t, common.Unmarshal(data, &fixture))
	contract := MiniMaxH3Contract()
	require.Equal(t, contract.SourceID, fixture.SourceID)
	require.Equal(t, contract.SchemaVersion, fixture.SchemaVersion)

	pluginEnvelope, err := BuildEnvelope(fixture.ProducerKind, contract, fixture.Stage, fixture.Usage)
	require.NoError(t, err)
	goEnvelope, err := BuildEnvelope(types.TaskUsageProducerKindGoAdapter, contract, fixture.Stage, fixture.Usage)
	require.NoError(t, err)
	require.Equal(t, goEnvelope.EvidenceDigest, pluginEnvelope.EvidenceDigest)
	require.NoError(t, ValidateEnvelope(contract, pluginEnvelope))
}
