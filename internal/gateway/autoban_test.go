package gateway

import (
	"testing"
	"time"
)

// banClock 供 autoBanManager 注入的静态时钟。
type banClock struct{ t time.Time }

func (c *banClock) now() time.Time { return c.t }

func newTestAutoBan(t *time.Time) *autoBanManager {
	m := newAutoBanManager()
	m.now = func() time.Time { return *t }
	return m
}

func TestAutoBan_IPBanAndTTLExpiry(t *testing.T) {
	now := time.Unix(0, 0)
	m := newTestAutoBan(&now)

	if m.isIPBanned("1.2.3.4") {
		t.Fatal("未封禁时不应 banned")
	}
	m.banIP("1.2.3.4", time.Second)
	if !m.isIPBanned("1.2.3.4") {
		t.Fatal("封禁期内应 banned")
	}
	// 未到 TTL 仍封禁。
	now = now.Add(999 * time.Millisecond)
	if !m.isIPBanned("1.2.3.4") {
		t.Fatal("未到 TTL 应保持封禁")
	}
	// 到期惰性解封。
	now = now.Add(time.Millisecond)
	if m.isIPBanned("1.2.3.4") {
		t.Fatal("到 TTL 应自动解封")
	}
}

func TestAutoBan_KeySuspend(t *testing.T) {
	now := time.Unix(0, 0)
	m := newTestAutoBan(&now)

	if m.isKeySuspended("partner-a") {
		t.Fatal("未停用不应 suspended")
	}
	m.suspendKey("partner-a", 2*time.Second)
	if !m.isKeySuspended("partner-a") {
		t.Fatal("停用期内应 suspended")
	}
	now = now.Add(2 * time.Second)
	if m.isKeySuspended("partner-a") {
		t.Fatal("到期应自动恢复")
	}
}

func TestAutoBan_RecordTriggersAtMax(t *testing.T) {
	now := time.Unix(0, 0)
	m := newTestAutoBan(&now)

	// 窗口内第 3 次违规达到 max=3 才触发。
	if m.recordIPViolation("203.0.113.9", 10*time.Second, 3) {
		t.Fatal("第 1 次不应触发")
	}
	if m.recordIPViolation("203.0.113.9", 10*time.Second, 3) {
		t.Fatal("第 2 次不应触发")
	}
	if !m.recordIPViolation("203.0.113.9", 10*time.Second, 3) {
		t.Fatal("第 3 次应达到阈值")
	}
	// 触发后调用方封禁。
	m.banIP("203.0.113.9", time.Minute)
	if !m.isIPBanned("203.0.113.9") {
		t.Fatal("触发后应处于封禁")
	}
}

func TestAutoBan_PruneAndClearAfterBan(t *testing.T) {
	now := time.Unix(0, 0)
	m := newTestAutoBan(&now)

	// 窗口 10s、max 2：一次违规后推进超窗，再违规不应被旧记录累计触发。
	if m.recordIPViolation("203.0.113.9", 10*time.Second, 2) {
		t.Fatal("第 1 次不应触发")
	}
	now = now.Add(11 * time.Second) // 旧记录滑出窗口
	if m.recordIPViolation("203.0.113.9", 10*time.Second, 2) {
		t.Fatal("旧记录滑出窗口, 不应触发")
	}
	// 同窗口内再违规达到 2 次 → 触发并清空计数。
	if !m.recordIPViolation("203.0.113.9", 10*time.Second, 2) {
		t.Fatal("窗口内 2 条应触发")
	}
	// 触发后计数已清空：单独一次违规不再瞬时触发（需重新攒满）。
	m.banIP("203.0.113.9", time.Minute)
	now = now.Add(time.Minute + time.Second) // 封禁到期
	if m.isIPBanned("203.0.113.9") {
		t.Fatal("封禁应已到期")
	}
	if m.recordIPViolation("203.0.113.9", 10*time.Second, 2) {
		t.Fatal("计数已清空, 单次违规不应触发")
	}
}

func TestAutoBan_KeyCountIndependent(t *testing.T) {
	now := time.Unix(0, 0)
	m := newTestAutoBan(&now)
	m.suspendKey("partner-a", time.Minute)
	if !m.isKeySuspended("partner-a") {
		t.Fatal("partner-a 应停用")
	}
	if m.isKeySuspended("partner-b") {
		t.Fatal("partner-b 不应受影响")
	}
}
