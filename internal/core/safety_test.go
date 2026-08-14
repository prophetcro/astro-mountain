package core

import (
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
)

func TestProfileUsable(t *testing.T) {
	if ProfileUsable(nil) {
		t.Fatal("空廓线必须判为不可用")
	}
	if ProfileUsable([]profile.Level{}) {
		t.Fatal("零长廓线必须判为不可用")
	}
	if !ProfileUsable([]profile.Level{{Pressure: 850, Height: 1500}}) {
		t.Fatal("有层的廓线应判为可用")
	}
}

func TestAllLevelsMissing(t *testing.T) {
	all := map[int]model.RawLevel{
		1000: {CC: model.Missing(), GH: model.Missing(), RH: model.Missing()},
		850:  {CC: model.Missing(), GH: model.Num(1500), RH: model.Missing()},
	}
	if !AllLevelsMissing(all) {
		t.Fatal("仅有位势高度、云量与湿度全缺测，应判为全缺测")
	}
	partial := map[int]model.RawLevel{
		1000: {CC: model.Missing(), GH: model.Missing(), RH: model.Missing()},
		850:  {CC: model.Missing(), GH: model.Num(1500), RH: model.Num(88)},
	}
	if AllLevelsMissing(partial) {
		t.Fatal("有一层 RH 有效就不该判为全缺测")
	}
}

func TestSafetyAllMissingRowMustBeNoData(t *testing.T) {
	good := HourRow{
		Site: "牵牛岗", TimeISO: "2026-08-13T23:00",
		HasData: false, Rating: RATING_NODATA, Relation: model.NullStr(),
	}
	if err := AssertNoDataRow(good); err != nil {
		t.Fatalf("合规的无数据行被判违规：%v", err)
	}

	bad := good
	bad.Rating = RATING_CLEAR
	err := AssertNoDataRow(bad)
	if err == nil {
		t.Fatal("无数据行被评为「通透」却没有被拦下 —— 缺测安全红线失效")
	}
	if !strings.Contains(err.Error(), "缺测安全红线") {
		t.Fatalf("错误信息应点明红线被破坏，实际：%v", err)
	}
	if bad.Rating == RATING_NODATA {
		t.Fatal("测试自身写错了：RATING_CLEAR 不应等于 RATING_NODATA")
	}
}

func TestAuditRowsFindsContradictions(t *testing.T) {
	rows := []HourRow{
		{Site: "A", TimeISO: "2026-08-13T22:00", HasData: false, Rating: RATING_NODATA,
			Relation: model.NullStr()},
		{Site: "B", TimeISO: "2026-08-13T22:00", HasData: false, Rating: RATING_CLEAR},
		{Site: "C", TimeISO: "2026-08-13T23:00", HasData: true, Rating: RATING_OK,
			Relation: model.NullStr()},
		{Site: "D", TimeISO: "2026-08-13T23:00", HasData: true, Rating: RATING_NODATA,
			Relation: model.Str(REL_CLEAR)},
		{Site: "E", TimeISO: "2026-08-14T00:00", HasData: true, Rating: RATING_OK,
			Relation: model.Str(REL_SEA_BELOW)},
	}
	issues := AuditRows(rows)
	if len(issues) != 3 {
		t.Fatalf("应抓出 3 条矛盾，实际 %d 条：%v", len(issues), issues)
	}
	joined := strings.Join(issues, "\n")
	for _, site := range []string{"B", "C", "D"} {
		if !strings.Contains(joined, site) {
			t.Fatalf("点位 %s 的矛盾未被抓出：%s", site, joined)
		}
	}
	if strings.Contains(joined, "点位 A") || strings.Contains(joined, "点位 E") {
		t.Fatalf("合规行被误报：%s", joined)
	}
}

func TestSafeSpreadAndLCLPropagateMissing(t *testing.T) {
	if got := SafeSpread(model.Num(15), model.Missing()); got.Valid {
		t.Fatalf("露点缺测时温露差应缺测，实际 %+v", got)
	}
	if got := SafeSpread(model.Missing(), model.Num(5)); got.Valid {
		t.Fatalf("气温缺测时温露差应缺测，实际 %+v", got)
	}
	spread := SafeSpread(model.Num(15), model.Num(12.5))
	if !spread.Valid || spread.V != 2.5 {
		t.Fatalf("SafeSpread(15, 12.5) = %+v", spread)
	}
	lcl := SafeLCL(spread)
	if !lcl.Valid || lcl.V != 310 {
		t.Fatalf("SafeLCL(2.5) = %+v，want 310", lcl)
	}
	if got := SafeLCL(model.Missing()); got.Valid {
		t.Fatalf("缺测温露差的 LCL 应缺测，实际 %+v", got)
	}
}
