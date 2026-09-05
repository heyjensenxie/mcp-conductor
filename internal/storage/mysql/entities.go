package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/xmj128/mcp-conductor/internal/model"
)

// ---- ToolStore ----

const toolColumns = `id, server_id, original_name, gateway_name, COALESCE(description,''), COALESCE(input_schema,''), COALESCE(risk_level,''), enabled, created_at, updated_at`

func scanTool(scan func(dest ...any) error) (model.Tool, error) {
	var tool model.Tool
	var inputSchema string
	err := scan(&tool.ID, &tool.ServerID, &tool.OriginalName, &tool.GatewayName,
		&tool.Description, &inputSchema, &tool.RiskLevel, &tool.Enabled,
		&tool.CreatedAt, &tool.UpdatedAt)
	if err != nil {
		return model.Tool{}, err
	}
	if err := unmarshalJSON(inputSchema, &tool.InputSchema); err != nil {
		return model.Tool{}, fmt.Errorf("解析 tool %q input_schema 失败: %w", tool.ID, err)
	}
	return tool, nil
}

// UpsertTool 以 gateway_name 为业务键写入/覆盖 Tool（MySQL 5.7 的
// INSERT ... ON DUPLICATE KEY UPDATE 保证幂等）。
// 已存在时复用既有 id（与内存实现语义一致），并刷新其余字段。
func (s *Store) UpsertTool(ctx context.Context, tool *model.Tool) error {
	schema, err := marshalJSON(tool.InputSchema)
	if err != nil {
		return err
	}
	// ON DUPLICATE KEY UPDATE 不会改动主键：先查出既有 id 复用，
	// 保证对外 tool.ID 稳定。
	var existingID string
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM tools WHERE gateway_name = ? LIMIT 1`, tool.GatewayName).Scan(&existingID); err == nil {
		tool.ID = existingID
	} else if tool.ID == "" {
		tool.ID = newID("tool")
	}
	now := nowOr(tool.CreatedAt)
	tool.CreatedAt = now
	tool.UpdatedAt = nowOr(tool.UpdatedAt)

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO tools
		   (id, server_id, original_name, gateway_name, description, input_schema, risk_level, enabled, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE
		   server_id = VALUES(server_id), original_name = VALUES(original_name),
		   description = VALUES(description), input_schema = VALUES(input_schema),
		   risk_level = VALUES(risk_level), enabled = VALUES(enabled), updated_at = VALUES(updated_at)`,
		tool.ID, tool.ServerID, tool.OriginalName, tool.GatewayName,
		nullIfEmpty(tool.Description), schema, nullIfEmpty(tool.RiskLevel),
		tool.Enabled, fmtTimeUTC(tool.CreatedAt), fmtTimeUTC(tool.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert tools: %w", err)
	}
	return nil
}

// GetTool 按 id 读取 Tool。
func (s *Store) GetTool(ctx context.Context, id string) (*model.Tool, error) {
	tool, err := scanTool(func(dest ...any) error {
		return s.db.QueryRowContext(ctx,
			"SELECT "+toolColumns+" FROM tools WHERE id = ?", id).Scan(dest...)
	})
	if err != nil {
		return nil, notExistError(err, "tool", id)
	}
	return &tool, nil
}

// GetToolByGatewayName 按对外门面名读取 Tool。
func (s *Store) GetToolByGatewayName(ctx context.Context, gatewayName string) (*model.Tool, error) {
	tool, err := scanTool(func(dest ...any) error {
		return s.db.QueryRowContext(ctx,
			"SELECT "+toolColumns+" FROM tools WHERE gateway_name = ?", gatewayName).Scan(dest...)
	})
	if err != nil {
		return nil, notExistError(err, "tool", gatewayName)
	}
	return &tool, nil
}

// ListTools 返回全部 Tool。
func (s *Store) ListTools(ctx context.Context) ([]model.Tool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+toolColumns+" FROM tools ORDER BY gateway_name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.Tool, 0)
	for rows.Next() {
		tool, err := scanTool(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, tool)
	}
	return out, rows.Err()
}

// ListToolsByServer 返回指定 Server 的 Tool 列表。
func (s *Store) ListToolsByServer(ctx context.Context, serverID string) ([]model.Tool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+toolColumns+" FROM tools WHERE server_id = ? ORDER BY gateway_name", serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.Tool, 0)
	for rows.Next() {
		tool, err := scanTool(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, tool)
	}
	return out, rows.Err()
}

// DeleteToolsByServer 删除指定 Server 的 Tool。
func (s *Store) DeleteToolsByServer(ctx context.Context, serverID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tools WHERE server_id = ?`, serverID)
	return err
}

// ---- RouteStore ----

// CreateRoute 新增路由（tool_names 以 JSON 文本保存）。
func (s *Store) CreateRoute(ctx context.Context, route *model.Route) error {
	names, err := marshalJSON(route.ToolNames)
	if err != nil {
		return err
	}
	if route.ID == "" {
		route.ID = newID("route")
	}
	now := nowOr(route.CreatedAt)
	route.CreatedAt = now
	route.UpdatedAt = nowOr(route.UpdatedAt)

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO routes (id, name, server_id, tool_names, enabled, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
		route.ID, route.Name, route.ServerID, names, route.Enabled,
		fmtTimeUTC(route.CreatedAt), fmtTimeUTC(route.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert routes: %w", err)
	}
	return nil
}

// ListRoutes 返回全部路由。
func (s *Store) ListRoutes(ctx context.Context) ([]model.Route, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, server_id, COALESCE(tool_names,''), enabled, created_at, updated_at FROM routes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.Route, 0)
	for rows.Next() {
		var route model.Route
		var names string
		if err := rows.Scan(&route.ID, &route.Name, &route.ServerID, &names, &route.Enabled,
			&route.CreatedAt, &route.UpdatedAt); err != nil {
			return nil, err
		}
		if err := unmarshalJSON(names, &route.ToolNames); err != nil {
			return nil, err
		}
		out = append(out, route)
	}
	return out, rows.Err()
}

// ---- PolicyStore ----

// CreatePolicy 在事务中写入策略与规范化后的规则。
func (s *Store) CreatePolicy(ctx context.Context, policy *model.Policy) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if policy.ID == "" {
		policy.ID = newID("pol")
	}
	now := nowOr(policy.CreatedAt)
	policy.CreatedAt = now
	policy.UpdatedAt = nowOr(policy.UpdatedAt)

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO policies (id, name, enabled, created_at, updated_at) VALUES (?,?,?,?,?)`,
		policy.ID, policy.Name, policy.Enabled, fmtTimeUTC(policy.CreatedAt), fmtTimeUTC(policy.UpdatedAt)); err != nil {
		return fmt.Errorf("insert policies: %w", err)
	}
	for _, rule := range policy.Rules {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO policy_rules (policy_id, subject, tool, effect) VALUES (?,?,?,?)`,
			policy.ID, rule.Subject, rule.Tool, rule.Effect); err != nil {
			return fmt.Errorf("insert policy_rules: %w", err)
		}
	}
	return tx.Commit()
}

// GetPolicy 读取策略及其规则。
func (s *Store) GetPolicy(ctx context.Context, id string) (*model.Policy, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.name, p.enabled, p.created_at, p.updated_at,
		        COALESCE(r.subject,''), COALESCE(r.tool,''), COALESCE(r.effect,'')
		 FROM policies p
		 LEFT JOIN policy_rules r ON r.policy_id = p.id
		 WHERE p.id = ? ORDER BY r.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	policy, err := scanPolicies(rows)
	if err != nil {
		return nil, err
	}
	if len(policy) == 0 {
		return nil, fmt.Errorf("policy %q 不存在", id)
	}
	return &policy[0], nil
}

// ListPolicies 返回全部启用/停用策略（含规则，单次 JOIN 查询）。
func (s *Store) ListPolicies(ctx context.Context) ([]model.Policy, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.name, p.enabled, p.created_at, p.updated_at,
		        COALESCE(r.subject,''), COALESCE(r.tool,''), COALESCE(r.effect,'')
		 FROM policies p
		 LEFT JOIN policy_rules r ON r.policy_id = p.id
		 ORDER BY p.id, r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanPolicies(rows)
}

// scanPolicies 把「策略 LEFT JOIN 规则」的行流聚合成带规则列表的策略。
func scanPolicies(rows *sql.Rows) ([]model.Policy, error) {
	out := make([]model.Policy, 0)
	index := make(map[string]int) // policyID -> out 下标
	for rows.Next() {
		var p model.Policy
		var subject, tool, effect string
		if err := rows.Scan(&p.ID, &p.Name, &p.Enabled, &p.CreatedAt, &p.UpdatedAt,
			&subject, &tool, &effect); err != nil {
			return nil, err
		}
		if i, ok := index[p.ID]; ok {
			if subject != "" { // 有规则行才追加
				out[i].Rules = append(out[i].Rules, model.PolicyRule{Subject: subject, Tool: tool, Effect: model.PolicyEffect(effect)})
			}
			continue
		}
		index[p.ID] = len(out)
		if subject != "" {
			p.Rules = []model.PolicyRule{{Subject: subject, Tool: tool, Effect: model.PolicyEffect(effect)}}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- CredentialStore ----

// CreateCredential 新增凭证：值为空时仅登记元数据；非空时加密落库
// （AES-256-GCM，需配置 credentials.encryption_key）。
func (s *Store) CreateCredential(ctx context.Context, credential *model.Credential) error {
	encrypted, err := encryptValue(s.credCipher, credential.Value)
	if err != nil {
		return err
	}
	credential.HasValue = credential.Value != ""
	if credential.ID == "" {
		credential.ID = newID("cred")
	}
	now := nowOr(credential.CreatedAt)
	credential.CreatedAt = now
	credential.UpdatedAt = nowOr(credential.UpdatedAt)

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO credentials (id, server_id, name, kind, header, encrypted_value, has_value, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		credential.ID, credential.ServerID, credential.Name, credential.Kind,
		nullIfEmpty(credential.Header), nullIfEmpty(encrypted), credential.HasValue,
		fmtTimeUTC(credential.CreatedAt), fmtTimeUTC(credential.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert credentials: %w", err)
	}
	return nil
}

// ListCredentialsByServer 返回指定 Server 的凭证元数据并解密值（供上游注入）。
func (s *Store) ListCredentialsByServer(ctx context.Context, serverID string) ([]model.Credential, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, server_id, name, kind, COALESCE(header,''), COALESCE(encrypted_value,''), has_value, created_at, updated_at
		 FROM credentials WHERE server_id = ? ORDER BY id`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.Credential, 0)
	for rows.Next() {
		var cred model.Credential
		var encrypted string
		if err := rows.Scan(&cred.ID, &cred.ServerID, &cred.Name, &cred.Kind, &cred.Header,
			&encrypted, &cred.HasValue, &cred.CreatedAt, &cred.UpdatedAt); err != nil {
			return nil, err
		}
		value, err := decryptValue(s.credCipher, encrypted)
		if err != nil {
			return nil, err
		}
		cred.Value = value
		out = append(out, cred)
	}
	return out, rows.Err()
}

// UpdateCredential 更新凭证元数据；Name/Kind/Header 空值表示不改动。
// Value 空值保留原密文与 HasValue；非空时重新加密落库并置 HasValue=1。
func (s *Store) UpdateCredential(ctx context.Context, credential *model.Credential) error {
	sets := []string{"updated_at = ?"}
	args := []any{fmtTimeUTC(time.Now().UTC())}
	if credential.Name != "" {
		sets = append(sets, "name = ?")
		args = append(args, credential.Name)
	}
	if credential.Kind != "" {
		sets = append(sets, "kind = ?")
		args = append(args, credential.Kind)
	}
	if credential.Header != "" {
		sets = append(sets, "header = ?")
		args = append(args, credential.Header)
	}
	if credential.Value != "" {
		encrypted, err := encryptValue(s.credCipher, credential.Value)
		if err != nil {
			return err
		}
		sets = append(sets, "encrypted_value = ?", "has_value = 1")
		args = append(args, encrypted)
	}
	args = append(args, credential.ID, credential.ServerID)

	res, err := s.db.ExecContext(ctx,
		"UPDATE credentials SET "+strings.Join(sets, ", ")+" WHERE id = ? AND server_id = ?", args...)
	if err != nil {
		return fmt.Errorf("update credentials: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("credential %q 不存在", credential.ID)
	}
	return nil
}

// DeleteCredential 删除单个凭证；不存在时返回存在性错误。
func (s *Store) DeleteCredential(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM credentials WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete credentials: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("credential %q 不存在", id)
	}
	return nil
}

// DeleteCredentialsByServer 删除指定 Server 的全部凭证（不存在时视为成功）。
func (s *Store) DeleteCredentialsByServer(ctx context.Context, serverID string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM credentials WHERE server_id = ?", serverID); err != nil {
		return fmt.Errorf("delete credentials by server: %w", err)
	}
	return nil
}

// ---- AccessKeyStore ----

const accessKeyColumns = `id, name, subject, enabled, COALESCE(key_hash,''), qps, burst, created_at, updated_at`

func scanAccessKey(scan func(dest ...any) error) (model.AccessKey, error) {
	var k model.AccessKey
	err := scan(&k.ID, &k.Name, &k.Subject, &k.Enabled, &k.KeyHash, &k.QPS, &k.Burst,
		&k.CreatedAt, &k.UpdatedAt)
	if err != nil {
		return model.AccessKey{}, err
	}
	return k, nil
}

// getAccessKey 按查询条件读取单个 key，并补齐其 grants。
func (s *Store) getAccessKey(ctx context.Context, where, arg string) (*model.AccessKey, error) {
	k, err := scanAccessKey(func(dest ...any) error {
		return s.db.QueryRowContext(ctx,
			"SELECT "+accessKeyColumns+" FROM access_keys WHERE "+where, arg).Scan(dest...)
	})
	if err != nil {
		return nil, notExistError(err, "access key", arg)
	}
	grants, err := s.loadGrants(ctx, k.ID)
	if err != nil {
		return nil, err
	}
	k.Grants = grants
	return &k, nil
}

// loadGrants 读取某 key 的全部白名单条目（含 headers / default_args JSON 文本）。
func (s *Store) loadGrants(ctx context.Context, keyID string) ([]model.ToolGrant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT gateway_name, COALESCE(headers,''), COALESCE(default_args,'')
		 FROM access_key_grants WHERE key_id = ? ORDER BY gateway_name`, keyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.ToolGrant, 0)
	for rows.Next() {
		var grant model.ToolGrant
		var headers, defaultArgs string
		if err := rows.Scan(&grant.GatewayName, &headers, &defaultArgs); err != nil {
			return nil, err
		}
		if err := unmarshalJSON(headers, &grant.Headers); err != nil {
			return nil, err
		}
		if err := unmarshalJSON(defaultArgs, &grant.DefaultArgs); err != nil {
			return nil, err
		}
		out = append(out, grant)
	}
	return out, rows.Err()
}

// allAccessKeys 供 List 一次性读取全部 key 及其 grants（两次查询后聚组）。
func (s *Store) allAccessKeys(ctx context.Context) ([]model.AccessKey, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+accessKeyColumns+" FROM access_keys ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.AccessKey, 0)
	index := make(map[string]int) // keyID -> out 下标
	for rows.Next() {
		k, err := scanAccessKey(rows.Scan)
		if err != nil {
			return nil, err
		}
		index[k.ID] = len(out)
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 一次读取全部 grants 并按 key 分组填充。
	grows, err := s.db.QueryContext(ctx,
		`SELECT key_id, gateway_name, COALESCE(headers,''), COALESCE(default_args,'')
		 FROM access_key_grants ORDER BY key_id, gateway_name`)
	if err != nil {
		return nil, err
	}
	defer grows.Close()
	for grows.Next() {
		var keyID, gatewayName, headers, defaultArgs string
		if err := grows.Scan(&keyID, &gatewayName, &headers, &defaultArgs); err != nil {
			return nil, err
		}
		i, ok := index[keyID]
		if !ok {
			continue
		}
		grant := model.ToolGrant{GatewayName: gatewayName}
		if err := unmarshalJSON(headers, &grant.Headers); err != nil {
			return nil, err
		}
		if err := unmarshalJSON(defaultArgs, &grant.DefaultArgs); err != nil {
			return nil, err
		}
		out[i].Grants = append(out[i].Grants, grant)
	}
	return out, grows.Err()
}

// replaceGrants 在事务中先清空再写入指定 key 的白名单条目。
func (s *Store) replaceGrants(ctx context.Context, tx *sql.Tx, keyID string, grants []model.ToolGrant) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_key_grants WHERE key_id = ?`, keyID); err != nil {
		return err
	}
	for _, grant := range grants {
		headers, err := marshalJSON(grant.Headers)
		if err != nil {
			return err
		}
		defaultArgs, err := marshalJSON(grant.DefaultArgs)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO access_key_grants (id, key_id, gateway_name, headers, default_args) VALUES (?,?,?,?,?)`,
			newID("grant"), keyID, grant.GatewayName, headers, defaultArgs); err != nil {
			return err
		}
	}
	return nil
}

// CreateAccessKey 新增 API Key（key + grants 一个事务写入）。
func (s *Store) CreateAccessKey(ctx context.Context, key *model.AccessKey) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if key.ID == "" {
		key.ID = newID("key")
	}
	now := nowOr(key.CreatedAt)
	key.CreatedAt = now
	key.UpdatedAt = nowOr(key.UpdatedAt)

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO access_keys (id, name, subject, enabled, key_hash, qps, burst, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		key.ID, key.Name, key.Subject, key.Enabled, key.KeyHash, key.QPS, key.Burst,
		fmtTimeUTC(key.CreatedAt), fmtTimeUTC(key.UpdatedAt)); err != nil {
		return fmt.Errorf("insert access_keys: %w", err)
	}
	if err := s.replaceGrants(ctx, tx, key.ID, key.Grants); err != nil {
		return err
	}
	return tx.Commit()
}

// GetAccessKey 按 id 读取 API Key（含 grants）。
func (s *Store) GetAccessKey(ctx context.Context, id string) (*model.AccessKey, error) {
	return s.getAccessKey(ctx, "id = ?", id)
}

// GetAccessKeyBySubject 按主体读取 API Key。
func (s *Store) GetAccessKeyBySubject(ctx context.Context, subject string) (*model.AccessKey, error) {
	return s.getAccessKey(ctx, "subject = ?", subject)
}

// GetAccessKeyByKeyHash 按密钥哈希读取 API Key（认证用）。
func (s *Store) GetAccessKeyByKeyHash(ctx context.Context, keyHash string) (*model.AccessKey, error) {
	return s.getAccessKey(ctx, "key_hash = ?", keyHash)
}

// ListAccessKeys 返回全部 API Key（含 grants）。
func (s *Store) ListAccessKeys(ctx context.Context) ([]model.AccessKey, error) {
	return s.allAccessKeys(ctx)
}

// UpdateAccessKey 覆盖 key 字段并整体替换 grants。
func (s *Store) UpdateAccessKey(ctx context.Context, key *model.AccessKey) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	key.UpdatedAt = nowOr(key.UpdatedAt)
	res, err := tx.ExecContext(ctx,
		`UPDATE access_keys SET name=?, subject=?, enabled=?, key_hash=?, qps=?, burst=?, updated_at=? WHERE id=?`,
		key.Name, key.Subject, key.Enabled, key.KeyHash, key.QPS, key.Burst,
		fmtTimeUTC(key.UpdatedAt), key.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("access key %q 不存在", key.ID)
	}
	if err := s.replaceGrants(ctx, tx, key.ID, key.Grants); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteAccessKey 删除 API Key（grants 由外键 ON DELETE CASCADE 清理）。
func (s *Store) DeleteAccessKey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM access_keys WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("access key %q 不存在", id)
	}
	return nil
}

// ---- TrafficStore ----

// AppendTraffic 追加一条调用采样。请求/追踪标识为可选，错误文本仅在失败时写入。
func (s *Store) AppendTraffic(ctx context.Context, sample model.TrafficSample) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO traffic_log (request_id, trace_id, server_id, tool, client, status, latency_ms, error, ts)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		sample.RequestID, nullIfEmpty(sample.TraceID), nullIfEmpty(sample.ServerID),
		sample.Tool, nullIfEmpty(sample.Client), sample.Status, sample.LatencyMS,
		nullIfEmpty(sample.Error), fmtTimeUTC(sample.Timestamp),
	)
	if err != nil {
		return fmt.Errorf("insert traffic_log: %w", err)
	}
	return nil
}

// RecentTraffic 返回最近 limit 条调用采样（按写入顺序倒序）。
func (s *Store) RecentTraffic(ctx context.Context, limit int) ([]model.TrafficSample, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT request_id, COALESCE(trace_id,''), COALESCE(server_id,''), tool, COALESCE(client,''),
		        status, latency_ms, COALESCE(error,''), ts
		 FROM traffic_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.TrafficSample, 0)
	for rows.Next() {
		var sample model.TrafficSample
		if err := rows.Scan(&sample.RequestID, &sample.TraceID, &sample.ServerID, &sample.Tool,
			&sample.Client, &sample.Status, &sample.LatencyMS, &sample.Error, &sample.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, sample)
	}
	return out, rows.Err()
}

// RecentTrafficByServer 返回指定 Server 最近 limit 条调用采样（按写入倒序）。
func (s *Store) RecentTrafficByServer(ctx context.Context, serverID string, limit int) ([]model.TrafficSample, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT request_id, COALESCE(trace_id,''), COALESCE(server_id,''), tool, COALESCE(client,''),
		        status, latency_ms, COALESCE(error,''), ts
		 FROM traffic_log WHERE server_id = ? ORDER BY id DESC LIMIT ?`, serverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.TrafficSample, 0)
	for rows.Next() {
		var sample model.TrafficSample
		if err := rows.Scan(&sample.RequestID, &sample.TraceID, &sample.ServerID, &sample.Tool,
			&sample.Client, &sample.Status, &sample.LatencyMS, &sample.Error, &sample.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, sample)
	}
	return out, rows.Err()
}
