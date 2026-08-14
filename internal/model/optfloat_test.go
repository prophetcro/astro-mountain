package model

import (
	"encoding/json"
	"testing"
)

func TestOptFloatJSONNull(t *testing.T) {
	var payload struct {
		CloudCover OptFloat `json:"cloud_cover"`
		Visibility OptFloat `json:"visibility"`
		Temp       OptFloat `json:"temp"`
	}
	raw := []byte(`{"cloud_cover": null, "visibility": 0, "temp": 21.4}`)
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}

	if payload.CloudCover.Valid {
		t.Fatalf("null 被解析成了有效值 %+v —— 缺测安全红线被击穿", payload.CloudCover)
	}
	if payload.CloudCover.V != 0 {
		t.Fatalf("缺测值的 V 应保持零值，实际 %v", payload.CloudCover.V)
	}

	if !payload.Visibility.Valid || payload.Visibility.V != 0 {
		t.Fatalf("数值 0 被误判为缺测：%+v", payload.Visibility)
	}
	if !payload.Temp.Valid || payload.Temp.V != 21.4 {
		t.Fatalf("正常数值解析错误：%+v", payload.Temp)
	}
}

func TestOptFloatMissingArrayElement(t *testing.T) {
	var vals []OptFloat
	if err := json.Unmarshal([]byte(`[1.5, null, 3, null]`), &vals); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	want := []bool{true, false, true, false}
	if len(vals) != len(want) {
		t.Fatalf("长度 %d，want %d", len(vals), len(want))
	}
	for i, w := range want {
		if vals[i].Valid != w {
			t.Fatalf("第 %d 项 Valid=%v，want %v", i, vals[i].Valid, w)
		}
	}
}

func TestOptFloatComparisonsAreMissingSafe(t *testing.T) {
	m := Missing()
	if m.GE(0) {
		t.Fatal("Missing().GE(0) 必须为 false，否则缺测会满足「云量>=0」这类判据")
	}
	if m.LT(1e9) {
		t.Fatal("Missing().LT(1e9) 必须为 false，否则缺测会满足「能见度<1000」判成雾")
	}
	if !m.IsZero() {
		t.Fatal("Missing().IsZero() 应为 true")
	}
	if got := m.Or(42); got != 42 {
		t.Fatalf("Missing().Or(42) = %v", got)
	}
	if got := Num(3).Or(42); got != 3 {
		t.Fatalf("Num(3).Or(42) = %v", got)
	}
}

func TestOptFloatArithmeticPropagatesMissing(t *testing.T) {
	if got := Sub(Num(10), Missing()); got.Valid {
		t.Fatalf("Sub(有效, 缺测) 返回了有效值 %+v", got)
	}
	if got := Sub(Missing(), Num(10)); got.Valid {
		t.Fatalf("Sub(缺测, 有效) 返回了有效值 %+v", got)
	}
	if got := Sub(Num(10), Num(3.5)); !got.Valid || got.V != 6.5 {
		t.Fatalf("Sub(10, 3.5) = %+v", got)
	}
	if got := Scale(Missing(), 124); got.Valid {
		t.Fatalf("Scale(缺测, k) 返回了有效值 %+v", got)
	}
	if got := Scale(Num(2), 124); !got.Valid || got.V != 248 {
		t.Fatalf("Scale(2, 124) = %+v", got)
	}
}

func TestOptFloatMarshalNull(t *testing.T) {
	data, err := json.Marshal(Missing())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "null" {
		t.Fatalf("Missing() 序列化为 %s，want null", data)
	}
	data, err = json.Marshal(Num(1489.9))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "1489.9" {
		t.Fatalf("Num(1489.9) 序列化为 %s", data)
	}
}

func TestNumOrMissingRejectsNonFinite(t *testing.T) {
	inf := 1.0
	for i := 0; i < 400; i++ {
		inf *= 10
	}
	if got := NumOrMissing(inf); got.Valid {
		t.Fatalf("NumOrMissing(+Inf) = %+v，应为缺测", got)
	}
	if got := NumOrMissing(inf - inf); got.Valid {
		t.Fatalf("NumOrMissing(NaN) = %+v，应为缺测", got)
	}
}
