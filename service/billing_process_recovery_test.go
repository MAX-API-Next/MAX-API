package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const billingProcessConfigEnv = "MAXAPI_BILLING_PROCESS_CONFIG"

type billingProcessFunding struct {
	name, preference, source string
	wallet, subscriptionUsed int64
	allowOverflow            bool
}

func billingProcessFundingCases() []billingProcessFunding {
	return []billingProcessFunding{
		{"wallet", "wallet_only", model.BillingSettlementSourceWallet, 1000, 0, false},
		{"subscription", "subscription_only", model.BillingSettlementSourceSubscription, 1000, 0, false},
		{"wallet_fallback", "subscription_first", model.BillingSettlementSourceWallet, 1000, 1000, true},
		{"subscription_fallback", "wallet_first", model.BillingSettlementSourceSubscription, 50, 0, false},
	}
}

type billingProcessConfig struct {
	Database       string
	SeparateLogDB  bool
	LogUnavailable bool
	Ready          string
	Release        string
	Role           string
	Phase          string
	Preference     string
	Source         string
	Quota          int
	Refund         bool
	Playground     bool
	AdmissionOnly  bool
}

type billingProcessBarrier struct {
	PID               int
	Phase             string
	Table             string
	Database          string
	InsideTransaction bool
}

// Each child is this package's actual test executable, with fresh globals and
// connections. Only the child replaces model.DB. No production failpoints,
// graceful shutdown, transaction errors, or in-memory DBs simulate a crash.
func TestBillingProcessHelper(t *testing.T) {
	configPath := os.Getenv(billingProcessConfigEnv)
	if configPath == "" {
		t.Skip("subprocess helper; invoked by the process recovery tests")
	}
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	var cfg billingProcessConfig
	require.NoError(t, common.Unmarshal(data, &cfg))
	require.Equal(t, filepath.Join(filepath.Dir(configPath), "ledger.sqlite"), cfg.Database)
	var db, logDB *gorm.DB
	if cfg.SeparateLogDB {
		// Use the real startup configuration instead of injecting two handles.
		t.Setenv("SQL_DSN", "local")
		t.Setenv("LOG_SQL_DSN", "")
		t.Setenv("LOG_SQLITE_PATH", filepath.Join(filepath.Dir(cfg.Database), "logs.sqlite"))
		common.SQLitePath = cfg.Database
		common.IsMasterNode = false // The isolated fixture already owns its schema.
		require.NoError(t, model.InitDB())
		require.NoError(t, model.InitLogDB())
		db, logDB = model.DB, model.LOG_DB
		require.NotSame(t, db, logDB)
		configureBillingProcessDB(t, db)
		configureBillingProcessDB(t, logDB)
	} else {
		db = openBillingProcessDB(t, cfg.Database)
		logDB = db
	}
	model.DB, model.LOG_DB = db, logDB
	if cfg.LogUnavailable {
		require.True(t, cfg.SeparateLogDB, "only the independent log connection may be closed")
		logSQL, err := logDB.DB()
		require.NoError(t, err)
		require.NoError(t, logSQL.Close())
	}
	common.UsingSQLite, common.RedisEnabled, common.BatchUpdateEnabled = true, false, false
	common.LogConsumeEnabled, common.DataExportEnabled = true, false
	gin.SetMode(gin.TestMode)
	var user model.User
	require.NoError(t, db.First(&user, 951).Error)
	require.Equal(t, "synthetic-process-recovery", user.Username, "only the isolated fixture may be mutated")

	if cfg.Role == "work" {
		c, info := billingProcessContext(cfg)
		if cfg.AdmissionOnly {
			installBillingProcessBarrier(t, db, logDB, cfg)
		}
		session, apiErr := NewBillingSession(c, info, 100)
		require.Nil(t, apiErr)
		require.Equal(t, cfg.Source, info.BillingSource)
		require.Equal(t, 100, session.GetPreConsumedQuota())
		if cfg.AdmissionOnly {
			if cfg.Phase == "complete" {
				pauseBillingProcess(t, cfg, billingProcessBarrier{Phase: cfg.Phase})
			}
			return
		}
		installBillingProcessBarrier(t, db, logDB, cfg)
		if cfg.Refund {
			input, err := session.PrepareRefundSettlement()
			require.NoError(t, err)
			require.NotNil(t, input)
			_, _, err = model.ApplyBillingSettlementOnce(*input)
			require.NoError(t, err)
		} else {
			prompt, completion := billingProcessUsage(cfg.Quota)
			effect := newConsumeBillingSettlementEffect(info, model.RecordConsumeLogParams{
				Content: "synthetic process recovery", ChannelId: 953,
				ModelName: "process-recovery-model", TokenId: info.TokenId, TokenName: "synthetic-process-token",
				Group: "default", Quota: cfg.Quota, PromptTokens: prompt, CompletionTokens: completion,
			}, info.RequestId, "", true)
			require.NoError(t, session.SettleWithEffect(cfg.Quota, effect))
		}
		if cfg.Phase == "complete" {
			pauseBillingProcess(t, cfg, billingProcessBarrier{Phase: cfg.Phase})
		}
		return
	}
	if cfg.Role == "reset" {
		_, err := model.AdminResetUserSubscriptionsByPlan(951, 954, false)
		require.NoError(t, err)
		return
	}

	if cfg.Release != "" {
		writeBillingProcessJSON(t, cfg.Ready, billingProcessBarrier{PID: os.Getpid(), Phase: "ready"})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := os.Stat(cfg.Release); err == nil {
				break
			} else {
				require.True(t, errors.Is(err, os.ErrNotExist))
			}
			select {
			case <-ctx.Done():
				t.Fatal("parent did not release the recovery workers")
			case <-ticker.C:
			}
		}
	}
	installBillingProcessBarrier(t, db, logDB, cfg)
	if cfg.Role == "refund_competitor" {
		c, info := billingProcessContext(cfg)
		// The original session's refund intent competes with the already durable
		// final usage intent. Do not create another request or reserve again.
		var funding FundingSource = &WalletFunding{userId: 951, consumed: 100}
		if cfg.Source == model.BillingSettlementSourceSubscription {
			funding = &SubscriptionFunding{requestId: info.RequestId, userId: 951, subscriptionId: 955, preConsumed: 100}
		}
		session := &BillingSession{relayInfo: info, funding: funding, preConsumedQuota: 100, tokenConsumed: 100}
		if cfg.Playground {
			session.tokenConsumed = 0
		}
		session.Refund(c)
		// The current durable Refund completes synchronously, including conflicts.
		session.mu.Lock()
		refunded := session.refunded
		session.mu.Unlock()
		require.False(t, refunded, "a conflicting refund cannot replace the durable final usage")
		return
	}
	// This is the production restart runner, including pending funding and
	// effect-only repair. It reconstructs inputs from the first persisted intent.
	model.ProcessPendingBillingSettlementsOnce()
	model.ProcessPendingBillingSettlementsOnce()
	if cfg.Role == "refund_admission" {
		c, info := billingProcessContext(cfg)
		var initial model.BillingSettlement
		require.NoError(t, db.Where("operation_key = ?", "request:process-recovery-request:pre-consume").First(&initial).Error)
		require.Equal(t, model.BillingSettlementStatusApplied, initial.Status)
		session := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: 951, consumed: int(initial.AppliedFundingDelta)}, preConsumedQuota: int(initial.AppliedFundingDelta)}
		session.Refund(c)
		session.Refund(c)
	}
	var record model.BillingSettlement
	if cfg.AdmissionOnly {
		require.NoError(t, db.Where("operation_key = ?", "request:process-recovery-request:pre-consume").First(&record).Error)
	} else {
		record = billingProcessRecord(t, db)
	}
	t.Logf("recovery pid=%d status=%s effect=%s attempts=%d next_attempt=%d", os.Getpid(), record.Status, record.EffectStatus, record.Attempts, record.NextAttempt)
	if record.Status == model.BillingSettlementStatusApplied {
		var effect *model.BillingSettlementEffect
		if record.EffectPayload != "" {
			effect = &model.BillingSettlementEffect{}
			require.NoError(t, common.UnmarshalJsonStr(record.EffectPayload, effect))
		}
		applied, duplicate, err := model.ApplyBillingSettlementOnce(model.BillingSettlementInput{
			OperationKey: record.OperationKey, Source: record.Source, UserID: record.UserID,
			SubscriptionID: record.SubscriptionID, TokenID: record.TokenID,
			FundingDelta: record.FundingDelta, TokenDelta: record.TokenDelta,
			TaskID: record.TaskID, TaskQuota: record.TaskQuota, TaskQuotaTarget: record.TaskQuotaTarget,
			SubscriptionPreConsumeRequestID: record.SubscriptionPreConsumeRequestID,
			FinalizeSubscriptionPreConsume:  record.FinalizeSubscriptionPreConsume, AllowMissingToken: record.AllowMissingToken,
			ManualOnFailure: record.ManualOnFailure, PreConsumeRequestID: record.PreConsumeRequestID,
			PreConsumeModelName: record.PreConsumeModelName, PreConsumeRequestedQuota: record.PreConsumeRequestedQuota,
			PreConsumeEffectiveQuota: record.PreConsumeEffectiveQuota, Effect: effect,
		})
		require.NoError(t, err)
		require.True(t, duplicate)
		require.Equal(t, record.AppliedFundingDelta, applied, "a new process must return the original signed result, including zero")
	}
	if cfg.Phase == "complete" {
		pauseBillingProcess(t, cfg, billingProcessBarrier{Phase: cfg.Phase})
	}
}

func billingProcessContext(cfg billingProcessConfig) (*gin.Context, *relaycommon.RelayInfo) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{RequestId: "process-recovery-request", UserId: 951, TokenId: 952,
		TokenKey: "synthetic-process-recovery-key", OriginModelName: "process-recovery-model", ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: cfg.Preference}}
	if cfg.Playground {
		info.IsPlayground, info.TokenId, info.TokenKey = true, 0, ""
	}
	return c, info
}

func billingProcessUsage(quota int) (int, int) {
	if quota == 0 {
		return 0, 0
	}
	return quota - 2, 2
}

func openBillingProcessDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	configureBillingProcessDB(t, db)
	return db
}

func configureBillingProcessDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	db.Logger = logger.Default.LogMode(logger.Silent)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	// Bound SQLite writer waits. Use SQLite's durable journal; no
	// synchronous=OFF or memory/WAL shortcuts weaken the interruption test.
	require.NoError(t, db.Exec("PRAGMA busy_timeout = 30000").Error)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
}

func seedBillingProcessDB(t *testing.T, funding billingProcessFunding) (string, *gorm.DB) {
	t.Helper()
	dir, db, _ := seedBillingProcessDatabases(t, funding, false)
	return dir, db
}

func seedBillingProcessDatabases(t *testing.T, funding billingProcessFunding, separateLog bool) (string, *gorm.DB, *gorm.DB) {
	t.Helper()
	dir := t.TempDir()
	db := openBillingProcessDB(t, filepath.Join(dir, "ledger.sqlite"))
	logDB := db
	if separateLog {
		logDB = openBillingProcessDB(t, filepath.Join(dir, "logs.sqlite"))
	}
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{},
		&model.BillingSettlement{}, &model.BillingPreConsumeSelection{}, &model.CacheInvalidationTask{},
		&model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}, &model.SubscriptionPlan{}))
	require.NoError(t, logDB.AutoMigrate(&model.Log{}, &model.BillingLogReceipt{}))
	if separateLog {
		require.False(t, db.Migrator().HasTable(&model.Log{}), "the business DB must not silently receive consume logs")
		require.False(t, db.Migrator().HasTable(&model.BillingLogReceipt{}))
		require.False(t, logDB.Migrator().HasTable(&model.User{}))
		require.False(t, logDB.Migrator().HasTable(&model.BillingSettlement{}))
	}
	require.NoError(t, db.Create(&model.User{Id: 951, Username: "synthetic-process-recovery", Quota: funding.wallet}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 952, UserId: 951, Key: "synthetic-process-recovery-key",
		Name: "synthetic-process-token", RemainQuota: 1000, Status: common.TokenStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 953}).Error)
	require.NoError(t, db.Create(&model.SubscriptionPlan{Id: 954, Title: "synthetic-process-plan", Enabled: true,
		TotalAmount: 1000, AllowWalletOverflow: &funding.allowOverflow, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.UserSubscription{Id: 955, UserId: 951, PlanId: 954, Status: "active",
		AmountTotal: 1000, AmountUsed: funding.subscriptionUsed, AllowWalletOverflow: funding.allowOverflow,
		StartTime: now - 3600, EndTime: now + 86400, LastResetTime: now - 3600}).Error)
	return dir, db, logDB
}

// Callbacks exist only in the test child. The marker is written after the SQL
// at that boundary succeeded, and the process remains blocked until killed.
func installBillingProcessBarrier(t *testing.T, db, logDB *gorm.DB, cfg billingProcessConfig) {
	t.Helper()
	if cfg.Phase == "" || cfg.Phase == "complete" {
		return
	}
	var once sync.Once
	barrierDB, database := db, "ledger"
	if cfg.Phase == "funding_committed" || cfg.Phase == "receipt_before_log" || cfg.Phase == "log_before_commit" {
		barrierDB = logDB
		if cfg.SeparateLogDB {
			database = "logs"
		}
	}
	stop := func(tx *gorm.DB) {
		if tx.Error != nil {
			return
		}
		once.Do(func() {
			inTx := billingProcessHasSQLTransaction(tx.Statement.ConnPool)
			pauseBillingProcess(t, cfg, billingProcessBarrier{Phase: cfg.Phase, Table: tx.Statement.Table, Database: database, InsideTransaction: inTx})
		})
	}
	create := func(tx *gorm.DB) {
		switch cfg.Phase {
		case "intent_committed":
			target := model.BillingRequestFinalizeOperationKey("process-recovery-request")
			if cfg.AdmissionOnly {
				target = "request:process-recovery-request:pre-consume"
			}
			if record, ok := tx.Statement.Dest.(*model.BillingSettlement); ok && record.OperationKey == target {
				stop(tx)
			}
		case "funding_committed", "receipt_before_log":
			if tx.Statement.Table == "billing_log_receipts" {
				stop(tx)
			}
		case "log_before_commit":
			if tx.Statement.Table == "logs" {
				stop(tx)
			}
		}
	}
	update := func(tx *gorm.DB) {
		if tx.Statement.Table != "billing_settlements" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]interface{})
		if !ok {
			return
		}
		if cfg.Phase == "funding_before_commit" && updates["status"] == model.BillingSettlementStatusApplied ||
			cfg.Phase == "log_committed" && updates["effect_status"] == model.BillingSettlementEffectApplying ||
			cfg.Phase == "usage_before_commit" && updates["effect_status"] == model.BillingSettlementEffectApplied {
			stop(tx)
		}
	}
	const name = "test:billing-process-barrier"
	if cfg.Phase == "intent_committed" {
		require.NoError(t, barrierDB.Callback().Create().After("gorm:commit_or_rollback_transaction").Register(name, create))
	} else if cfg.Phase == "funding_committed" {
		require.NoError(t, barrierDB.Callback().Create().Before("gorm:create").Register(name, create))
	} else if cfg.Phase == "receipt_before_log" || cfg.Phase == "log_before_commit" {
		require.NoError(t, barrierDB.Callback().Create().After("gorm:create").Register(name, create))
	} else if cfg.Phase == "log_committed" {
		require.NoError(t, barrierDB.Callback().Update().Before("gorm:update").Register(name, update))
	} else if cfg.Phase == "funding_before_commit" || cfg.Phase == "usage_before_commit" {
		require.NoError(t, barrierDB.Callback().Update().After("gorm:update").Register(name, update))
	} else {
		t.Fatalf("unknown interruption phase %q", cfg.Phase)
	}
}

func writeBillingProcessJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	// Rename prevents the parent from observing a partially written marker.
	require.NoError(t, os.WriteFile(path+".writing", data, 0600))
	require.NoError(t, os.Rename(path+".writing", path))
}

func pauseBillingProcess(t *testing.T, cfg billingProcessConfig, event billingProcessBarrier) {
	t.Helper()
	event.PID = os.Getpid()
	writeBillingProcessJSON(t, cfg.Ready, event)
	<-make(chan struct{})
}

type billingProcessOutput struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *billingProcessOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *billingProcessOutput) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

type billingTestProcess struct {
	cmd    *exec.Cmd
	done   chan error
	exited chan struct{}
	output *billingProcessOutput
	ready  string
}

func startBillingTestProcess(t *testing.T, dir string, cfg billingProcessConfig) *billingTestProcess {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	file, err := os.CreateTemp(dir, "process-*.json")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	cfg.Database, cfg.Ready = filepath.Join(dir, "ledger.sqlite"), file.Name()+".ready"
	writeBillingProcessJSON(t, file.Name(), cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestBillingProcessHelper$", "-test.timeout=40s", "-test.v")
	cmd.Env = append(os.Environ(), billingProcessConfigEnv+"="+file.Name())
	p := &billingTestProcess{cmd: cmd, done: make(chan error, 1), exited: make(chan struct{}), output: &billingProcessOutput{}, ready: cfg.Ready}
	cmd.Stdout, cmd.Stderr = p.output, p.output
	require.NoError(t, cmd.Start())
	go func() {
		p.done <- cmd.Wait()
		close(p.exited)
	}()
	t.Cleanup(func() {
		cancel()
		<-p.exited
	})
	return p
}

func awaitBillingProcessBarrier(t *testing.T, p *billingTestProcess) billingProcessBarrier {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		data, err := os.ReadFile(p.ready)
		if err == nil {
			var event billingProcessBarrier
			require.NoError(t, common.Unmarshal(data, &event))
			require.Equal(t, p.cmd.Process.Pid, event.PID)
			return event
		}
		require.True(t, billingProcessMarkerNotReady(err), "barrier read: %v", err)
		select {
		case err := <-p.done:
			t.Fatalf("child exited before its barrier: %v\n%s", err, p.output.text())
		case <-timer.C:
			t.Fatalf("child never reached its barrier\n%s", p.output.text())
		case <-ticker.C:
		}
	}
}

func billingProcessHasSQLTransaction(pool gorm.ConnPool) bool {
	switch tx := pool.(type) {
	case *sql.Tx:
		return tx != nil
	case *gorm.PreparedStmtTX:
		return tx != nil && billingProcessHasSQLTransaction(tx.Tx)
	default:
		return false
	}
}

func TestBillingProcessTransactionDetection(t *testing.T) {
	db := openBillingProcessDB(t, filepath.Join(t.TempDir(), "transaction.sqlite"))
	for _, prepared := range []bool{false, true} {
		t.Run(fmt.Sprintf("prepared_%t", prepared), func(t *testing.T) {
			connection := db.Session(&gorm.Session{PrepareStmt: prepared})
			require.False(t, billingProcessHasSQLTransaction(connection.Statement.ConnPool), "an open connection is not a transaction")
			tx := connection.Begin()
			require.NoError(t, tx.Error)
			t.Cleanup(func() { require.NoError(t, tx.Rollback().Error) })
			require.True(t, billingProcessHasSQLTransaction(tx.Statement.ConnPool), "the marker must unwrap to a real SQL transaction")
		})
	}
	require.False(t, billingProcessHasSQLTransaction((*sql.Tx)(nil)))
	require.False(t, billingProcessHasSQLTransaction((*gorm.PreparedStmtTX)(nil)))
}

func billingProcessMarkerNotReady(err error) bool {
	// Windows can temporarily deny access while the atomic rename is observed.
	// Retry only ERROR_SHARING_VIOLATION (32), never arbitrary I/O failures.
	return errors.Is(err, os.ErrNotExist) || runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(32))
}

func killBillingTestProcess(t *testing.T, p *billingTestProcess, phase string) billingProcessBarrier {
	t.Helper()
	event := awaitBillingProcessBarrier(t, p)
	require.Equal(t, phase, event.Phase)
	if phase != "complete" && phase != "intent_committed" {
		require.True(t, event.InsideTransaction, "the interruption must hit the actual SQL transaction")
	}
	require.NoError(t, p.cmd.Process.Kill())
	err := <-p.done
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "an OS-forced termination must not be a normal test exit")
	require.False(t, exitErr.Success())
	t.Logf("forced termination pid=%d phase=%s database=%s table=%s inside_transaction=%t", event.PID, phase, event.Database, event.Table, event.InsideTransaction)
	return event
}

func finishBillingTestProcess(t *testing.T, p *billingTestProcess) {
	t.Helper()
	err := <-p.done
	require.NoError(t, err, "child process failed\n%s", p.output.text())
}

func billingProcessRecord(t *testing.T, db *gorm.DB) model.BillingSettlement {
	t.Helper()
	var record model.BillingSettlement
	require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey("process-recovery-request")).First(&record).Error)
	return record
}

func assertBillingProcessLedger(t *testing.T, db *gorm.DB, funding billingProcessFunding, quota int, phase string, refund bool, playground ...bool) {
	t.Helper()
	assertBillingProcessDatabases(t, db, db, funding, quota, phase, refund, playground...)
}

func assertBillingProcessDatabases(t *testing.T, db, logDB *gorm.DB, funding billingProcessFunding, quota int, phase string, refund bool, playground ...bool) {
	t.Helper()
	beforeFunding := phase == "intent_committed" || phase == "funding_before_commit"
	fundedQuota := quota
	if beforeFunding {
		fundedQuota = 100
	}
	if refund && !beforeFunding {
		fundedQuota = 0
	}
	var user model.User
	var token model.Token
	var sub model.UserSubscription
	var channel model.Channel
	require.NoError(t, db.First(&user, 951).Error)
	require.NoError(t, db.First(&token, 952).Error)
	require.NoError(t, db.First(&sub, 955).Error)
	require.NoError(t, db.First(&channel, 953).Error)
	wallet, subUsed := funding.wallet, funding.subscriptionUsed
	if funding.source == model.BillingSettlementSourceWallet {
		wallet -= int64(fundedQuota)
	} else {
		subUsed += int64(fundedQuota)
	}
	require.EqualValues(t, wallet, user.Quota)
	require.EqualValues(t, subUsed, sub.AmountUsed)
	tokenFunded, tokenID, tokenDelta := fundedQuota, 952, quota-100
	if len(playground) > 0 && playground[0] {
		tokenFunded, tokenID, tokenDelta = 0, 0, 0
	}
	require.EqualValues(t, 1000-tokenFunded, token.RemainQuota)
	require.EqualValues(t, tokenFunded, token.UsedQuota)
	counted := phase == "complete" && !refund
	wantUsed, wantRequests := 0, 0
	if counted {
		wantUsed, wantRequests = quota, 1
	}
	require.EqualValues(t, wantUsed, user.UsedQuota)
	require.EqualValues(t, wantRequests, user.RequestCount)
	require.EqualValues(t, wantUsed, channel.UsedQuota)
	var logs []model.Log
	var receipts []model.BillingLogReceipt
	require.NoError(t, logDB.Find(&logs).Error)
	require.NoError(t, logDB.Find(&receipts).Error)
	wantLogs := 0
	if !refund && (phase == "log_committed" || phase == "usage_before_commit" || phase == "complete") {
		wantLogs = 1
	}
	require.Len(t, logs, wantLogs)
	require.Len(t, receipts, wantLogs)
	if wantLogs == 1 {
		prompt, completion := billingProcessUsage(quota)
		require.EqualValues(t, quota, logs[0].Quota)
		require.Equal(t, prompt, logs[0].PromptTokens)
		require.Equal(t, completion, logs[0].CompletionTokens)
		require.Equal(t, "process-recovery-request", logs[0].RequestId)
		require.Equal(t, model.BillingRequestFinalizeOperationKey("process-recovery-request"), receipts[0].OperationKey)
	}
	record := billingProcessRecord(t, db)
	require.Equal(t, funding.source, record.Source)
	require.EqualValues(t, 951, record.UserID)
	require.EqualValues(t, tokenID, record.TokenID)
	require.EqualValues(t, quota-100, record.FundingDelta)
	require.EqualValues(t, tokenDelta, record.TokenDelta)
	if beforeFunding {
		require.Equal(t, model.BillingSettlementStatusPending, record.Status)
		require.Zero(t, record.AppliedFundingDelta)
		require.Zero(t, record.AppliedTokenDelta)
		require.Empty(t, record.EffectStatus)
	} else {
		require.Equal(t, model.BillingSettlementStatusApplied, record.Status)
		require.EqualValues(t, quota-100, record.AppliedFundingDelta)
		require.EqualValues(t, tokenDelta, record.AppliedTokenDelta)
		if !refund {
			if counted {
				require.Equal(t, model.BillingSettlementEffectApplied, record.EffectStatus)
			} else {
				require.Equal(t, model.BillingSettlementEffectPending, record.EffectStatus)
			}
		} else {
			require.Empty(t, record.EffectStatus)
		}
	}
	if refund {
		require.Empty(t, record.EffectPayload)
	} else {
		var effect model.BillingSettlementEffect
		require.NoError(t, common.UnmarshalJsonStr(record.EffectPayload, &effect))
		require.True(t, effect.QuotaIsActual)
		require.EqualValues(t, quota, effect.Quota)
	}
	var selections []model.BillingPreConsumeSelection
	require.NoError(t, db.Find(&selections).Error)
	require.Len(t, selections, 1)
	require.Equal(t, funding.source, selections[0].Source)
	require.EqualValues(t, 100, selections[0].EffectiveQuota)
	var preconsume []model.SubscriptionPreConsumeRecord
	require.NoError(t, db.Find(&preconsume).Error)
	var settlementCount int64
	require.NoError(t, db.Model(&model.BillingSettlement{}).Count(&settlementCount).Error)
	if funding.source == model.BillingSettlementSourceSubscription {
		require.EqualValues(t, 1, settlementCount)
		require.Len(t, preconsume, 1)
		require.EqualValues(t, 100, preconsume[0].PreConsumed)
		require.Equal(t, "process-recovery-request", record.SubscriptionPreConsumeRequestID)
		wantStatus := "consumed"
		if refund && !beforeFunding {
			wantStatus = "refunded"
		}
		require.Equal(t, wantStatus, preconsume[0].Status)
	} else {
		require.EqualValues(t, 2, settlementCount)
		require.Empty(t, preconsume)
	}
	t.Logf("ledger phase=%s source=%s quota=%d wallet=%d subscription_used=%d token_remaining=%d token_used=%d user_used=%d requests=%d channel_used=%d logs=%d receipts=%d funding=%s effect=%s",
		phase, record.Source, quota, user.Quota, sub.AmountUsed, token.RemainQuota, token.UsedQuota, user.UsedQuota,
		user.RequestCount, channel.UsedQuota, len(logs), len(receipts), record.Status, record.EffectStatus)
}

func billingProcessSnapshot(t *testing.T, db *gorm.DB) [32]byte {
	t.Helper()
	return billingProcessDatabasesSnapshot(t, db, db)
}

func billingProcessDatabasesSnapshot(t *testing.T, db, logDB *gorm.DB) [32]byte {
	t.Helper()
	var snapshot struct {
		Users       []model.User
		Tokens      []model.Token
		Channels    []model.Channel
		Subs        []model.UserSubscription
		Reserved    []model.SubscriptionPreConsumeRecord
		Selections  []model.BillingPreConsumeSelection
		Settlements []model.BillingSettlement
		Logs        []model.Log
		Receipts    []model.BillingLogReceipt
		Outbox      []model.CacheInvalidationTask
	}
	for _, target := range []any{&snapshot.Users, &snapshot.Tokens, &snapshot.Channels, &snapshot.Subs, &snapshot.Reserved,
		&snapshot.Selections, &snapshot.Settlements, &snapshot.Outbox} {
		require.NoError(t, db.Order("id").Find(target).Error)
	}
	for _, target := range []any{&snapshot.Logs, &snapshot.Receipts} {
		require.NoError(t, logDB.Order("id").Find(target).Error)
	}
	data, err := common.Marshal(snapshot)
	require.NoError(t, err)
	return sha256.Sum256(data)
}

func TestBillingProcessCrashRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	phases := []string{"intent_committed", "funding_before_commit", "funding_committed", "receipt_before_log",
		"log_before_commit", "log_committed", "usage_before_commit", "complete"}
	for _, funding := range billingProcessFundingCases() {
		for _, quota := range []int{130, 100, 6, 0} {
			for _, phase := range phases {
				t.Run(fmt.Sprintf("%s/quota_%d/%s", funding.name, quota, phase), func(t *testing.T) {
					dir, db := seedBillingProcessDB(t, funding)
					cfg := billingProcessConfig{Role: "work", Phase: phase, Preference: funding.preference, Source: funding.source, Quota: quota}
					killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), phase)
					assertBillingProcessLedger(t, db, funding, quota, phase, false)
					original := billingProcessRecord(t, db)
					cfg.Role, cfg.Phase = "recover", ""
					finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
					assertBillingProcessLedger(t, db, funding, quota, "complete", false)
					applied := billingProcessRecord(t, db)
					require.Equal(t, original.EffectPayload, applied.EffectPayload, "the first persisted usage, including explicit zero, must survive restart")
					snapshot := billingProcessSnapshot(t, db)
					finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
					require.Equal(t, snapshot, billingProcessSnapshot(t, db), "another OS process must not change any durable accounting row")
				})
			}
		}
	}
}

func TestBillingProcessRefundRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	for _, funding := range billingProcessFundingCases() {
		for _, phase := range []string{"intent_committed", "funding_before_commit", "complete"} {
			t.Run(funding.name+"/"+phase, func(t *testing.T) {
				dir, db := seedBillingProcessDB(t, funding)
				cfg := billingProcessConfig{Role: "work", Phase: phase, Preference: funding.preference, Source: funding.source, Refund: true}
				killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), phase)
				assertBillingProcessLedger(t, db, funding, 0, phase, true)
				cfg.Role, cfg.Phase = "recover", ""
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				assertBillingProcessLedger(t, db, funding, 0, "complete", true)
				snapshot := billingProcessSnapshot(t, db)
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				require.Equal(t, snapshot, billingProcessSnapshot(t, db))
			})
		}
	}
}

func TestBillingProcessRepeatedCrashRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	for _, funding := range billingProcessFundingCases() {
		for _, quota := range []int{130, 0} {
			t.Run(fmt.Sprintf("%s/quota_%d", funding.name, quota), func(t *testing.T) {
				dir, db := seedBillingProcessDB(t, funding)
				cfg := billingProcessConfig{Role: "work", Phase: "funding_before_commit", Preference: funding.preference, Source: funding.source, Quota: quota}
				killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), cfg.Phase)
				assertBillingProcessLedger(t, db, funding, quota, cfg.Phase, false)
				cfg.Role = "recover"
				for _, phase := range []string{"log_committed", "usage_before_commit"} {
					cfg.Phase = phase
					killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), phase)
					assertBillingProcessLedger(t, db, funding, quota, phase, false)
				}
				cfg.Phase = ""
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				assertBillingProcessLedger(t, db, funding, quota, "complete", false)
				snapshot := billingProcessSnapshot(t, db)
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				require.Equal(t, snapshot, billingProcessSnapshot(t, db))
			})
		}
	}
}

func TestBillingProcessConcurrentRecoveryAndRefund(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	for _, funding := range billingProcessFundingCases() {
		for _, quota := range []int{130, 0} {
			t.Run(fmt.Sprintf("%s/quota_%d", funding.name, quota), func(t *testing.T) {
				dir, db := seedBillingProcessDB(t, funding)
				cfg := billingProcessConfig{Role: "work", Phase: "intent_committed", Preference: funding.preference, Source: funding.source, Quota: quota}
				killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), cfg.Phase)
				original := billingProcessRecord(t, db)
				cfg.Phase, cfg.Release = "", filepath.Join(dir, "release")
				workers := make([]*billingTestProcess, 0, 3)
				for _, role := range []string{"recover", "recover", "refund_competitor"} {
					cfg.Role = role
					p := startBillingTestProcess(t, dir, cfg)
					workers = append(workers, p)
					awaitBillingProcessBarrier(t, p)
				}
				require.NoError(t, os.WriteFile(cfg.Release, []byte("release"), 0600))
				for _, p := range workers {
					finishBillingTestProcess(t, p)
				}
				cfg.Role, cfg.Release = "recover", ""
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				current := billingProcessRecord(t, db)
				t.Logf("post-contention funding=%s effect=%s attempts=%d next_attempt=%d cause=%s", current.Status, current.EffectStatus, current.Attempts, current.NextAttempt, current.LastError)
				if current.Status == model.BillingSettlementStatusPending {
					require.Positive(t, current.Attempts)
					require.True(t, strings.Contains(current.LastError, "SQLITE_BUSY") || strings.Contains(current.LastError, "database is locked"),
						"only the observed transient writer contention may be scheduled")
					assertBillingProcessLedger(t, db, funding, quota, "intent_committed", false)
					// Honor the production durable backoff, rather than treating an
					// intentionally not-yet-due intent as a failed financial recovery.
					// One scheduled attempt is enough after all competing writers exit;
					// any further error must fail the test instead of retrying to green.
					wait := time.Until(time.Unix(current.NextAttempt, 0))
					require.Positive(t, current.NextAttempt)
					require.LessOrEqual(t, wait, 64*time.Second)
					if wait > 0 {
						timer := time.NewTimer(wait)
						<-timer.C
					}
					finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				}
				assertBillingProcessLedger(t, db, funding, quota, "complete", false)
				require.Equal(t, original.EffectPayload, billingProcessRecord(t, db).EffectPayload)
				snapshot := billingProcessSnapshot(t, db)
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
				require.Equal(t, snapshot, billingProcessSnapshot(t, db))
			})
		}
	}
}

func TestBillingProcessUnsupportedStateIsNotRetried(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	for _, funding := range billingProcessFundingCases()[:2] {
		t.Run(funding.name, func(t *testing.T) {
			dir, db := seedBillingProcessDB(t, funding)
			cfg := billingProcessConfig{Role: "work", Phase: "intent_committed", Preference: funding.preference, Source: funding.source, Quota: 130}
			killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), cfg.Phase)
			assertBillingProcessLedger(t, db, funding, cfg.Quota, cfg.Phase, false)
			// Inject an unsupported durable state. This tests the runner's allowlist,
			// not a real provider's outcome-unknown protocol or commit ambiguity.
			record := billingProcessRecord(t, db)
			require.NoError(t, db.Model(&record).Update("status", "outcome_unknown").Error)
			snapshot := billingProcessSnapshot(t, db)
			cfg.Role, cfg.Phase = "recover", ""
			finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
			require.Equal(t, snapshot, billingProcessSnapshot(t, db), "unsupported state must remain durably inspectable without automatic balance changes")
		})
	}
}

func TestBillingProcessSubscriptionPeriodRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("OS-process recovery matrix; run without -short")
	}
	funding := billingProcessFundingCases()[1]
	for _, phase := range []string{"intent_committed", "funding_committed"} {
		t.Run(phase, func(t *testing.T) {
			dir, db := seedBillingProcessDB(t, funding)
			cfg := billingProcessConfig{Role: "work", Phase: phase, Preference: funding.preference, Source: funding.source, Quota: 130}
			killBillingTestProcess(t, startBillingTestProcess(t, dir, cfg), phase)
			original := billingProcessRecord(t, db)
			if phase == "funding_committed" {
				// Applied funding permits the real reset API. Only effects of the
				// original period may recover afterward, without charging the new one.
				cfg.Role, cfg.Phase = "reset", ""
				finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
			} else {
				// A completed reset fixture puts the original pending intent in an
				// older period. It does not assert that the admin API allows such a
				// reset while same-period funding is unresolved.
				var sub model.UserSubscription
				require.NoError(t, db.First(&sub, 955).Error)
				require.NoError(t, db.Model(&sub).Updates(map[string]interface{}{
					"last_reset_time": sub.LastResetTime + 1, "amount_used": 0,
				}).Error)
			}
			cfg.Role, cfg.Phase = "recover", ""
			finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
			record := billingProcessRecord(t, db)
			require.Equal(t, original.EffectPayload, record.EffectPayload)
			var sub model.UserSubscription
			var user model.User
			var token model.Token
			var channel model.Channel
			require.NoError(t, db.First(&sub, 955).Error)
			require.NoError(t, db.First(&user, 951).Error)
			require.NoError(t, db.First(&token, 952).Error)
			require.NoError(t, db.First(&channel, 953).Error)
			require.Zero(t, sub.AmountUsed, "the new subscription period cannot pay for the original request")
			require.EqualValues(t, 1000, user.Quota)
			var logs []model.Log
			var receipts []model.BillingLogReceipt
			require.NoError(t, db.Find(&logs).Error)
			require.NoError(t, db.Find(&receipts).Error)
			if phase == "intent_committed" {
				require.Equal(t, model.BillingSettlementStatusManual, record.Status)
				require.Contains(t, record.LastError, model.ErrSubscriptionSettlementPeriodChanged.Error())
				require.Zero(t, record.AppliedFundingDelta)
				require.Zero(t, record.AppliedTokenDelta)
				require.EqualValues(t, 900, token.RemainQuota)
				require.EqualValues(t, 100, token.UsedQuota)
				require.Zero(t, user.UsedQuota)
				require.Zero(t, user.RequestCount)
				require.Zero(t, channel.UsedQuota)
				require.Empty(t, logs)
				require.Empty(t, receipts)
			} else {
				require.Equal(t, model.BillingSettlementStatusApplied, record.Status)
				require.Equal(t, model.BillingSettlementEffectApplied, record.EffectStatus)
				require.EqualValues(t, 30, record.AppliedFundingDelta)
				require.EqualValues(t, 30, record.AppliedTokenDelta)
				require.EqualValues(t, 870, token.RemainQuota)
				require.EqualValues(t, 130, token.UsedQuota)
				require.EqualValues(t, 130, user.UsedQuota)
				require.EqualValues(t, 1, user.RequestCount)
				require.EqualValues(t, 130, channel.UsedQuota)
				require.Len(t, logs, 1)
				require.Len(t, receipts, 1)
				var other map[string]interface{}
				require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
				require.EqualValues(t, 130, other["subscription_used"], "logs must retain the original period's usage")
				require.EqualValues(t, 870, other["subscription_remain"])
			}
			snapshot := billingProcessSnapshot(t, db)
			finishBillingTestProcess(t, startBillingTestProcess(t, dir, cfg))
			require.Equal(t, snapshot, billingProcessSnapshot(t, db))
		})
	}
}
