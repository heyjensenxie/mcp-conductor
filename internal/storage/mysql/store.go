// Package mysql 提供基于 MySQL 5.7+ 的存储实现，满足 storage.Store 接口。
//
// 兼容契约见 docs/architecture/database.md：SQL 不使用 MySQL 8 专属特性；
// JSON 字段（input_schema / tool_names 等）以 TEXT 保存、由应用层 Go 解析；
// 实体沿用稳定字符串 id；仅流水表使用自增主键。
// DSN 须包含 parseTime=true&loc=UTC / charset=utf8mb4，以保证时间与字符集确定。
package mysql

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	// 注册 MySQL 驱动。
	_ "github.com/go-sql-driver/mysql"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// Store 是基于 database/sql 的 MySQL 存储实现。
type Store struct {
	db         *sql.DB
	credCipher *credentialCipher
}

// Option 是 Store 构造选项。
type Option func(*Store) error

// WithCredentialKey 配置凭证加密密钥（64 位 hex，AES-256）。未配置时
// MySQL 存储拒绝落库明文凭证值。
func WithCredentialKey(keyHex string) Option {
	return func(s *Store) error {
		c, err := newCredentialCipher(keyHex)
		if err != nil {
			return err
		}
		s.credCipher = c
		return nil
	}
}

// Open 建立连接池并 Ping 验证连通性。
func Open(ctx context.Context, dsn string, opts ...Option) (*Store, error) {
	if !strings.Contains(dsn, "parseTime=true") {
		return nil, errors.New("DSN 必须包含 parseTime=true（时间字段需要）")
	}
	if !strings.Contains(dsn, "loc=UTC") {
		return nil, errors.New("DSN 必须包含 loc=UTC（统一按 UTC 存储/读取）")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 MySQL 连接失败: %w", err)
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接 MySQL 失败: %w", err)
	}
	st := &Store{db: db}
	for _, opt := range opts {
		if err := opt(st); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return st, nil
}

// Close 释放数据库连接池。
func (s *Store) Close() error { return s.db.Close() }

// ---- 通用辅助 ----

// newID 生成带前缀的稳定字符串 id（16 字节随机十六进制）。
func newID(prefix string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return prefix + "-" + hex.EncodeToString(buf)
}

// marshalJSON 序列化任意结构为 JSON 字符串（空值返回空串）。
func marshalJSON(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalJSON 把 JSON 文本反序列化到 out；空文本置 out 为零值并按 out 类型留空。
func unmarshalJSON(text string, out any) error {
	if text == "" {
		switch o := out.(type) {
		case *map[string]any:
			*o = nil
		case *[]string:
			*o = nil
		}
		return nil
	}
	return json.Unmarshal([]byte(text), out)
}

// nowOr 返回给定时间，零值时回退到当前 UTC 时间。
func nowOr(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC()
}

// isNoRows 判定是否为无记录错误。
func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// notExistError 把 ErrNoRows 翻译为与其他存储一致的存在性错误。
func notExistError(err error, kind, id string) error {
	if isNoRows(err) {
		return fmt.Errorf("%s %q 不存在", kind, id)
	}
	return err
}

// ---- ServerStore ----
//
// Server 只持久化逻辑字段（name/description/enabled/聚合 health_status）；
// Endpoint/Transport 由 server_instances 表承载（见 instances.go）。servers 表
// 遗留的 endpoint/transport/version 列已改可空且不再读写（0008 起）。

// CreateServer 新增 Server；id 为空时自动生成。
func (s *Store) CreateServer(ctx context.Context, server *model.Server) error {
	if server.ID == "" {
		server.ID = newID("srv")
	}
	now := nowOr(server.CreatedAt)
	server.CreatedAt = now
	server.UpdatedAt = nowOr(server.UpdatedAt)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO servers (id, name, description, enabled, health_status, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?)`,
		server.ID, server.Name, nullIfEmpty(server.Description), server.Enabled, server.HealthStatus,
		fmtTimeUTC(server.CreatedAt), fmtTimeUTC(server.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert servers: %w", err)
	}
	return nil
}

// GetServer 按 id 读取 Server。
func (s *Store) GetServer(ctx context.Context, id string) (*model.Server, error) {
	var server model.Server
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(description,''), enabled, health_status, created_at, updated_at
		 FROM servers WHERE id = ?`, id,
	).Scan(&server.ID, &server.Name, &server.Description,
		&server.Enabled, &server.HealthStatus, &server.CreatedAt, &server.UpdatedAt)
	if err != nil {
		return nil, notExistError(err, "server", id)
	}
	return &server, nil
}

// ListServers 返回全部 Server（按 id 排序，保证输出稳定）。
func (s *Store) ListServers(ctx context.Context) ([]model.Server, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(description,''), enabled, health_status, created_at, updated_at
		 FROM servers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.Server, 0)
	for rows.Next() {
		var server model.Server
		if err := rows.Scan(&server.ID, &server.Name, &server.Description,
			&server.Enabled, &server.HealthStatus, &server.CreatedAt, &server.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, server)
	}
	return out, rows.Err()
}

// UpdateServer 覆盖更新 Server 的逻辑字段（不写 endpoint/transport/version）。
func (s *Store) UpdateServer(ctx context.Context, server *model.Server) error {
	server.UpdatedAt = nowOr(server.UpdatedAt)
	res, err := s.db.ExecContext(ctx,
		`UPDATE servers
		 SET name=?, description=?, enabled=?, health_status=?, updated_at=?
		 WHERE id=?`,
		server.Name, nullIfEmpty(server.Description), server.Enabled, server.HealthStatus,
		fmtTimeUTC(server.UpdatedAt), server.ID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("server %q 不存在", server.ID)
	}
	return nil
}

// DeleteServer 删除 Server（外键 ON DELETE CASCADE 级联清理子表）。
func (s *Store) DeleteServer(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM servers WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("server %q 不存在", id)
	}
	return nil
}

// nullIfEmpty 把空串转为 NULL，便于数据库可空列存 NULL 而非空串。
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// fmtTimeUTC 输出 MySQL DATETIME(3) 的 UTC 字面量，保证时区确定。
func fmtTimeUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.000")
}
