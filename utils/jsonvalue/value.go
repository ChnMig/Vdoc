// Package jsonvalue 保留契约数字精度，统一 JSON 与 YAML 的数值表示。
package jsonvalue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func Decode(data []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected a single JSON value")
	}
	return nil
}

// Number 不经浮点转换；指数形式只操作十进制字符串，不按指数分配内存。
func Number(raw string) (json.Number, error) { return canonicalNumber(raw, 4096) }

func canonicalNumber(raw string, maxLength int) (json.Number, error) {
	if len(raw) > maxLength {
		return "", fmt.Errorf("number exceeds %d digit limit", maxLength)
	}
	if !json.Valid([]byte(raw)) || len(raw) == 0 || !strings.ContainsAny(raw[:1], "-0123456789") {
		return "", fmt.Errorf("invalid number %q", raw)
	}
	negative := strings.HasPrefix(raw, "-")
	raw = strings.TrimPrefix(raw, "-")
	exponent := int64(0)
	if i := strings.IndexAny(raw, "eE"); i >= 0 {
		var err error
		exponent, err = strconv.ParseInt(raw[i+1:], 10, 64)
		if err != nil || exponent < -1000000 || exponent > 1000000 {
			return "", fmt.Errorf("number exponent exceeds supported range")
		}
		raw = raw[:i]
	}
	if i := strings.IndexByte(raw, '.'); i >= 0 {
		exponent -= int64(len(raw) - i - 1)
		raw = raw[:i] + raw[i+1:]
	}
	raw = strings.TrimLeft(raw, "0")
	if raw == "" {
		return json.Number("0"), nil
	}
	trimmed := strings.TrimRight(raw, "0")
	exponent += int64(len(raw) - len(trimmed))
	raw = trimmed
	sign := ""
	if negative {
		sign = "-"
	}
	point := int64(len(raw)) + exponent
	// JSONB 以 PostgreSQL numeric 保存数字，提前拒绝无法无损持久化的值。
	if point > 131072 || exponent < -16383 {
		return "", fmt.Errorf("number exceeds JSONB numeric range (131072 integer digits, 16383 fractional digits)")
	}
	if exponent >= 0 && point <= 32 {
		return json.Number(sign + raw + strings.Repeat("0", int(exponent))), nil
	}
	if exponent < 0 && point > 0 {
		return json.Number(sign + raw[:point] + "." + raw[point:]), nil
	}
	if point <= 0 && point >= -6 {
		return json.Number(sign + "0." + strings.Repeat("0", int(-point)) + raw), nil
	}
	mantissa := raw[:1]
	if len(raw) > 1 {
		mantissa += "." + raw[1:]
	}
	return json.Number(sign + mantissa + "e" + strconv.FormatInt(point-1, 10)), nil
}

func Normalize(value any) any {
	switch v := value.(type) {
	case json.Number:
		// JSONB 读取可能将指数形式展开为完整数字；内部规范化仍需恢复相同身份。
		if number, err := canonicalNumber(string(v), 150000); err == nil {
			return number
		}
		return v
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = Normalize(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = Normalize(item)
		}
		return out
	default:
		return value
	}
}
