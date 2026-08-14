package model

import (
	"math"
	"strconv"
	"strings"
)

// Round 把浮点四舍五入到指定小数位（digits），NaN/Inf 原样返回。
func Round(v float64, digits int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	s := strconv.FormatFloat(v, 'f', digits, 64)
	r, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return v
	}
	return r
}

// RoundPy 与 Python round 语义对齐的四舍五入：digits==0 且结果为 0 时返回 0，
// 吞掉 -0，避免与 Python 版数值产生分歧。
func RoundPy(v float64, digits int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	s := strconv.FormatFloat(v, 'f', digits, 64)
	r, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return v
	}
	if digits == 0 && r == 0 {
		return 0
	}
	return r
}

// RoundToInt 四舍五入到最近整数，NaN/Inf 视为 0。
func RoundToInt(v float64) int {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return int(Round(v, 0))
}

// RoundOpt 对可能为空的 OptFloat 先取值再四舍五入，无效值返回 Missing()。
func RoundOpt(o OptFloat, digits int) OptFloat {
	if !o.Valid {
		return Missing()
	}
	return Num(RoundPy(o.V, digits))
}

// FormatG 用 %g 格式输出浮点（自动去多余小数/指数）。
func FormatG(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// FormatPyFloat 输出保证含小数点，使 Python/JSON 解析为浮点而非整数。
func FormatPyFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// FormatPyBool 返回 Python 风格布尔字面量 "True"/"False"。
func FormatPyBool(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

// FormatFixed 按固定小数位格式化浮点（不足补零）。
func FormatFixed(v float64, digits int) string {
	return strconv.FormatFloat(v, 'f', digits, 64)
}
