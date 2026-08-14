package model

import (
	"encoding/json"
	"math"
	"testing"
)

func TestRoundBankers(t *testing.T) {
	cases := []struct {
		name   string
		v      float64
		digits int
		want   float64
	}{
		{"half-to-even/0.5→0", 0.5, 0, 0},
		{"half-to-even/1.5→2", 1.5, 0, 2},
		{"half-to-even/2.5→2", 2.5, 0, 2},
		{"half-to-even/3.5→4", 3.5, 0, 4},
		{"half-to-even/-2.5→-2", -2.5, 0, -2},

		{"binary-repr/2.675→2.67", 2.675, 2, 2.67},

		{"binary-repr/0.135→0.14", 0.135, 2, 0.14},
		{"one-digit/21.04→21", 21.04, 1, 21.0},
		{"one-digit/-0.05→-0.1", -0.05, 1, -0.1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Round(c.v, c.digits); got != c.want {
				t.Fatalf("Round(%v, %d) = %v, want %v", c.v, c.digits, got, c.want)
			}
		})
	}
}

func TestRoundToInt(t *testing.T) {
	cases := []struct {
		v    float64
		want int
	}{{0.5, 0}, {1.5, 2}, {2.5, 2}, {-1.5, -2}, {1489.6, 1490}}
	for _, c := range cases {
		if got := RoundToInt(c.v); got != c.want {
			t.Fatalf("RoundToInt(%v) = %d, want %d", c.v, got, c.want)
		}
	}
}

func TestRoundOptKeepsMissing(t *testing.T) {
	if got := RoundOpt(Missing(), 1); got.Valid {
		t.Fatalf("RoundOpt(Missing) 返回了有效值 %v，缺测被静默转成了数值", got)
	}
	got := RoundOpt(Num(2.675), 2)
	if !got.Valid || got.V != 2.67 {
		t.Fatalf("RoundOpt(2.675, 2) = %+v, want {true 2.67}", got)
	}
}

func TestFormatPyFloat(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{21.0, "21.0"},
		{21.5, "21.5"},
		{-3.0, "-3.0"},
		{0.0, "0.0"},
		{1489.9, "1489.9"},
	}
	for _, c := range cases {
		if got := FormatPyFloat(c.v); got != c.want {
			t.Fatalf("FormatPyFloat(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestFormatG(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{1489.9, "1489.9"},
		{1557.0, "1557"},
		{0.0, "0"},
		{8.0, "8"},
	}
	for _, c := range cases {
		if got := FormatG(c.v); got != c.want {
			t.Fatalf("FormatG(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestFormatPyBool(t *testing.T) {
	if FormatPyBool(true) != "True" || FormatPyBool(false) != "False" {
		t.Fatal("FormatPyBool 必须输出 Python 风格的 True/False")
	}
}

func TestWorse(t *testing.T) {
	if got := Worse(RATING_OK, RATING_BAD); got != RATING_BAD {
		t.Fatalf("Worse(OK, BAD) = %q", got)
	}
	if got := Worse(RATING_BAD, RATING_NODATA); got != RATING_NODATA {
		t.Fatalf("Worse(BAD, NODATA) = %q，无数据必须最劣", got)
	}
}

func TestNullStringJSON(t *testing.T) {
	data, err := json.Marshal(NullStr())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "null" {
		t.Fatalf("NullStr() 序列化为 %s，want null", data)
	}
	data, err = json.Marshal(Str("CLEAR"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"CLEAR"` {
		t.Fatalf("Str(CLEAR) 序列化为 %s", data)
	}
}

func TestRoundPy(t *testing.T) {

	zeroCases := []struct {
		name string
		v    float64
	}{
		{"neg-0.4→0", -0.4},
		{"neg-0.5→0", -0.5},
		{"neg-0.40→0", -0.40},
		{"pos-0.4→0", 0.4},
	}
	for _, c := range zeroCases {
		if got := RoundPy(c.v, 0); got != 0 || math.Signbit(got) {
			t.Fatalf("RoundPy(%v, 0) = %v，signbit=%v；期望正零（值=0 且符号位 false）",
				c.v, got, math.Signbit(got))
		}
	}

	if got := RoundPy(-0.6, 0); got != -1 {
		t.Fatalf("RoundPy(-0.6, 0) = %v，want -1（普通舍入语义不变）", got)
	}

	if got := RoundPy(-0.04, 1); !math.Signbit(got) {
		t.Fatalf("RoundPy(-0.04, 1) = %v，signbit=%v；digits>0 必须保留负零",
			got, math.Signbit(got))
	}
	if got := RoundPy(-0.004, 2); !math.Signbit(got) {
		t.Fatalf("RoundPy(-0.004, 2) = %v，signbit=%v；digits>0 必须保留负零",
			got, math.Signbit(got))
	}

	if s := FormatG(RoundPy(-0.4, 0)); s == "-0" {
		t.Fatalf(`FormatG(RoundPy(-0.4, 0)) = %q，不应带负号`, s)
	}
}
