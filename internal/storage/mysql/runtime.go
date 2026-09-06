package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// runtimeConfigID 是 runtime_config 表的固定主键：该表始终只保存一行
// "运行期治理配置"快照（id=1），由控制面整份覆盖写。
const runtimeConfigID = 1

// ---- RuntimeConfigStore ----

// runtimeConfigColumns 是 runtime_config 的读取列（不含 id 固定 1）。
const runtimeConfigColumns = `qps, burst, window_seconds, ip_qps, ip_burst, global_qps, global_burst,
	auto_ban_enabled, auto_ban_window_seconds, auto_ban_max_violations, auto_ban_ban_seconds,
	COALESCE(ip_blocklist,''), COALESCE(ip_whitelist,''), updated_at`

// GetRuntimeConfig 读取运行期治理配置；尚无保存值时返回 (nil, false, nil)。
func (s *Store) GetRuntimeConfig(ctx context.Context) (*model.RuntimeConfig, bool, error) {
	var cfg model.RuntimeConfig
	var blocklistJSON, whitelistJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT `+runtimeConfigColumns+` FROM runtime_config WHERE id = ?`, runtimeConfigID,
	).Scan(&cfg.RateLimit.QPS, &cfg.RateLimit.Burst, &cfg.RateLimit.WindowSeconds,
		&cfg.RateLimit.IPQPS, &cfg.RateLimit.IPBurst,
		&cfg.RateLimit.GlobalQPS, &cfg.RateLimit.GlobalBurst,
		&cfg.AutoBan.Enabled, &cfg.AutoBan.WindowSeconds, &cfg.AutoBan.MaxViolations, &cfg.AutoBan.BanSeconds,
		&blocklistJSON, &whitelistJSON, &cfg.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("读取 runtime_config 失败: %w", err)
	}
	if err := unmarshalJSON(blocklistJSON, &cfg.IPBlocklist); err != nil {
		return nil, false, fmt.Errorf("解析 runtime_config.ip_blocklist 失败: %w", err)
	}
	if err := unmarshalJSON(whitelistJSON, &cfg.IPWhitelist); err != nil {
		return nil, false, fmt.Errorf("解析 runtime_config.ip_whitelist 失败: %w", err)
	}
	return &cfg, true, nil
}

// PutRuntimeConfig 整份覆盖保存运行期治理配置（INSERT ... ON DUPLICATE KEY UPDATE，
// 并发首写由主键去重吸收，last-writer-wins）。updated_at 为空时取当前 UTC。
func (s *Store) PutRuntimeConfig(ctx context.Context, cfg *model.RuntimeConfig) error {
	if cfg == nil {
		return fmt.Errorf("runtime config 不能为空")
	}
	blocklistJSON, err := marshalJSON(cfg.IPBlocklist)
	if err != nil {
		return fmt.Errorf("marshal runtime_config.ip_blocklist: %w", err)
	}
	whitelistJSON, err := marshalJSON(cfg.IPWhitelist)
	if err != nil {
		return fmt.Errorf("marshal runtime_config.ip_whitelist: %w", err)
	}
	rl := cfg.RateLimit
	ab := cfg.AutoBan
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO runtime_config
		   (id, qps, burst, window_seconds, ip_qps, ip_burst, global_qps, global_burst,
		    auto_ban_enabled, auto_ban_window_seconds, auto_ban_max_violations, auto_ban_ban_seconds,
		    ip_blocklist, ip_whitelist, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE
		   qps=VALUES(qps), burst=VALUES(burst), window_seconds=VALUES(window_seconds),
		   ip_qps=VALUES(ip_qps), ip_burst=VALUES(ip_burst),
		   global_qps=VALUES(global_qps), global_burst=VALUES(global_burst),
		   auto_ban_enabled=VALUES(auto_ban_enabled),
		   auto_ban_window_seconds=VALUES(auto_ban_window_seconds),
		   auto_ban_max_violations=VALUES(auto_ban_max_violations),
		   auto_ban_ban_seconds=VALUES(auto_ban_ban_seconds),
		   ip_blocklist=VALUES(ip_blocklist), ip_whitelist=VALUES(ip_whitelist), updated_at=VALUES(updated_at)`,
		runtimeConfigID, rl.QPS, rl.Burst, rl.WindowSeconds, rl.IPQPS, rl.IPBurst, rl.GlobalQPS, rl.GlobalBurst,
		ab.Enabled, ab.WindowSeconds, ab.MaxViolations, ab.BanSeconds,
		nullIfEmpty(blocklistJSON), nullIfEmpty(whitelistJSON), fmtTimeUTC(nowOr(cfg.UpdatedAt)))
	if err != nil {
		return fmt.Errorf("upsert runtime_config: %w", err)
	}
	return nil
}
