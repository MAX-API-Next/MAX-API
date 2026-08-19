package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PerfMetric stores aggregated relay performance metrics for the model square.
type PerfMetric struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	ModelName      string `json:"model_name" gorm:"size:128;uniqueIndex:idx_perf_model_group_bucket,priority:1"`
	Group          string `json:"group" gorm:"column:group;size:64;uniqueIndex:idx_perf_model_group_bucket,priority:2"`
	BucketTs       int64  `json:"bucket_ts" gorm:"uniqueIndex:idx_perf_model_group_bucket,priority:3;index:idx_perf_bucket_ts"`
	RequestCount   int64  `json:"-" gorm:"default:0"`
	SuccessCount   int64  `json:"-" gorm:"default:0"`
	TotalLatencyMs int64  `json:"-" gorm:"default:0"`
	TtftSumMs      int64  `json:"-" gorm:"default:0"`
	TtftCount      int64  `json:"-" gorm:"default:0"`
	OutputTokens   int64  `json:"-" gorm:"default:0"`
	GenerationMs   int64  `json:"-" gorm:"default:0"`
}

// PerfMetricFlushReceipt makes a node-local bucket commit idempotent across
// process restarts and ambiguous database responses. It is intentionally kept
// separate from PerfMetric so existing metric rows and query contracts remain
// unchanged.
type PerfMetricFlushReceipt struct {
	Id         int64  `gorm:"primaryKey"`
	ReceiptKey string `gorm:"size:64;uniqueIndex;not null"`
	ClaimToken string `gorm:"size:36;not null"`
	CreatedAt  int64  `gorm:"not null;index"`
}

func (PerfMetricFlushReceipt) TableName() string {
	return "perf_metric_flush_receipts"
}

func (PerfMetric) TableName() string {
	return "perf_metrics"
}

func UpsertPerfMetric(metric *PerfMetric) error {
	return upsertPerfMetric(DB, metric)
}

func upsertPerfMetric(db *gorm.DB, metric *PerfMetric) error {
	if metric == nil || metric.RequestCount == 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "model_name"},
			{Name: "group"},
			{Name: "bucket_ts"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"request_count":    gorm.Expr("perf_metrics.request_count + ?", metric.RequestCount),
			"success_count":    gorm.Expr("perf_metrics.success_count + ?", metric.SuccessCount),
			"total_latency_ms": gorm.Expr("perf_metrics.total_latency_ms + ?", metric.TotalLatencyMs),
			"ttft_sum_ms":      gorm.Expr("perf_metrics.ttft_sum_ms + ?", metric.TtftSumMs),
			"ttft_count":       gorm.Expr("perf_metrics.ttft_count + ?", metric.TtftCount),
			"output_tokens":    gorm.Expr("perf_metrics.output_tokens + ?", metric.OutputTokens),
			"generation_ms":    gorm.Expr("perf_metrics.generation_ms + ?", metric.GenerationMs),
		}),
	}).Create(metric).Error
}

// CommitPerfMetricBucket atomically records a durable receipt and applies the
// bucket counters once. A repeated receipt is a successful no-op, allowing a
// recovered Redis bucket to be cleaned without incrementing PerfMetric twice.
func CommitPerfMetricBucket(receiptKey string, metric *PerfMetric) (bool, error) {
	if metric == nil || metric.RequestCount == 0 {
		return false, nil
	}
	if receiptKey == "" {
		return false, gorm.ErrInvalidData
	}

	applied := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		claimToken := uuid.NewString()
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "receipt_key"}},
			DoNothing: true,
		}).Create(&PerfMetricFlushReceipt{
			ReceiptKey: receiptKey,
			ClaimToken: claimToken,
			CreatedAt:  time.Now().Unix(),
		})
		if result.Error != nil {
			return result.Error
		}
		var storedReceipt PerfMetricFlushReceipt
		if err := tx.Select("claim_token").Where("receipt_key = ?", receiptKey).Take(&storedReceipt).Error; err != nil {
			return err
		}
		// MySQL can report a row as affected for its no-op duplicate-key
		// update when clientFoundRows is enabled. Ownership is therefore proven
		// by a per-attempt claim rather than driver-specific RowsAffected values.
		if storedReceipt.ClaimToken != claimToken {
			return nil
		}
		if err := upsertPerfMetric(tx, metric); err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied, err
}

func GetPerfMetrics(modelName string, group string, startTs int64, endTs int64) ([]PerfMetric, error) {
	var metrics []PerfMetric
	query := DB.Model(&PerfMetric{}).
		Where("model_name = ? AND bucket_ts >= ? AND bucket_ts <= ?", modelName, startTs, endTs)
	if group != "" {
		query = query.Where(map[string]interface{}{"group": group})
	}
	err := query.Order("bucket_ts ASC").Find(&metrics).Error
	return metrics, err
}

type PerfMetricSummary struct {
	ModelName      string `json:"model_name"`
	RequestCount   int64  `json:"request_count"`
	SuccessCount   int64  `json:"success_count"`
	TotalLatencyMs int64  `json:"total_latency_ms"`
	OutputTokens   int64  `json:"output_tokens"`
	GenerationMs   int64  `json:"generation_ms"`
	MinBucketTs    int64  `json:"min_bucket_ts"`
	MaxBucketTs    int64  `json:"max_bucket_ts"`
}

func GetPerfMetricsSummaryAll(startTs int64, endTs int64, groups []string) ([]PerfMetricSummary, error) {
	var summaries []PerfMetricSummary
	query := DB.Model(&PerfMetric{}).
		Select("model_name, SUM(request_count) as request_count, SUM(success_count) as success_count, SUM(total_latency_ms) as total_latency_ms, SUM(output_tokens) as output_tokens, SUM(generation_ms) as generation_ms, MIN(bucket_ts) as min_bucket_ts, MAX(bucket_ts) as max_bucket_ts").
		Where("bucket_ts >= ? AND bucket_ts <= ?", startTs, endTs)
	if groups != nil {
		if len(groups) == 0 {
			return summaries, nil
		}
		query = query.Where(map[string]interface{}{"group": groups})
	}
	err := query.
		Group("model_name").
		Having("SUM(request_count) > 0").
		Find(&summaries).Error
	return summaries, err
}

func DeletePerfMetricsBefore(cutoffTs int64) error {
	if cutoffTs <= 0 {
		return nil
	}
	return DB.Where("bucket_ts < ?", cutoffTs).Delete(&PerfMetric{}).Error
}

func DeletePerfMetricFlushReceiptsBefore(cutoffTs int64) error {
	if cutoffTs <= 0 {
		return nil
	}
	return DB.Where("created_at < ?", cutoffTs).Delete(&PerfMetricFlushReceipt{}).Error
}

func GetPerfMetricFlushReceiptKeys(receiptKeys []string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(receiptKeys))
	if len(receiptKeys) == 0 {
		return result, nil
	}
	var receipts []PerfMetricFlushReceipt
	if err := DB.Select("receipt_key").Where("receipt_key IN ?", receiptKeys).Find(&receipts).Error; err != nil {
		return nil, err
	}
	for _, receipt := range receipts {
		result[receipt.ReceiptKey] = struct{}{}
	}
	return result, nil
}

func PerfMetricStartTime(hours int) int64 {
	if hours <= 0 {
		hours = 24
	}
	return time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
}
