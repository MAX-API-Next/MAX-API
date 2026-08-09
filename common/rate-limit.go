package common

import (
	"context"
	"sync"
	"time"
)

type InMemoryRateLimiter struct {
	store              map[string]*[]int64
	mutex              sync.Mutex
	expirationDuration time.Duration
	initOnce           sync.Once
	stopOnce           sync.Once
	stop               chan struct{}
	done               chan struct{}
	stopped            bool
}

func (l *InMemoryRateLimiter) Init(expirationDuration time.Duration) {
	l.initOnce.Do(func() {
		l.mutex.Lock()
		defer l.mutex.Unlock()
		if l.stopped {
			return
		}
		l.store = make(map[string]*[]int64)
		l.expirationDuration = expirationDuration
		if expirationDuration > 0 {
			l.stop = make(chan struct{})
			l.done = make(chan struct{})
			go l.clearExpiredItems(expirationDuration, l.stop, l.done)
		}
	})
}

func (l *InMemoryRateLimiter) clearExpiredItems(interval time.Duration, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			l.mutex.Lock()
			now := time.Now().Unix()
			for key := range l.store {
				queue := l.store[key]
				size := len(*queue)
				if size == 0 || now-(*queue)[size-1] > int64(interval.Seconds()) {
					delete(l.store, key)
				}
			}
			l.mutex.Unlock()
		}
	}
}

func (l *InMemoryRateLimiter) Stop(ctx context.Context) error {
	l.mutex.Lock()
	l.stopped = true
	stop := l.stop
	done := l.done
	l.mutex.Unlock()
	if stop == nil || done == nil {
		return nil
	}

	l.stopOnce.Do(func() {
		close(stop)
	})
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Request parameter duration's unit is seconds
func (l *InMemoryRateLimiter) Request(key string, maxRequestNum int, duration int64) bool {
	allowed, _ := l.RequestWithRetry(key, maxRequestNum, duration)
	return allowed
}

// RequestWithRetry records a request and returns the approximate time until
// the oldest request leaves the window when the request is rejected.
func (l *InMemoryRateLimiter) RequestWithRetry(key string, maxRequestNum int, duration int64) (bool, time.Duration) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if maxRequestNum <= 0 {
		return true, 0
	}
	// [old <-- new]
	queue, ok := l.store[key]
	now := time.Now().Unix()
	if ok {
		if len(*queue) < maxRequestNum {
			*queue = append(*queue, now)
			return true, 0
		} else {
			if now-(*queue)[0] >= duration {
				*queue = (*queue)[1:]
				*queue = append(*queue, now)
				return true, 0
			} else {
				waitSeconds := duration - (now - (*queue)[0])
				if waitSeconds < 1 {
					waitSeconds = 1
				}
				return false, time.Duration(waitSeconds) * time.Second
			}
		}
	} else {
		s := make([]int64, 0, maxRequestNum)
		l.store[key] = &s
		*(l.store[key]) = append(*(l.store[key]), now)
	}
	return true, 0
}

func (l *InMemoryRateLimiter) Allow(key string, maxRequestNum int, duration int64) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if maxRequestNum <= 0 {
		return true
	}
	queue, ok := l.store[key]
	if !ok || len(*queue) < maxRequestNum {
		return true
	}
	now := time.Now().Unix()
	return now-(*queue)[0] >= duration
}
