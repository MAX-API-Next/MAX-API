package channelhealth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	maxcommon "github.com/MAX-API-Next/MAX-API/common"
	"github.com/go-redis/redis/v8"
)

var ErrInvalidRuntimeState = errors.New("invalid channel timeout runtime state")

// RuntimeStateReadResult preserves per-channel validation failures without
// converting corrupt state into an apparently healthy empty observation.
type RuntimeStateReadResult struct {
	State RuntimeState
	Err   error
}

// LoadRuntimeStatesStrict only reads the requested hashes. Routing continues to
// use the existing tolerant loaders; this stricter adapter is for management views.
func LoadRuntimeStatesStrict(ctx context.Context, channelIDs []int) (map[int]RuntimeStateReadResult, error) {
	results := make(map[int]RuntimeStateReadResult, len(channelIDs))
	if len(channelIDs) == 0 {
		return results, nil
	}
	client := maxcommon.RDB
	if !maxcommon.RedisEnabled || client == nil {
		return results, ErrRuntimeUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return results, err
	}
	pipe := client.Pipeline()
	defer pipe.Close()
	commands := make(map[int]*redis.StringStringMapCmd, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			continue
		}
		if _, ok := commands[channelID]; ok {
			continue
		}
		commands[channelID] = pipe.HGetAll(ctx, runtimeStateKey(channelID))
	}
	if len(commands) == 0 {
		return results, nil
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return results, err
	}
	for channelID, command := range commands {
		fields, err := command.Result()
		if err != nil {
			return nil, err
		}
		if err := validateRuntimeStateFields(channelID, fields); err != nil {
			results[channelID] = RuntimeStateReadResult{Err: err}
			continue
		}
		results[channelID] = RuntimeStateReadResult{State: decodeRuntimeState(channelID, fields)}
	}
	return results, nil
}

func validateRuntimeStateFields(channelID int, fields map[string]string) error {
	if len(fields) == 0 {
		return nil // A successful read of a missing hash is a known empty state.
	}
	invalid := func(field string) error {
		return fmt.Errorf("%w: %s", ErrInvalidRuntimeState, field)
	}
	// Both fields have always been written by the runtime encoder. Other fields
	// remain optional for older states, including states predating recovery watermarks.
	if _, ok := fields["penalty"]; !ok {
		return invalid("penalty")
	}
	if value := fields["runtime_disabled"]; value != "true" && value != "false" {
		return invalid("runtime_disabled")
	}
	if value, ok := fields["channel_id"]; ok {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id != int64(channelID) {
			return invalid("channel_id")
		}
	}
	intFields := []string{"penalty", "generation", "recovery_probe_success_count"}
	timeFields := []string{"penalty_until", "disabled_until", "last_observed_at", "recovery_watermark"}
	for _, mode := range []string{RequestModeStreaming, RequestModeNonStreaming, "combined"} {
		intFields = append(intFields, "timeout_count_"+mode, "sample_count_"+mode)
		timeFields = append(timeFields, "window_start_"+mode)
	}
	for _, field := range intFields {
		if value, ok := fields[field]; ok {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil || parsed < 0 {
				return invalid(field)
			}
		}
	}
	for _, field := range timeFields {
		if value := fields[field]; value != "" {
			if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
				return invalid(field)
			}
		}
	}
	if mode := fields["last_timeout_mode"]; mode != "" &&
		mode != RequestModeStreaming && mode != RequestModeNonStreaming {
		return invalid("last_timeout_mode")
	}
	return nil
}
