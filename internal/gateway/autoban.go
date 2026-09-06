package gateway

import (
	"sync"
	"time"
)

// autoBanManager 实现"被限流(429)超限即临时封禁"的进程内状态：
//   - 对来源 IP 与 API Key(subject) 各自维护滑动违规计数（窗口内 FIFO ms）；
//   - 窗口内违规数达到 max 即触发临时封禁（IP 封禁 / key 停用），时长 TTL；
//   - 封禁为内存态临时：到期惰性解封（请求查询时若已过 TTL 则放行并清理），
//     不写持久黑名单、不改 AccessKey.Enabled。
//
// 并发安全；`now` 可注入时钟便于测试。
type autoBanManager struct {
	mu  sync.Mutex
	now func() time.Time

	ipBans  map[string]time.Time // ip → 解封时刻
	subBans map[string]time.Time // subject → 解封时刻
	ipHits  map[string][]int64   // ip → 窗口内 429 时刻(ms)
	subHits map[string][]int64   // subject → 窗口内 429 时刻(ms)
}

// newAutoBanManager 创建自动封禁管理器。
func newAutoBanManager() *autoBanManager {
	return &autoBanManager{
		now:     time.Now,
		ipBans:  make(map[string]time.Time),
		subBans: make(map[string]time.Time),
		ipHits:  make(map[string][]int64),
		subHits: make(map[string][]int64),
	}
}

// isIPBanned 判断 IP 是否在临时封禁中；已过 TTL 则视为解封并清理。
func (m *autoBanManager) isIPBanned(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.liveBanned(m.ipBans, ip)
}

// isKeySuspended 判断 key(subject) 是否被临时停用；已过 TTL 则视为恢复并清理。
func (m *autoBanManager) isKeySuspended(subject string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.liveBanned(m.subBans, subject)
}

// liveBanned 返回 key 是否在封禁中（锁内调用），到期（now >= until）则删除该记录。
func (m *autoBanManager) liveBanned(bans map[string]time.Time, key string) bool {
	until, ok := bans[key]
	if !ok {
		return false
	}
	if !until.After(m.now()) {
		delete(bans, key)
		return false
	}
	return true
}

// banIP 临时封禁 IP（重置该 IP 的违规计数，避免解封后瞬时再触发）。
func (m *autoBanManager) banIP(ip string, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ipBans[ip] = m.now().Add(ttl)
	delete(m.ipHits, ip)
}

// suspendKey 临时停用 key（重置该 subject 的违规计数）。
func (m *autoBanManager) suspendKey(subject string, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subBans[subject] = m.now().Add(ttl)
	delete(m.subHits, subject)
}

// recordIPViolation 记录 IP 的一次 429；窗口内违规数达到 max 返回 true（调用方据此 banIP）。
func (m *autoBanManager) recordIPViolation(ip string, window time.Duration, max int) bool {
	return m.recordViolation(ip, m.ipHits, window, max)
}

// recordKeyViolation 记录 key(subject) 的一次 429；达到 max 返回 true（调用方据此 suspendKey）。
func (m *autoBanManager) recordKeyViolation(subject string, window time.Duration, max int) bool {
	return m.recordViolation(subject, m.subHits, window, max)
}

// recordViolation 记录一次违规并做窗口裁剪；达到 max 时清空计数并返回 true（锁内调用）。
func (m *autoBanManager) recordViolation(id string, hits map[string][]int64, window time.Duration, max int) bool {
	if id == "" || max <= 0 {
		return false
	}
	if window <= 0 {
		window = time.Minute
	}
	nowMs := m.now().UnixMilli()
	cutoff := nowMs - window.Milliseconds()

	m.mu.Lock()
	defer m.mu.Unlock()
	arr := hits[id]
	start := 0
	for start < len(arr) && arr[start] <= cutoff {
		start++
	}
	arr = arr[start:]
	arr = append(arr, nowMs)
	if len(arr) >= max {
		hits[id] = nil // 触发封禁即重置，避免解封后旧计数瞬时再触发
		return true
	}
	hits[id] = arr
	return false
}
