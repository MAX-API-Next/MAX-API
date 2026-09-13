package taskusage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/MAX-API-Next/MAX-API/types"
)

const (
	SourceIDMiniMax     = "minimax"
	SourceIDDoubaoVideo = "doubao_video"
)

// legacyEvidenceFieldOrder is the field order used by envelopes emitted before
// evidence digests were scoped to the frozen contract. Keep it unchanged so
// historical in-flight tasks remain verifiable after future schema additions.
var legacyEvidenceFieldOrder = [...]string{
	types.TaskUsageFieldOutputDurationMs,
	types.TaskUsageFieldInputVideoDurationMs,
	types.TaskUsageFieldInputAudioDurationMs,
	types.TaskUsageFieldInputImageCount,
	types.TaskUsageFieldInputVideoCount,
	types.TaskUsageFieldInputAudioCount,
	types.TaskUsageFieldCompletionTokens,
	types.TaskUsageFieldTotalTokens,
}

var canonicalFieldUnits = map[string]string{
	types.TaskUsageFieldOutputDurationMs:     types.TaskUsageUnitMillisecond,
	types.TaskUsageFieldInputVideoDurationMs: types.TaskUsageUnitMillisecond,
	types.TaskUsageFieldInputAudioDurationMs: types.TaskUsageUnitMillisecond,
	types.TaskUsageFieldInputImageCount:      types.TaskUsageUnitCount,
	types.TaskUsageFieldInputVideoCount:      types.TaskUsageUnitCount,
	types.TaskUsageFieldInputAudioCount:      types.TaskUsageUnitCount,
	types.TaskUsageFieldCompletionTokens:     types.TaskUsageUnitToken,
	types.TaskUsageFieldTotalTokens:          types.TaskUsageUnitToken,
}

// MiniMaxH3Contract returns a copy of the host-owned H3 usage schema. Pricing
// and request-conditional requirements remain in the frozen billing plan; this
// contract only bounds facts that the adaptor may produce.
func MiniMaxH3Contract() types.TaskUsageContract {
	return types.TaskUsageContract{
		SourceID:      SourceIDMiniMax,
		SchemaVersion: 1,
		Fields: []types.TaskUsageFieldContract{
			usageField(types.TaskUsageFieldOutputDurationMs, types.TaskUsageUnitMillisecond, true, 4_000, int64Pointer(15_000)),
			usageField(types.TaskUsageFieldInputVideoDurationMs, types.TaskUsageUnitMillisecond, false, 0, int64Pointer(15_000)),
			usageField(types.TaskUsageFieldInputAudioDurationMs, types.TaskUsageUnitMillisecond, false, 0, int64Pointer(15_000)),
			usageField(types.TaskUsageFieldInputImageCount, types.TaskUsageUnitCount, true, 0, int64Pointer(9)),
		},
	}
}

// DoubaoVideoContract describes only the token facts already exposed by the
// current fixed Go adaptor. It does not select a price or authorize settlement.
func DoubaoVideoContract() types.TaskUsageContract {
	return types.TaskUsageContract{
		SourceID:      SourceIDDoubaoVideo,
		SchemaVersion: 1,
		Fields: []types.TaskUsageFieldContract{
			usageField(types.TaskUsageFieldCompletionTokens, types.TaskUsageUnitToken, true, 0, nil),
			usageField(types.TaskUsageFieldTotalTokens, types.TaskUsageUnitToken, true, 0, nil),
		},
	}
}

// ResolveContract returns the host-owned contract frozen by a task plan.
// Prior versions must remain registered so in-flight tasks are never
// reinterpreted under a newer usage schema.
func ResolveContract(sourceID string, schemaVersion int, contractDigest string) (types.TaskUsageContract, error) {
	var contract types.TaskUsageContract
	switch {
	case sourceID == SourceIDMiniMax && schemaVersion == 1:
		contract = MiniMaxH3Contract()
	case sourceID == SourceIDDoubaoVideo && schemaVersion == 1:
		contract = DoubaoVideoContract()
	default:
		return types.TaskUsageContract{}, fmt.Errorf("unsupported task usage contract %q version %d", sourceID, schemaVersion)
	}
	digest, err := ContractDigest(contract)
	if err != nil {
		return types.TaskUsageContract{}, err
	}
	if contractDigest == "" || contractDigest != digest {
		return types.TaskUsageContract{}, fmt.Errorf("task usage contract digest mismatch")
	}
	return contract, nil
}

func usageField(key, unit string, required bool, minValue int64, maxValue *int64) types.TaskUsageFieldContract {
	return types.TaskUsageFieldContract{
		Key: key, Unit: unit, RequiredForTerminal: required,
		MinValue: int64Pointer(minValue), MaxValue: maxValue,
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}

// ContractDigest returns a deterministic digest independent of field order.
func ContractDigest(contract types.TaskUsageContract) (string, error) {
	if err := validateContract(contract); err != nil {
		return "", err
	}
	fields := append([]types.TaskUsageFieldContract(nil), contract.Fields...)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Key < fields[j].Key })
	var canonical strings.Builder
	fmt.Fprintf(&canonical, "source_id=%s\nschema_version=%d\n", contract.SourceID, contract.SchemaVersion)
	for _, field := range fields {
		fmt.Fprintf(&canonical, "field=%s\nunit=%s\nrequired=%t\nmin=%s\nmax=%s\n",
			field.Key, field.Unit, field.RequiredForTerminal, pointerString(field.MinValue), pointerString(field.MaxValue))
	}
	return sha256Hex(canonical.String()), nil
}

// BuildEnvelope validates canonical facts and binds them to a host-owned
// contract. The raw payload is intentionally absent, so digests cannot leak
// provider URLs, prompts, credentials, or response metadata.
func BuildEnvelope(producerKind string, contract types.TaskUsageContract, stage string, usage *types.TaskUsage) (*types.TaskUsageEnvelope, error) {
	if producerKind != types.TaskUsageProducerKindGoAdapter && producerKind != types.TaskUsageProducerKindTaskPlugin {
		return nil, fmt.Errorf("unsupported task usage producer kind %q", producerKind)
	}
	if !validStage(stage) {
		return nil, fmt.Errorf("unsupported task usage stage %q", stage)
	}
	contractDigest, err := ContractDigest(contract)
	if err != nil {
		return nil, err
	}
	cloned := types.CloneTaskUsage(usage)
	completeness := types.TaskUsageCompletenessMissing
	if cloned != nil {
		if cloned.Source != stage {
			return nil, fmt.Errorf("task usage source %q does not match stage %q", cloned.Source, stage)
		}
		if !validCompleteness(cloned.Completeness) {
			return nil, fmt.Errorf("unsupported task usage completeness %q", cloned.Completeness)
		}
		completeness = cloned.Completeness
	}
	presence, err := validateUsage(contract, cloned, completeness)
	if err != nil {
		return nil, err
	}
	envelope := &types.TaskUsageEnvelope{
		ProducerKind:   producerKind,
		SourceID:       contract.SourceID,
		SchemaVersion:  contract.SchemaVersion,
		ContractDigest: contractDigest,
		Stage:          stage,
		Presence:       presence,
		Completeness:   completeness,
		Usage:          cloned,
	}
	envelope.EvidenceDigest = evidenceDigest(contract, stage, presence, completeness, cloned)
	return envelope, nil
}

// ValidateEnvelope detects identity, presence, fact, and digest drift before a
// caller can use the envelope as settlement evidence.
func ValidateEnvelope(contract types.TaskUsageContract, envelope *types.TaskUsageEnvelope, expectedProducerKind string) error {
	if envelope == nil {
		return fmt.Errorf("task usage envelope is required")
	}
	if expectedProducerKind != types.TaskUsageProducerKindGoAdapter && expectedProducerKind != types.TaskUsageProducerKindTaskPlugin {
		return fmt.Errorf("unsupported expected task usage producer kind %q", expectedProducerKind)
	}
	if envelope.ProducerKind != expectedProducerKind {
		return fmt.Errorf("task usage producer kind mismatch")
	}
	expected, err := BuildEnvelope(expectedProducerKind, contract, envelope.Stage, envelope.Usage)
	if err != nil {
		return err
	}
	if envelope.SourceID != expected.SourceID ||
		envelope.SchemaVersion != expected.SchemaVersion ||
		envelope.ContractDigest != expected.ContractDigest ||
		envelope.Presence != expected.Presence ||
		envelope.Completeness != expected.Completeness {
		return fmt.Errorf("task usage envelope identity or digest mismatch")
	}
	if envelope.EvidenceDigest != expected.EvidenceDigest &&
		envelope.EvidenceDigest != legacyEvidenceDigest(envelope.Stage, envelope.Presence, envelope.Completeness, envelope.Usage) {
		return fmt.Errorf("task usage envelope identity or digest mismatch")
	}
	return nil
}

func validateContract(contract types.TaskUsageContract) error {
	if strings.TrimSpace(contract.SourceID) == "" || contract.SourceID != strings.TrimSpace(contract.SourceID) {
		return fmt.Errorf("task usage contract source_id is invalid")
	}
	if contract.SchemaVersion <= 0 {
		return fmt.Errorf("task usage contract schema_version is invalid")
	}
	if len(contract.Fields) == 0 {
		return fmt.Errorf("task usage contract fields are required")
	}
	seen := make(map[string]struct{}, len(contract.Fields))
	for _, field := range contract.Fields {
		expectedUnit, ok := canonicalFieldUnits[field.Key]
		if !ok || field.Unit != expectedUnit {
			return fmt.Errorf("task usage contract field %q has invalid unit", field.Key)
		}
		if _, duplicate := seen[field.Key]; duplicate {
			return fmt.Errorf("task usage contract field %q is duplicated", field.Key)
		}
		seen[field.Key] = struct{}{}
		if field.MinValue != nil && field.MaxValue != nil && *field.MinValue > *field.MaxValue {
			return fmt.Errorf("task usage contract field %q has invalid range", field.Key)
		}
	}
	return nil
}

func validateUsage(contract types.TaskUsageContract, usage *types.TaskUsage, completeness string) (string, error) {
	values := usageValues(usage)
	allowed := make(map[string]types.TaskUsageFieldContract, len(contract.Fields))
	for _, field := range contract.Fields {
		allowed[field.Key] = field
	}
	for key, value := range values {
		if value == nil {
			continue
		}
		field, ok := allowed[key]
		if !ok {
			return "", fmt.Errorf("task usage field %q is not declared by the contract", key)
		}
		if completeness == types.TaskUsageCompletenessComplete || completeness == types.TaskUsageCompletenessPartial {
			if (field.MinValue != nil && *value < *field.MinValue) ||
				(field.MaxValue != nil && *value > *field.MaxValue) {
				return "", fmt.Errorf("task usage field %q is outside the contract range", key)
			}
		}
	}
	if completeness == types.TaskUsageCompletenessComplete || completeness == types.TaskUsageCompletenessPartial {
		completionTokens := values[types.TaskUsageFieldCompletionTokens]
		totalTokens := values[types.TaskUsageFieldTotalTokens]
		if completionTokens != nil && totalTokens != nil && *totalTokens < *completionTokens {
			return "", fmt.Errorf("total tokens cannot be less than completion tokens")
		}
	}

	presentCount := 0
	allZero := true
	for _, value := range values {
		if value == nil {
			continue
		}
		presentCount++
		if *value != 0 {
			allZero = false
		}
	}
	switch completeness {
	case types.TaskUsageCompletenessMissing:
		if presentCount != 0 {
			return "", fmt.Errorf("missing task usage cannot contain canonical facts")
		}
		return types.TaskUsagePresenceNotPresent, nil
	case types.TaskUsageCompletenessPartial:
		if presentCount == 0 {
			return "", fmt.Errorf("partial task usage requires at least one canonical fact")
		}
		return types.TaskUsagePresencePartial, nil
	case types.TaskUsageCompletenessInvalid:
		return types.TaskUsagePresenceInvalid, nil
	case types.TaskUsageCompletenessAmbiguous:
		return types.TaskUsagePresenceAmbiguous, nil
	case types.TaskUsageCompletenessComplete:
		for _, field := range contract.Fields {
			if field.RequiredForTerminal && values[field.Key] == nil {
				return "", fmt.Errorf("complete task usage is missing required field %q", field.Key)
			}
		}
		if presentCount == 0 {
			return "", fmt.Errorf("complete task usage requires canonical facts")
		}
		if allZero {
			return types.TaskUsagePresencePresentZero, nil
		}
		return types.TaskUsagePresencePresentValid, nil
	default:
		return "", fmt.Errorf("unsupported task usage completeness %q", completeness)
	}
}

func usageValues(usage *types.TaskUsage) map[string]*int64 {
	values := map[string]*int64{}
	if usage == nil {
		return values
	}
	values[types.TaskUsageFieldOutputDurationMs] = usage.OutputDurationMs
	values[types.TaskUsageFieldInputVideoDurationMs] = usage.InputVideoDurationMs
	values[types.TaskUsageFieldInputAudioDurationMs] = usage.InputAudioDurationMs
	values[types.TaskUsageFieldInputImageCount] = usage.InputImageCount
	values[types.TaskUsageFieldInputVideoCount] = usage.InputVideoCount
	values[types.TaskUsageFieldInputAudioCount] = usage.InputAudioCount
	values[types.TaskUsageFieldCompletionTokens] = usage.CompletionTokens
	values[types.TaskUsageFieldTotalTokens] = usage.TotalTokens
	return values
}

func evidenceDigest(contract types.TaskUsageContract, stage, presence, completeness string, usage *types.TaskUsage) string {
	values := usageValues(usage)
	fields := append([]types.TaskUsageFieldContract(nil), contract.Fields...)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Key < fields[j].Key })
	var canonical strings.Builder
	fmt.Fprintf(&canonical, "stage=%s\npresence=%s\ncompleteness=%s\n", stage, presence, completeness)
	for _, field := range fields {
		fmt.Fprintf(&canonical, "%s=%s\n", field.Key, pointerString(values[field.Key]))
	}
	return sha256Hex(canonical.String())
}

// legacyEvidenceDigest accepts envelopes emitted before the digest was scoped
// to the frozen contract. This keeps in-flight task evidence verifiable while
// new envelopes remain stable when later contracts add fields.
func legacyEvidenceDigest(stage, presence, completeness string, usage *types.TaskUsage) string {
	values := usageValues(usage)
	var canonical strings.Builder
	fmt.Fprintf(&canonical, "stage=%s\npresence=%s\ncompleteness=%s\n", stage, presence, completeness)
	for _, key := range legacyEvidenceFieldOrder {
		fmt.Fprintf(&canonical, "%s=%s\n", key, pointerString(values[key]))
	}
	return sha256Hex(canonical.String())
}

func pointerString(value *int64) string {
	if value == nil {
		return "absent"
	}
	return fmt.Sprintf("%d", *value)
}

func validStage(stage string) bool {
	switch stage {
	case types.TaskUsageSourceRequestEstimate,
		types.TaskUsageSourceProviderResponse,
		types.TaskUsageSourceCallback,
		types.TaskUsageSourceManualReconcile:
		return true
	default:
		return false
	}
}

func validCompleteness(completeness string) bool {
	switch completeness {
	case types.TaskUsageCompletenessComplete,
		types.TaskUsageCompletenessPartial,
		types.TaskUsageCompletenessMissing,
		types.TaskUsageCompletenessInvalid,
		types.TaskUsageCompletenessAmbiguous:
		return true
	default:
		return false
	}
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
