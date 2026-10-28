package controller

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/channelhealth"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const p43NodeEnv = "MAXAPI_CHANNEL_ROUTING_P43_NODE"
const p43CoordinatorEnv = "MAXAPI_CHANNEL_ROUTING_P43_COORDINATOR"
const p43ReplyPrefix = "P43_NODE_REPLY "

type p43NodeConfig struct {
	Database string
	Redis    string
}

type p43Command struct {
	Action    string
	Model     string
	ChannelID int
	AttemptID string
	Success   bool
	Observed  time.Time
}

type p43Reply struct {
	PID          int
	Error        string
	RouteStarted time.Time
	SelectedID   int
	SelectedBase int64
	Health       *dto.ChannelRoutingHealth
	Transition   *channelhealth.EvaluationResult
}

func p43Setting() operation_setting.MonitorSetting {
	return operation_setting.MonitorSetting{
		AutoPriorityDemotionEnabled: true, TimeoutAutoDisableEnabled: true,
		StreamingFirstResultTimeoutSeconds: 1, PriorityDeduction: 2,
		PenaltyCooldownSeconds: 600, RecoveryMode: operation_setting.RecoveryModeAutomatic,
		TimeoutAutoDisableCount: 2, TimeoutAutoDisableWindowSeconds: 3600,
		TimeoutAutoDisableDurationSeconds:      1,
		TimeoutAutoDisableRecoveryMode:         operation_setting.TimeoutAutoDisableRecoveryProbe,
		TimeoutAutoDisableSuccessfulProbeCount: 2,
	}
}

// This helper runs only as a child of the isolated acceptance test. Each child
// has its own real model caches and policy globals, while SQL and Redis are shared.
func TestP43RoutingHealthNodeProcess(t *testing.T) {
	encoded := os.Getenv(p43NodeEnv)
	if encoded == "" {
		t.Skip("helper requires the parent-owned isolated fixture")
	}
	var config p43NodeConfig
	require.NoError(t, common.Unmarshal([]byte(encoded), &config))
	db, err := gorm.Open(sqlite.Open(config.Database), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()
	model.DB, model.LOG_DB = db, db
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = true, false, false
	common.MemoryCacheEnabled, common.RedisEnabled = true, true
	common.RDB = redis.NewClient(&redis.Options{Addr: config.Redis, PoolSize: 1, MaxRetries: -1})
	defer common.RDB.Close()
	require.NoError(t, common.RDB.Ping(context.Background()).Err())
	setting := p43Setting()
	*operation_setting.GetMonitorSetting() = setting
	model.InitChannelCache()
	writeReply := func(reply p43Reply) {
		reply.PID = os.Getpid()
		data, err := common.Marshal(reply)
		require.NoError(t, err)
		_, err = fmt.Fprintln(os.Stdout, p43ReplyPrefix+string(data))
		require.NoError(t, err)
	}
	writeReply(p43Reply{})
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var command p43Command
		require.NoError(t, common.Unmarshal(scanner.Bytes(), &command))
		reply := p43Reply{}
		var actionErr error
		switch command.Action {
		case "sync":
			model.InitChannelCache()
		case "policy_off":
			*operation_setting.GetMonitorSetting() = operation_setting.MonitorSetting{}
		case "policy_on":
			*operation_setting.GetMonitorSetting() = setting
		case "timeout":
			result, err := channelhealth.EvaluateTimeoutGuard(context.Background(), channelhealth.AttemptEvidence{
				ChannelID: command.ChannelID, AttemptID: command.AttemptID,
				RequestMode:     channelhealth.RequestModeStreaming,
				TimeoutKind:     channelhealth.TimeoutKindStreamingFirstResult,
				TimeoutEligible: true, AutoBan: true, ObservedAt: command.Observed,
			}, &setting)
			reply.Transition, actionErr = &result, err
		case "probe":
			result, err := channelhealth.RecoverRuntimeState(context.Background(), command.ChannelID, command.AttemptID, command.Success, &setting)
			reply.Transition, actionErr = &result, err
		case "sample", "database_sample":
			common.MemoryCacheEnabled = command.Action != "database_sample"
			reply.RouteStarted = time.Now().UTC()
			selected, err := model.GetRandomSatisfiedChannel("default", command.Model, 0, "/v1/chat/completions")
			common.MemoryCacheEnabled = true
			actionErr = err
			if selected != nil {
				reply.SelectedID, reply.SelectedBase = selected.Id, selected.GetPriority()
			}
			channel, err := model.GetChannelById(command.ChannelID, false)
			if err != nil {
				actionErr = err
			} else {
				views := service.BuildChannelRoutingHealthViews(context.Background(), []*model.Channel{channel})
				reply.Health = &views[0].RoutingHealth
			}
		default:
			actionErr = fmt.Errorf("unsupported fixture command %q", command.Action)
		}
		if actionErr != nil {
			reply.Error = actionErr.Error()
		}
		writeReply(reply)
	}
	require.NoError(t, scanner.Err())
}

type p43Node struct {
	input   io.WriteCloser
	replies <-chan string
}

func startP43Node(t *testing.T, config p43NodeConfig) *p43Node {
	t.Helper()
	data, err := common.Marshal(config)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestP43RoutingHealthNodeProcess$", "-test.count=1", "-test.timeout=85s")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, p43NodeEnv+"=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, p43NodeEnv+"="+string(data))
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	require.NoError(t, err)
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	replies := make(chan string, 1)
	require.NoError(t, command.Start())
	go func() {
		defer close(replies)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), p43ReplyPrefix) {
				replies <- strings.TrimPrefix(scanner.Text(), p43ReplyPrefix)
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		defer cancel()
		_ = input.Close()
		select {
		case err := <-done:
			require.NoError(t, err, "isolated node exited unsuccessfully")
		case <-time.After(5 * time.Second):
			cancel()
			t.Error("isolated node did not exit after stdin closed")
		}
	})
	node := &p43Node{input: input, replies: replies}
	ready := node.receive(t)
	require.Positive(t, ready.PID)
	return node
}

func (node *p43Node) receive(t *testing.T) p43Reply {
	t.Helper()
	select {
	case data, ok := <-node.replies:
		require.True(t, ok, "isolated node closed its reply stream")
		var reply p43Reply
		require.NoError(t, common.Unmarshal([]byte(data), &reply))
		require.Empty(t, reply.Error, "fixture operation failed; never retry a runtime mutation to manufacture a pass")
		return reply
	case <-time.After(15 * time.Second):
		t.Fatal("isolated node reply timed out")
		return p43Reply{}
	}
}

func (node *p43Node) call(t *testing.T, stage string, command p43Command) p43Reply {
	t.Helper()
	data, err := common.Marshal(command)
	require.NoError(t, err)
	_, err = fmt.Fprintln(node.input, string(data))
	require.NoError(t, err)
	reply := node.receive(t)
	data, err = common.Marshal(reply)
	require.NoError(t, err)
	t.Logf("%s %s", stage, data)
	return reply
}

// All expected priorities and selected IDs below are literals derived from the
// fixture inputs, never from EffectivePriority or the projection under test.
func TestP43TwoNodeRoutingHealthFaultsAndRecovery(t *testing.T) {
	// Keep the fixture Redis server away from background work left by earlier
	// controller tests. Each runtime transaction retains the production deadline.
	if os.Getenv(p43CoordinatorEnv) != "isolated" {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestP43TwoNodeRoutingHealthFaultsAndRecovery$", "-test.count=1", "-test.timeout=95s", "-test.v")
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, p43CoordinatorEnv+"=") {
				command.Env = append(command.Env, entry)
			}
		}
		command.Env = append(command.Env, p43CoordinatorEnv+"=isolated")
		output, err := command.CombinedOutput()
		t.Logf("isolated coordinator output:\n%s", output)
		require.NoError(t, err)
		return
	}
	database := filepath.Join(t.TempDir(), "channel-routing.sqlite")
	db, err := gorm.Open(sqlite.Open(database), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	for _, item := range []struct {
		id       int
		priority int64
		model    string
	}{
		{4301, 10, "p43-model"}, {4302, 9, "p43-model"},
		{4303, 20, "p43-expiry"}, {4304, 19, "p43-expiry"},
		{4305, 7, "p43-recovery"}, {4306, 6, "p43-recovery"},
	} {
		channel := model.Channel{Id: item.id, Name: fmt.Sprintf("synthetic-p43-%d", item.id), Type: 1,
			Key: "synthetic-unused-key", Status: common.ChannelStatusEnabled, Group: "default", Models: item.model,
			Priority: common.GetPointer(item.priority), Weight: common.GetPointer(uint(100))}
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: item.model, ChannelId: item.id,
			Enabled: true, Priority: common.GetPointer(item.priority), Weight: 100}).Error)
	}
	server := miniredis.RunT(t)
	config := p43NodeConfig{Database: database, Redis: server.Addr()}
	nodeA, nodeB := startP43Node(t, config), startP43Node(t, config)
	sample := func(node *p43Node, stage, modelName string, id int) p43Reply {
		return node.call(t, stage, p43Command{Action: "sample", Model: modelName, ChannelID: id})
	}
	assertHealth := func(reply p43Reply, selected int, base int64, effective *int64, status string) {
		t.Helper()
		require.Equal(t, selected, reply.SelectedID)
		require.NotNil(t, reply.Health)
		require.Equal(t, base, reply.Health.BasePriority)
		require.Equal(t, effective, reply.Health.EffectivePriority)
		require.Equal(t, status, reply.Health.Status)
	}

	normal := sample(nodeA, "A-empty-runtime", "p43-model", 4301)
	assertHealth(normal, 4301, 10, common.GetPointer(int64(10)), "normal")
	server.HSet(channelhealth.RuntimeStateKey(4301), "penalty", "2", "runtime_disabled", "false",
		"penalty_until", time.Now().Add(10*time.Minute).UTC().Format(time.RFC3339Nano))
	// Strict management sees Redis immediately; routing still has its known-empty
	// local state until its one-second cache expires. Check the timing precondition.
	lag := sample(nodeA, "A-fresh-view-stale-runtime-cache", "p43-model", 4301)
	require.Less(t, lag.RouteStarted.Sub(normal.RouteStarted), time.Second, "fixture must remain inside the fresh-cache window")
	assertHealth(lag, 4301, 10, common.GetPointer(int64(8)), "penalized")
	time.Sleep(1100 * time.Millisecond)
	prime := sample(nodeA, "A-shared-penalty", "p43-model", 4301)
	assertHealth(prime, 4302, 10, common.GetPointer(int64(8)), "penalized")
	other := sample(nodeB, "B-shared-penalty", "p43-model", 4301)
	assertHealth(other, 4302, 10, common.GetPointer(int64(8)), "penalized")
	require.NotEqual(t, prime.PID, other.PID, "nodes must be separate OS processes")

	server.SetError("ERR synthetic-p43-outage")
	freshFailure := sample(nodeA, "A-outage-with-fresh-cache", "p43-model", 4301)
	require.Less(t, freshFailure.RouteStarted.Sub(prime.RouteStarted), time.Second, "fresh-cache fault precondition")
	assertHealth(freshFailure, 4302, 10, nil, "unknown")
	require.Equal(t, "redis_read_failed", *freshFailure.Health.UnavailableReason)
	time.Sleep(1100 * time.Millisecond)
	assertHealth(sample(nodeA, "A-outage-after-cache-expiry", "p43-model", 4301), 4301, 10, nil, "unknown")
	server.SetError("")
	time.Sleep(300 * time.Millisecond)
	assertHealth(sample(nodeA, "A-store-restored", "p43-model", 4301), 4302, 10, common.GetPointer(int64(8)), "penalized")

	// Save shared configuration and ability priority together; neither child is
	// told to sync until explicitly commanded. P4.2 covers the real PUT seam.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Channel{}).Where("id = ?", 4301).Update("priority", int64(12)).Error; err != nil {
			return err
		}
		return tx.Model(&model.Ability{}).Where("channel_id = ?", 4301).Update("priority", int64(12)).Error
	}))
	assertHealth(sample(nodeA, "A-new-SQL-old-channel-cache", "p43-model", 4301), 4302, 12, common.GetPointer(int64(10)), "penalized")
	nodeB.call(t, "B-sync-config", p43Command{Action: "sync"})
	synced := sample(nodeB, "B-new-channel-cache", "p43-model", 4301)
	assertHealth(synced, 4301, 12, common.GetPointer(int64(10)), "penalized")
	require.EqualValues(t, 12, synced.SelectedBase)
	assertHealth(nodeA.call(t, "A-database-candidates", p43Command{Action: "database_sample", Model: "p43-model", ChannelID: 4301}),
		4301, 12, common.GetPointer(int64(10)), "penalized")
	nodeA.call(t, "A-sync-config", p43Command{Action: "sync"})
	assertHealth(sample(nodeA, "A-config-converged", "p43-model", 4301), 4301, 12, common.GetPointer(int64(10)), "penalized")
	nodeA.call(t, "A-policy-disabled", p43Command{Action: "policy_off"})
	assertHealth(sample(nodeA, "A-local-policy-off", "p43-model", 4301), 4301, 12, common.GetPointer(int64(12)), "policy_off")
	assertHealth(sample(nodeB, "B-local-policy-still-on", "p43-model", 4301), 4301, 12, common.GetPointer(int64(10)), "penalized")
	nodeA.call(t, "A-policy-restored", p43Command{Action: "policy_on"})

	// This pair deliberately changes the winning channel across the deadline.
	// miniredis.FastForward cannot advance the Go wall clock used by routing.
	until := time.Now().Add(4 * time.Second).UTC()
	server.HSet(channelhealth.RuntimeStateKey(4303), "penalty", "2", "runtime_disabled", "false",
		"penalty_until", until.Format(time.RFC3339Nano))
	beforeExpiry := sample(nodeA, "A-before-penalty-deadline", "p43-expiry", 4303)
	require.True(t, beforeExpiry.Health.EvaluatedAt.Before(until), "pre-expiry observation must precede the fixture deadline")
	assertHealth(beforeExpiry, 4304, 20, common.GetPointer(int64(18)), "penalized")
	beforeExpiryDump := server.Dump()
	time.Sleep(max(time.Until(until)+50*time.Millisecond, 0))
	afterExpiry := sample(nodeA, "A-after-penalty-deadline", "p43-expiry", 4303)
	assertHealth(afterExpiry, 4303, 20, common.GetPointer(int64(20)), "normal")
	require.EqualValues(t, 2, *afterExpiry.Health.StoredPenalty)
	require.Zero(t, *afterExpiry.Health.ActivePenalty)
	require.Equal(t, beforeExpiryDump, server.Dump(), "expiry is read-time interpretation; GET must not rewrite the stored penalty")

	// Real timeout/probe transactions alternate between the two nodes. Backdate
	// the timeout evidence so disabled_until is already due without arbitrary sleep.
	observed := time.Now().Add(-2 * time.Minute).UTC()
	firstTimeout := nodeA.call(t, "A-first-counted-timeout", p43Command{Action: "timeout", ChannelID: 4305, AttemptID: "p43-timeout-1", Observed: observed})
	require.EqualValues(t, 1, firstTimeout.Transition.State.TimeoutCount[channelhealth.RequestModeStreaming])
	disabled := nodeB.call(t, "B-second-counted-timeout", p43Command{Action: "timeout", ChannelID: 4305, AttemptID: "p43-timeout-2", Observed: observed})
	require.Equal(t, "timeout_auto_disabled", disabled.Transition.Transition)
	require.True(t, disabled.Transition.State.DisabledUntil.Before(time.Now()))
	assertHealth(sample(nodeA, "A-disabled-deadline-alone-does-not-recover", "p43-recovery", 4305), 4306, 7, nil, "runtime_disabled")
	probe := func(node *p43Node, id string, success bool) *channelhealth.EvaluationResult {
		return node.call(t, id, p43Command{Action: "probe", ChannelID: 4305, AttemptID: id, Success: success}).Transition
	}
	firstProbe := probe(nodeA, "p43-probe-1", true)
	require.True(t, firstProbe.State.RuntimeDisabled)
	require.EqualValues(t, 1, firstProbe.State.RecoveryProbeSuccessCount)
	duplicate := probe(nodeB, "p43-probe-1", true)
	require.True(t, duplicate.Deduplicated)
	require.EqualValues(t, 1, duplicate.State.RecoveryProbeSuccessCount)
	failedProbe := probe(nodeB, "p43-probe-fail", false)
	require.True(t, failedProbe.State.RuntimeDisabled)
	require.Zero(t, failedProbe.State.RecoveryProbeSuccessCount)
	require.True(t, probe(nodeA, "p43-probe-2", true).State.RuntimeDisabled)
	recovered := probe(nodeB, "p43-probe-3", true)
	require.Equal(t, "runtime_recovered", recovered.Transition)
	require.False(t, recovered.State.RuntimeDisabled)
	require.Zero(t, recovered.State.Penalty)
	beforeLate := server.Dump()
	late := nodeA.call(t, "A-old-observation-after-recovery", p43Command{Action: "timeout", ChannelID: 4305,
		AttemptID: "p43-late-timeout", Observed: recovered.State.RecoveryWatermark.Add(-time.Second)})
	require.False(t, late.Transition.Applied)
	require.Equal(t, recovered.State.Generation, late.Transition.State.Generation)
	require.Equal(t, beforeLate, server.Dump(), "late observation must not modify shared runtime state")
	time.Sleep(1100 * time.Millisecond)
	assertHealth(sample(nodeA, "A-recovery-after-local-cache-expiry", "p43-recovery", 4305), 4305, 7, common.GetPointer(int64(7)), "normal")
}
