package report

import (
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// mkCloudSeaRow 构造一行仅携带测试所需字段的逐小时评估行。
// t 为该行的本地时刻，用于「日出窗云海」按日出窗口过滤的判定。
func mkCloudSeaRow(site, night string, hour int, rating, cloudSea string, t time.Time) model.HourRow {
	return model.HourRow{
		Site: site, Night: night, Hour: hour, HasData: true,
		Rating: rating, CloudSea: cloudSea, Time: t,
	}
}

// TestComputeSiteNightStatsCloudSea 锁死「日出窗云海」夜间级聚合：
// 与「主要状态/主要诱因」解耦，仅看日出窗口内机位下方是否形成连续云面。
func TestComputeSiteNightStatsCloudSea(t *testing.T) {
	const night = "2026-08-12"
	// 显式窗口（与真实日出无关，仅用于验证窗口过滤逻辑）：08-13 05:00 ~ 07:00。
	win := [2]time.Time{
		time.Date(2026, 8, 13, 5, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 13, 7, 0, 0, 0, time.UTC),
	}
	inWin := time.Date(2026, 8, 13, 6, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		rows []model.HourRow
		want string
	}{
		{
			name: "窗口内无任何云海时记无",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_OK, "无", inWin),
				mkCloudSeaRow("A", night, 0, model.RATING_OK, "无", inWin),
			},
			want: "无",
		},
		{
			name: "窗口内有云海且存在通透小时记有",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_OK, "有", inWin),
				mkCloudSeaRow("A", night, 0, model.RATING_BAD, "有", inWin),
			},
			want: "有",
		},
		{
			name: "窗口内有云海但全被压成不宜记被遮蔽",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_BAD, "有", inWin),
				mkCloudSeaRow("A", night, 0, model.RATING_BAD, "有", inWin),
			},
			want: "有（被山顶雾/降水遮蔽）",
		},
		{
			name: "云海在窗口外不计入（整夜有、窗口无）",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_OK, "有", time.Date(2026, 8, 13, 2, 0, 0, 0, time.UTC)),
				mkCloudSeaRow("A", night, 0, model.RATING_BAD, "有", time.Date(2026, 8, 13, 2, 30, 0, 0, time.UTC)),
			},
			want: "无",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := ComputeSiteNightStats("A", night, c.rows, nil, config.Default(), win)
			if st.CloudSea != c.want {
				t.Fatalf("CloudSea = %q, want %q", st.CloudSea, c.want)
			}
		})
	}
}

// TestMarkdownDetailTableHasCloudSeaColumn 确保汇总表真的多出一列「日出窗云海」，
// 且对窗口内存在云海的站点打出「有」，避免云海提示形同虚设。
func TestMarkdownDetailTableHasCloudSeaColumn(t *testing.T) {
	meta := testMeta()
	meta.Peak = model.Str("2026-08-12")
	meta.Sites = []model.Site{
		{Name: "牵牛岗", Lat: 30.026, Lon: 119.007, Alt: 1489.9},
		{Name: "太子尖", Lat: 30.0, Lon: 119.0, Alt: 1000},
	}
	meta.Nights = []string{"2026-08-12"}

	// 用与报告相同的函数算出日出窗口，把行时刻置于窗口内，避免依赖具体日出时刻。
	sw, ok := SunriseWindowForNight(meta.Sites[0], "2026-08-12", int(meta.UTCOffsetHours*3600), config.Default())
	if !ok {
		t.Fatalf("SunriseWindowForNight 未求得日出窗口")
	}
	mid := sw[0].Add(sw[1].Sub(sw[0]) / 2)

	rows := []model.HourRow{
		mkCloudSeaRow("牵牛岗", "2026-08-12", 23, model.RATING_OK, "有", mid),
		mkCloudSeaRow("牵牛岗", "2026-08-12", 0, model.RATING_BAD, "有", mid),
		mkCloudSeaRow("太子尖", "2026-08-12", 23, model.RATING_BAD, "无", mid),
	}
	text := BuildMarkdownReport(rows, nil, meta, config.Default())

	if !strings.Contains(text, "| 日出窗云海 |") {
		t.Fatalf("汇总表缺少「日出窗云海」列头：\n%s", text)
	}
	if !strings.Contains(text, "有") {
		t.Fatalf("汇总表未出现云海=有：\n%s", text)
	}
}
