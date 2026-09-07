package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 锁定 model.Time 的对外序列化契约：所有时间字段统一输出空格分隔的 UTC
// 秒级串（2006-01-02 15:04:05），无时区后缀、无纳秒；反向解析兼容
// RFC3339 / 空格分隔 / DATETIME 文本。

func TestTimeMarshalJSON_UTCSeconds(t *testing.T) {
	// 用高于秒的精度输入，验证统一截到秒并归一为 UTC（含时区偏移的本地时间）。
	local := time.Date(2026, 9, 7, 22, 32, 5, 987654321, time.FixedZone("CST", 8*60*60))
	raw, err := json.Marshal(T(local))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got, want := string(raw), `"2026-09-07 14:32:05"`; got != want {
		t.Fatalf("Marshal 输出 = %s，want %s", got, want)
	}
}

func TestTimeMarshalJSON_ZeroIsEmpty(t *testing.T) {
	raw, err := json.Marshal(Time{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got := string(raw); got != `""` {
		t.Fatalf("零值应序列化为空串，得到 %s", got)
	}
}

func TestTimeUnmarshalJSON_Formats(t *testing.T) {
	cases := []struct {
		in   string
		want string // 期望的 UTC "2006-01-02 15:04:05"
	}{
		{`"2026-09-07 14:32:05"`, "2026-09-07 14:32:05"},            // 统一格式
		{`"2026-09-07T14:32:05Z"`, "2026-09-07 14:32:05"},          // RFC3339
		{`"2026-09-07T14:32:05.123Z"`, "2026-09-07 14:32:05"},      // RFC3339 带毫秒（截秒）
		{`"2026-09-07 14:32:05.123"`, "2026-09-07 14:32:05"},       // DATETIME(3) 文本
		{`""`, ""},                                                 // 空串 → 零值
		{`"2026-09-07T22:32:05+08:00"`, "2026-09-07 14:32:05"},     // 带时区偏移归一 UTC
	}
	for _, tc := range cases {
		var t0 Time
		if err := json.Unmarshal([]byte(tc.in), &t0); err != nil {
			t.Fatalf("Unmarshal(%s): %v", tc.in, err)
		}
		var got string
		if t0.IsZero() {
			got = ""
		} else {
			got = t0.UTC().Format(TimeLayout)
		}
		if got != tc.want {
			t.Fatalf("Unmarshal(%s) → %q，want %q", tc.in, got, tc.want)
		}
	}
}

func TestTimeUnmarshalJSON_Invalid(t *testing.T) {
	var t0 Time
	if err := json.Unmarshal([]byte(`"not-a-time"`), &t0); err == nil {
		t.Fatal("非法时间串应报错")
	}
}

func TestTime_Value(t *testing.T) {
	raw, err := Now().Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	s, ok := raw.(string)
	if !ok {
		t.Fatalf("Value 应返回字符串，得到 %T", raw)
	}
	if _, err := time.Parse("2006-01-02 15:04:05.000", s); err != nil {
		t.Fatalf("Value 输出不满足 DATETIME(3) 格式 %q: %v", s, err)
	}
	// 零值落 NULL。
	if v, err := (Time{}).Value(); err != nil || v != nil {
		t.Fatalf("零值 Value 应为 nil，得到 %v / %v", v, err)
	}
}

func TestTime_Scan(t *testing.T) {
	cases := []struct {
		src  any
		want string
	}{
		{time.Date(2026, 9, 7, 14, 32, 5, 0, time.UTC), "2026-09-07 14:32:05"},
		{time.Date(2026, 9, 7, 22, 32, 5, 0, time.FixedZone("CST", 8*60*60)), "2026-09-07 14:32:05"},
		{[]byte("2026-09-07 14:32:05.000"), "2026-09-07 14:32:05"},
		{"2026-09-07T14:32:05Z", "2026-09-07 14:32:05"},
		{nil, ""},
	}
	for _, tc := range cases {
		var t0 Time
		if err := t0.Scan(tc.src); err != nil {
			t.Fatalf("Scan(%T %v): %v", tc.src, tc.src, err)
		}
		var got string
		if t0.IsZero() {
			got = ""
		} else {
			got = t0.UTC().Format(TimeLayout)
		}
		if got != tc.want {
			t.Fatalf("Scan(%v) → %q，want %q", tc.src, got, tc.want)
		}
	}
}

// TestAccessKeyTimeFields_JSONContract 验证文档模型 JSON 出口真正走统一格式：
// created_at/updated_at 都必须是空格分隔 UTC 串，不再输出 RFC3339。
func TestAccessKeyTimeFields_JSONContract(t *testing.T) {
	now := time.Date(2026, 9, 7, 14, 32, 5, 0, time.UTC)
	k := AccessKey{ID: "key-1", Subject: "a", CreatedAt: T(now), UpdatedAt: T(now)}
	raw, err := json.Marshal(k)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(raw)
	for _, want := range []string{`"created_at":"2026-09-07 14:32:05"`, `"updated_at":"2026-09-07 14:32:05"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("响应应包含 %s，得到 %s", want, s)
		}
	}
}