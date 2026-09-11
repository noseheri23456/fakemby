package ratelimit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLimiterLocksAfterMaxAttempts(t *testing.T) {
	l := New(3, 15)

	key := "127.0.0.1:alice"
	assert.False(t, l.IsLocked(key), "初始未锁定")

	assert.False(t, l.RecordFailure(key))                // 1
	assert.False(t, l.RecordFailure(key))                // 2
	assert.True(t, l.RecordFailure(key), "第 3 次失败应触发锁定") // 3
	assert.True(t, l.IsLocked(key))

	// 登录成功后清除，恢复正常
	l.Reset(key)
	assert.False(t, l.IsLocked(key))
}

func TestLimiterDisabledWhenZero(t *testing.T) {
	l := New(0, 0)
	key := "ip:user"
	for i := 0; i < 100; i++ {
		assert.False(t, l.RecordFailure(key), "maxAttempts<=0 时永不锁定")
	}
	assert.False(t, l.IsLocked(key))
}

func TestLimiterWindowExpiry(t *testing.T) {
	l := New(2, 0) // 0 分钟 → 窗口极短（1 纳秒？不，0 表示 15min 回落）
	// 用极小窗口验证过期：直接构造一个过期 entry
	key := "ip:bob"
	l.RecordFailure(key)
	l.RecordFailure(key)
	assert.True(t, l.IsLocked(key))

	// 通过内部 map 把 firstSeen 推到窗口之前，模拟过期
	l.mu.Lock()
	if e, ok := l.attempts[key]; ok {
		e.firstSeen = e.firstSeen.Add(-20 * 60 * 1e9) // 20 分钟前
	}
	l.mu.Unlock()

	assert.False(t, l.IsLocked(key), "窗口过期后锁定应解除")
}
