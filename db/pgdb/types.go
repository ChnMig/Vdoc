package pgdb

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type JSONB []byte

func NewJSONB(value any, fallback string) JSONB {
	if value == nil {
		return JSONB(fallback)
	}
	raw, err := json.Marshal(value)
	if err != nil || string(raw) == "null" {
		return JSONB(fallback)
	}
	return JSONB(raw)
}

func (j JSONB) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return string(j), nil
}

func (j *JSONB) Scan(value any) error {
	if value == nil {
		*j = nil
		return nil
	}
	switch typed := value.(type) {
	case []byte:
		*j = append((*j)[:0], typed...)
	case string:
		*j = append((*j)[:0], typed...)
	default:
		return fmt.Errorf("scan jsonb: unsupported %T", value)
	}
	return nil
}

func (j JSONB) Interface() any {
	if len(j) == 0 || string(j) == "null" {
		return nil
	}
	var out any
	if err := json.Unmarshal(j, &out); err != nil {
		return nil
	}
	return out
}

type StringArray []string

func (a StringArray) Value() (driver.Value, error) {
	parts := make([]string, 0, len(a))
	for _, value := range a {
		escaped := strings.ReplaceAll(value, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		parts = append(parts, `"`+escaped+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func (a *StringArray) Scan(value any) error {
	parsed, err := parseArray(value)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

type SmallintArray []int

func (a SmallintArray) Value() (driver.Value, error) {
	parts := make([]string, 0, len(a))
	for _, value := range a {
		parts = append(parts, strconv.Itoa(value))
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func (a *SmallintArray) Scan(value any) error {
	parsed, err := parseArray(value)
	if err != nil {
		return err
	}
	out := make([]int, 0, len(parsed))
	for _, item := range parsed {
		if item == "" {
			continue
		}
		value, err := strconv.Atoi(item)
		if err != nil {
			return err
		}
		out = append(out, value)
	}
	*a = out
	return nil
}

func parseArray(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	var raw string
	switch typed := value.(type) {
	case []byte:
		raw = string(typed)
	case string:
		raw = typed
	default:
		return nil, fmt.Errorf("scan array: unsupported %T", value)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "[") {
		var out []string
		if err := json.Unmarshal([]byte(raw), &out); err == nil {
			return out, nil
		}
		var numbers []int
		if err := json.Unmarshal([]byte(raw), &numbers); err == nil {
			out = make([]string, 0, len(numbers))
			for _, number := range numbers {
				out = append(out, strconv.Itoa(number))
			}
			return out, nil
		}
	}
	trimmed := strings.TrimPrefix(strings.TrimSuffix(raw, "}"), "{")
	if trimmed == "" {
		return nil, nil
	}
	parts := strings.Split(trimmed, ",")
	for i, part := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(part), `"`)
	}
	return parts, nil
}
