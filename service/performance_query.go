package service

import (
	"errors"
	"fmt"
	"time"
)

const (
	defaultPerformanceHours = 1
	maxPerformanceHours     = 168
	defaultPerformanceLimit = 100
	maxPerformanceLimit     = 200
)

type normalizedPerformanceQuery struct {
	StartAt int64
	EndAt   int64
	Hours   int
	Limit   int
}

func normalizePerformanceQuery(
	startAt int64,
	endAt int64,
	hours int,
	limit int,
	invalidQueryError error,
) (normalizedPerformanceQuery, bool, error) {
	if invalidQueryError == nil {
		invalidQueryError = errors.New("invalid performance query")
	}
	if endAt <= 0 {
		endAt = time.Now().Unix()
	}
	rangeWasClamped := false
	if startAt <= 0 {
		if hours <= 0 {
			hours = defaultPerformanceHours
		}
		if hours > maxPerformanceHours {
			hours = maxPerformanceHours
			rangeWasClamped = true
		}
		startAt = endAt - int64(hours)*int64(time.Hour/time.Second)
	}
	if endAt <= startAt {
		return normalizedPerformanceQuery{}, false, fmt.Errorf("%w: end must be greater than start", invalidQueryError)
	}
	if limit <= 0 {
		limit = defaultPerformanceLimit
	}
	if limit > maxPerformanceLimit {
		limit = maxPerformanceLimit
	}
	maxWindow := int64(maxPerformanceHours) * int64(time.Hour/time.Second)
	if endAt-startAt > maxWindow {
		startAt = endAt - maxWindow
		rangeWasClamped = true
	}
	return normalizedPerformanceQuery{
		StartAt: startAt,
		EndAt:   endAt,
		Hours:   hours,
		Limit:   limit,
	}, rangeWasClamped, nil
}
