// Package ratelimit 提供登录失败限流：按「IP + 用户名」计数，
// 在窗口期内失败达到阈值即锁定，用于缓解暴力破解（A5）。
//
// 设计为进程内内存实现：单实例部署足够，且避免引入外部存储。
// 多实例部署时各实例独立计数（可接受：攻击者需分别打满每个实例）。
package ratelimit

import (
	"sync"
	"time"
)

type entry struct {
	count     int
	firstSeen time.Time
}

// Limiter 简单的失败计数器限流器（固定窗口：窗口过期后计数清零）。
type Limiter struct {
	mu          sync.Mutex
	attempts    map[string]*entry
	maxAttempts int
	window      time.Duration
}

// New 创建限流器。maxAttempts<=0 表示禁用限流（永不锁定）；
// lockMinutes<=0 时回落 15 分钟窗口。
func New(maxAttempts, lockMinutes int) *Limiter {
	window := time.Duration(lockMinutes) * time.Minute
	if window <= 0 {
		window = 15 * time.Minute
	}
	if maxAttempts < 0 {
		maxAttempts = 0
	}
	return &Limiter{
		attempts:    make(map[string]*entry),
		maxAttempts: maxAttempts,
		window:      window,
	}
}

// RecordFailure 记录一次失败尝试，返回是否已触发锁定。
func (l *Limiter) RecordFailure(key string) bool {
	if l.maxAttempts <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	e, ok := l.attempts[key]
	if !ok || now.Sub(e.firstSeen) > l.window {
		// 新建或窗口已过期 → 重新计数
		e = &entry{firstSeen: now}
		l.attempts[key] = e
	}
	e.count++
	return e.count >= l.maxAttempts
}

// Reset 清除某 key 的失败记录（登录成功时调用，避免误伤正常用户）。
func (l *Limiter) Reset(key string) {
	if l.maxAttempts <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// IsLocked 返回某 key 是否处于锁定窗口内。
func (l *Limiter) IsLocked(key string) bool {
	if l.maxAttempts <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.attempts[key]
	if !ok {
		return false
	}
	if time.Since(e.firstSeen) > l.window {
		delete(l.attempts, key)
		return false
	}
	return e.count >= l.maxAttempts
}
