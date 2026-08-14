package model

import (
	"encoding/json"
	"math"
)

// OptFloat 可空的浮点值：Valid 为 false 表示数据缺失。
// 全仓气象要素统一用它能区分『0』与『无数据』。
type OptFloat struct {
	Valid bool
	V     float64
}

// Num 构造一个有效浮点值。
func Num(v float64) OptFloat {
	return OptFloat{Valid: true, V: v}
}

// Missing 构造一个无效（缺失）浮点值。
func Missing() OptFloat {
	return OptFloat{}
}

// NumOrMissing 有效数转 OptFloat，NaN/Inf 归为缺失。
func NumOrMissing(v float64) OptFloat {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return Missing()
	}
	return Num(v)
}

// IsZero 值是否缺失（Valid 为 false）。
func (o OptFloat) IsZero() bool { return !o.Valid }

// Or 取值，缺失时返回 fallback。
func (o OptFloat) Or(fallback float64) float64 {
	if o.Valid {
		return o.V
	}
	return fallback
}

// Must 取值，缺失时 panic——仅在调用方已确认 Valid 时使用。
func (o OptFloat) Must() float64 {
	if !o.Valid {
		panic("model.OptFloat.Must() called on invalid value")
	}
	return o.V
}

// GE 值是否有效且 ≥ threshold。
func (o OptFloat) GE(threshold float64) bool {
	return o.Valid && o.V >= threshold
}

// LT 值是否有效且 < threshold。
func (o OptFloat) LT(threshold float64) bool {
	return o.Valid && o.V < threshold
}

// UnmarshalJSON 解析 JSON 数字；空、null、空串或 NaN/Inf 一律视为缺失。
func (o *OptFloat) UnmarshalJSON(data []byte) error {
	s := string(data)
	if len(data) == 0 || s == "null" || s == `""` {
		o.Valid = false
		o.V = 0
		return nil
	}
	var v float64
	if err := json.Unmarshal(data, &v); err != nil {
		o.Valid = false
		o.V = 0
		return err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		o.Valid = false
		o.V = 0
		return nil
	}
	o.Valid = true
	o.V = v
	return nil
}

// MarshalJSON 有效值输出数字，缺失输出 null。
func (o OptFloat) MarshalJSON() ([]byte, error) {
	if !o.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(o.V)
}

// Sub 两值相减，任一缺失则结果缺失。
func Sub(a, b OptFloat) OptFloat {
	if !a.Valid || !b.Valid {
		return Missing()
	}
	return Num(a.V - b.V)
}

// Scale 按系数缩放，缺失则结果缺失。
func Scale(o OptFloat, k float64) OptFloat {
	if !o.Valid {
		return Missing()
	}
	return Num(o.V * k)
}

// NullString 可空的字符串值，用于 Relation 等可能缺失的文本字段。
type NullString struct {
	Valid bool
	V     string
}

// Str 构造一个有效字符串值。
func Str(s string) NullString { return NullString{Valid: true, V: s} }

// NullStr 构造一个缺失字符串值。
func NullStr() NullString { return NullString{} }

// String 取值，缺失时返回空串。
func (n NullString) String() string {
	if !n.Valid {
		return ""
	}
	return n.V
}

// MarshalJSON 有效值输出字符串，缺失输出 null。
func (n NullString) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.V)
}

// UnmarshalJSON 解析 JSON 字符串，null 视为缺失。
func (n *NullString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		n.Valid = false
		n.V = ""
		return nil
	}
	if err := json.Unmarshal(data, &n.V); err != nil {
		return err
	}
	n.Valid = true
	return nil
}
