package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TimeLayout 是全部时间字段对外 JSON 的统一格式：空格分隔的 UTC 秒级串
// （如 "2026-09-07 14:32:05"），不带时区后缀，控制台/客户端可直接可读。
const TimeLayout = "2006-01-02 15:04:05"

// dbTimeLayout 是落库到 MySQL DATETIME(3) 的字面量格式，与 storage/mysql
// 的 fmtTimeUTC 保持同构，保证存储与 API 两侧时间均为 UTC 秒/毫秒级。
const dbTimeLayout = "2006-01-02 15:04:05.000"

// Time 是领域内统一的时间类型：嵌入 time.Time 从而继承全部时间方法
// （Equal/Before/After/IsZero/Format/Unix…），但自定义 JSON 与数据库的
// 双向序列化，让"所有时间字段"出口格式一致。
//
// 约定：内表示与对外 JSON 均为 UTC。MarshalJSON 输出 TimeLayout 秒级串，
// Value 输出 dbTimeLayout（DATETIME(3)），Scan/UnmarshalJSON 兼容 RFC3339、
// 空格分隔与 DATETIME 文本并归一为 UTC。零值序列化/落库为空，避免比如
// 未做过保存的运行期配置返回 "0001-01-01 00:00:00"。
type Time struct {
	time.Time
}

// Now 返回当前 UTC 时间，作为各构造点的统一入口。
func Now() Time { return T(time.Now()) }

// T 把任意 time.Time 归一为 UTC 的 Time；零值原样返回（保持 IsZero 语义）。
func T(t time.Time) Time { return Time{t.UTC()} }

// MarshalJSON 输出 "2006-01-02 15:04:05"（UTC，秒级）；零值输出空串。
func (t Time) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(t.UTC().Format(TimeLayout))
}

// UnmarshalJSON 兼容 TimeLayout / RFC3339(RFC3339Nano) / DATETIME 文本三种
// 来源，统一归一为 UTC；空串或 null 置零值。
func (t *Time) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		t.Time = time.Time{}
		return nil
	}
	v, err := parseTimeText(s)
	if err != nil {
		return fmt.Errorf("无法解析时间字段 %q（支持 %s 与 RFC3339）: %w", s, TimeLayout, err)
	}
	t.Time = v
	return nil
}

// Value 实现 driver.Valuer：落库为 UTF-8 DATETIME(3) 字面量（UTC）；
// 零值落 NULL（进入的实体字段构造时均已置当前时间，不会触发 NOT NULL 冲突）。
func (t Time) Value() (driver.Value, error) {
	if t.IsZero() {
		return nil, nil
	}
	return t.UTC().Format(dbTimeLayout), nil
}

// Scan 实现 sql.Scanner：兼容 MySQL parseTime 返回的 time.Time，与未开启
// parseTime 时的 DATETIME 文本两种来源，统一归一为 UTC。
func (t *Time) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		t.Time = time.Time{}
	case time.Time:
		t.Time = v.UTC()
	case []byte:
		return t.scanText(string(v))
	case string:
		return t.scanText(v)
	default:
		return fmt.Errorf("不支持的数据库时间类型 %T", src)
	}
	return nil
}

// scanText 按 DATETIME/统一格式/RFC3339 依次尝试解析数据库时间文本。
func (t *Time) scanText(s string) error {
	v, err := parseTimeText(s)
	if err != nil {
		return fmt.Errorf("无法解析数据库时间 %q: %w", s, err)
	}
	t.Time = v
	return nil
}

// parseTimeText 依次按统一格式、DATETIME(3)、RFC3339Nano、RFC3339 解析，
// 统一归一为 UTC。
func parseTimeText(s string) (time.Time, error) {
	for _, layout := range []string{TimeLayout, dbTimeLayout, time.RFC3339Nano, time.RFC3339} {
		if v, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return v.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("不匹配任何支持的时间格式")
}