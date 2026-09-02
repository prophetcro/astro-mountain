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
// note 透传到 HourRow.Note，用于「日出窗云海」列的辐射雾识别。
func mkCloudSeaRow(site, night string, hour int, rating, cloudSea, note string, t time.Time) model.HourRow {
	return model.HourRow{
		Site: site, Night: night, Hour: hour, HasData: true,
		Rating: rating, CloudSea: cloudSea, Note: note, Time: t,
	}
}

// TestComputeSiteNightStatsCloudSea 锁死「日出窗云海」夜间级聚合：
// 与「主要状态/主要诱因」解耦，仅看日出窗口内机位下方是否形成连续云面。
// 优先级：可见云海+辐射雾兼具 > 云海可见 > 辐射雾遮蔽 > 其他遮蔽 > 无。
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
				mkCloudSeaRow("A", night, 23, model.RATING_OK, "无", "", inWin),
				mkCloudSeaRow("A", night, 0, model.RATING_OK, "无", "", inWin),
			},
			want: "无",
		},
		{
			name: "窗口内有云海且存在通透小时记有",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_OK, "有", "", inWin),
				mkCloudSeaRow("A", night, 0, model.RATING_BAD, "有", "", inWin),
			},
			want: "有",
		},
		{
			name: "窗口内有云海但全被压成不宜记被遮蔽",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_BAD, "有", "", inWin),
				mkCloudSeaRow("A", night, 0, model.RATING_BAD, "有", "", inWin),
			},
			want: "有（被山顶雾/降水遮蔽）",
		},
		{
			name: "云海在窗口外不计入（整夜有、窗口无）",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_OK, "有", "", time.Date(2026, 8, 13, 2, 0, 0, 0, time.UTC)),
				mkCloudSeaRow("A", night, 0, model.RATING_BAD, "有", "", time.Date(2026, 8, 13, 2, 30, 0, 0, time.UTC)),
			},
			want: "无",
		},
		// 新增：日出窗内 note 含"辐射雾"（贴地+静风+少云的雾场景）→ 单独列示为"辐射雾"，
		// 提示贴地雾日出后大概率消散可守候破云，区别于"有（被遮蔽）"的笼统归类。
		{
			name: "日出窗内无云海几何但出现辐射雾→辐射雾",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_BAD, "无",
					"近地RH 98%(代理判据)，辐射雾（静风 1.0m/s，天亮前最重）", inWin),
			},
			want: "辐射雾",
		},
		{
			name: "日出窗内辐射雾遮蔽云海→辐射雾（优先级高于通用被遮蔽）",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_BAD, "有",
					"辐射雾贴地（静风、晴夜辐射冷却形成），清晨随日出消散；"+
						"机位在雾顶之上，脚下为雾海/云海，可守候云隙破云与日出云海", inWin),
			},
			want: "辐射雾",
		},
		{
			name: "日出窗内可见云海与辐射雾同框→辐射雾（云海）",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_OK, "有",
					"辐射雾贴地（静风、晴夜辐射冷却形成），清晨随日出消散", inWin),
			},
			want: "辐射雾（云海）",
		},
		{
			name: "双时次：可见云海+辐射雾分属不同时次→辐射雾（云海）",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_OK, "有", "", inWin),
				mkCloudSeaRow("A", night, 0, model.RATING_BAD, "无",
					"近地RH 98%(代理判据)，辐射雾（静风 1.0m/s，天亮前最重）", inWin),
			},
			want: "辐射雾（云海）",
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

// TestComputeSiteNightStatsRadFogHeight 锁死：辐射雾档须在括号里给出雾层相对机位的
// 高度范围（机下/机上），便于判断无人机能否飞出雾顶、起降是否在雾中。
func TestComputeSiteNightStatsRadFogHeight(t *testing.T) {
	const night = "2026-08-12"
	win := [2]time.Time{
		time.Date(2026, 8, 13, 5, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 13, 7, 0, 0, 0, time.UTC),
	}
	inWin := time.Date(2026, 8, 13, 6, 0, 0, 0, time.UTC)
	mkRad := func(base, top float64, note string) model.HourRow {
		r := mkCloudSeaRow("A", night, 23, model.RATING_BAD, "无", note, inWin)
		r.CloudBaseAGL = model.OptFloat{Valid: true, V: base}
		r.CloudTopAGL = model.OptFloat{Valid: true, V: top}
		return r
	}
	cases := []struct {
		name string
		rows []model.HourRow
		want string
	}{
		{
			name: "辐射雾横跨机位→机下/机上双向标注",
			rows: []model.HourRow{
				mkRad(-10, 20, "近地RH 98%，辐射雾（静风 1.0m/s）"),
			},
			want: "辐射雾（机下10m·机上20m）",
		},
		{
			name: "辐射雾为纯云海在脚下几何(雾顶在机下)→排除不加括号",
			rows: []model.HourRow{
				mkRad(-30, -5, "近地RH 99%，辐射雾（静风 0.8m/s）"),
			},
			want: "辐射雾",
		},
		{
			name: "辐射雾全在机上→仅机上范围",
			rows: []model.HourRow{
				mkRad(5, 40, "近地RH 98%，辐射雾（静风 0.9m/s）"),
			},
			want: "辐射雾（机上5~40m）",
		},
		{
			name: "辐射雾（云海）同框→云海+高度",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_OK, "有", "", inWin),
				mkRad(-10, 20, "辐射雾贴地（静风），清晨随日出消散"),
			},
			want: "辐射雾（云海·机下10m·机上20m）",
		},
		{
			name: "辐射雾无有效高度数据→不加括号",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_BAD, "无",
					"近地RH 98%，辐射雾（静风 1.0m/s）", inWin),
			},
			want: "辐射雾",
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
		mkCloudSeaRow("牵牛岗", "2026-08-12", 23, model.RATING_OK, "有", "", mid),
		mkCloudSeaRow("牵牛岗", "2026-08-12", 0, model.RATING_BAD, "有", "", mid),
		mkCloudSeaRow("太子尖", "2026-08-12", 23, model.RATING_BAD, "无", "", mid),
	}
	text := BuildMarkdownReport(rows, nil, meta, config.Default())

	if !strings.Contains(text, "| 日出窗云海 |") {
		t.Fatalf("汇总表缺少「日出窗云海」列头：\n%s", text)
	}
	if !strings.Contains(text, "有") {
		t.Fatalf("汇总表未出现云海=有：\n%s", text)
	}
}

// TestInSunriseWindowCrossZone 锁死时区承载不一致的误判回归：
// 真实数据里 r.Time 以 UTC 承载本地墙钟（本地 05:00 存成 2026-08-22T05:00:00Z），
// 而 sunriseWin 由 astro.SunriseTime 以 +8 等本地时区承载同一本地墙钟。
// 修复前直接比较绝对瞬间会恒判「不在窗内」，使「日出窗云海」列对真实数据永远为「无」。
func TestInSunriseWindowCrossZone(t *testing.T) {
	meta := testMeta()
	site := model.Site{Name: "太子尖", Lat: 30.0, Lon: 119.0, Alt: 1000}
	sw, ok := SunriseWindowForNight(site, "2026-08-12", int(meta.UTCOffsetHours*3600), config.Default())
	if !ok {
		t.Fatalf("SunriseWindowForNight 未求得日出窗口")
	}
	mid := sw[0].Add(sw[1].Sub(sw[0]) / 2) // +8 区本地墙钟
	// 模拟真实 r.Time：以 UTC 承载同一本地墙钟
	utcCarrier := time.Date(mid.Year(), mid.Month(), mid.Day(), mid.Hour(), mid.Minute(), 0, 0, time.UTC)
	if !inSunriseWindow(utcCarrier, sw) {
		t.Fatalf("UTC 承载的本地 %02d:%02d 应落在 +8 窗口内，却被判为窗外（时区错位回归）", mid.Hour(), mid.Minute())
	}
	// 反例：窗口当天的本地 03:00 以 UTC 承载，应在窗外
	before := time.Date(mid.Year(), mid.Month(), mid.Day(), 3, 0, 0, 0, time.UTC)
	if inSunriseWindow(before, sw) {
		t.Fatalf("UTC 承载的本地 03:00 应在窗口外，却被判为窗内")
	}
}
