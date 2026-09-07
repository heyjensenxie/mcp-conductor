package mysql

import (
	"context"
	"fmt"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// ---- InstanceStore（MySQL 实现）----
//
// server_instances 是 Server 具体上游实例的载体（endpoint/transport/健康/stdio
// 参数），通过 server_id 外键挂到 servers；Server 删除由 FK ON DELETE CASCADE
// 级联清理。args 为 stdio 启动参数（JSON 数组 TEXT），https/sse 实例为空。

// instanceColumns 与 scanInstance 对应的投影列。args 可空，SELECT 用 COALESCE
// 归一为空串后由 scanInstance 反序列化。
const instanceColumns = `id, server_id, endpoint, transport, COALESCE(args,''), enabled, health_status, created_at, updated_at`

// scanInstance 由一行查询结果扫描实例；args 文本反序列化为参数字符串数组。
func scanInstance(scan func(dest ...any) error) (model.Instance, error) {
	var inst model.Instance
	var argsJSON string
	err := scan(&inst.ID, &inst.ServerID, &inst.Endpoint, &inst.Transport, &argsJSON,
		&inst.Enabled, &inst.HealthStatus, &inst.CreatedAt, &inst.UpdatedAt)
	if err != nil {
		return inst, err
	}
	if err := unmarshalJSON(argsJSON, &inst.Args); err != nil {
		return inst, fmt.Errorf("解析实例 args 失败: %w", err)
	}
	return inst, nil
}

// CreateInstance 新增实例；id 为空时自动生成。
func (s *Store) CreateInstance(ctx context.Context, instance *model.Instance) error {
	if instance.ID == "" {
		instance.ID = newID("inst")
	}
	now := nowOr(instance.CreatedAt)
	instance.CreatedAt = now
	instance.UpdatedAt = nowOr(instance.UpdatedAt)
	argsJSON, err := marshalJSON(instance.Args)
	if err != nil {
		return fmt.Errorf("marshal instance args: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO server_instances (id, server_id, endpoint, transport, args, enabled, health_status, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		instance.ID, instance.ServerID, instance.Endpoint, instance.Transport, argsJSON,
		instance.Enabled, instance.HealthStatus, fmtTimeUTC(instance.CreatedAt), fmtTimeUTC(instance.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert server_instances: %w", err)
	}
	return nil
}

// GetInstance 按 id 读取实例。
func (s *Store) GetInstance(ctx context.Context, id string) (*model.Instance, error) {
	inst, err := scanInstance(s.db.QueryRowContext(ctx,
		`SELECT `+instanceColumns+` FROM server_instances WHERE id = ?`, id).Scan)
	if err != nil {
		return nil, notExistError(err, "instance", id)
	}
	return &inst, nil
}

// ListInstances 返回全部实例（按 (created_at, id) 升序，首条即"主实例"）。
func (s *Store) ListInstances(ctx context.Context) ([]model.Instance, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+instanceColumns+` FROM server_instances ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectInstances(rows)
}

// ListInstancesByServer 返回指定 Server 的实例（按 (created_at, id) 升序）。
func (s *Store) ListInstancesByServer(ctx context.Context, serverID string) ([]model.Instance, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+instanceColumns+` FROM server_instances WHERE server_id = ? ORDER BY created_at, id`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectInstances(rows)
}

// collectInstances 遍历查询结果并组装实例列表。
func collectInstances(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]model.Instance, error) {
	out := make([]model.Instance, 0)
	for rows.Next() {
		inst, err := scanInstance(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

// UpdateInstance 覆盖更新实例（server_id 视为归属不变，不在本处迁移）。
func (s *Store) UpdateInstance(ctx context.Context, instance *model.Instance) error {
	instance.UpdatedAt = nowOr(instance.UpdatedAt)
	argsJSON, err := marshalJSON(instance.Args)
	if err != nil {
		return fmt.Errorf("marshal instance args: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE server_instances
		 SET endpoint=?, transport=?, args=?, enabled=?, health_status=?, updated_at=?
		 WHERE id=?`,
		instance.Endpoint, instance.Transport, argsJSON, instance.Enabled, instance.HealthStatus,
		fmtTimeUTC(instance.UpdatedAt), instance.ID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("instance %q 不存在", instance.ID)
	}
	return nil
}

// DeleteInstance 删除单个实例。
func (s *Store) DeleteInstance(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM server_instances WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("instance %q 不存在", id)
	}
	return nil
}

// DeleteInstancesByServer 删除指定 Server 的全部实例（供 registry 显式级联；
// 不存在时视为成功，与 memory 行为一致）。
func (s *Store) DeleteInstancesByServer(ctx context.Context, serverID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM server_instances WHERE server_id=?`, serverID)
	return err
}
