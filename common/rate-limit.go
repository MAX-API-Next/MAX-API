package common

import (
	"sync"
	"time"
)

type InMemoryRateLimiter struct {
	store              map[string]*[]int64
	mutex              sync.Mutex
	expirationDuration time.Duration
}

func (l *InMemoryRateLimiter) Init(expirationDuration time.Duration) {
	if l.store == nil {
		l.mutex.Lock()
		if l.store == nil {
			l.store = make(map[string]*[]int64)
			l.expirationDuration = expirationDuration
			if expirationDuration > 0 {
				go l.clearExpiredItems()
			}
		}
		l.mutex.Unlock()
	}
}

func (l *InMemoryRateLimiter) clearExpiredItems() {
	for {
		time.Sleep(l.expirationDuration)
		l.mutex.Lock()
		now := time.Now().Unix()
		for key := range l.store {
			queue := l.store[key]
			size := len(*queue)
			if size == 0 || now-(*queue)[size-1] > int64(l.expirationDuration.Seconds()) {
				delete(l.store, key)
			}
		}
		l.mutex.Unlock()
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
