package report

import (
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// mkCloudSeaRow 构造一行仅携带测试所需字段的逐小时评估行。
func mkCloudSeaRow(site, night string, hour int, rating, cloudSea string) model.HourRow {
	return model.HourRow{
		Site: site, Night: night, Hour: hour, HasData: true,
		Rating: rating, CloudSea: cloudSea,
	}
}

// TestComputeSiteNightStatsCloudSea 锁死「有无云海」夜间级聚合：
// 与「主要状态/主要诱因」解耦，仅看几何上机位下方是否形成连续云面。
func TestComputeSiteNightStatsCloudSea(t *testing.T) {
	const night = "2026-08-12"
	cases := []struct {
		name string
		rows []model.HourRow
		want string
	}{
		{
			name: "无任何云海时记无",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_OK, "无"),
				mkCloudSeaRow("A", night, 0, model.RATING_OK, "无"),
			},
			want: "无",
		},
		{
			name: "有云海且存在通透小时记有",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_OK, "有"),
				mkCloudSeaRow("A", night, 0, model.RATING_BAD, "有"),
			},
			want: "有",
		},
		{
			name: "有云海但全被压成不宜记被遮蔽",
			rows: []model.HourRow{
				mkCloudSeaRow("A", night, 23, model.RATING_BAD, "有"),
				mkCloudSeaRow("A", night, 0, model.RATING_BAD, "有"),
			},
			want: "有（被山顶雾/降水遮蔽）",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := ComputeSiteNightStats("A", night, c.rows, nil, config.Default())
			if st.CloudSea != c.want {
				t.Fatalf("CloudSea = %q, want %q", st.CloudSea, c.want)
			}
		})
	}
}

// TestMarkdownDetailTableHasCloudSeaColumn 确保汇总表真的多出一列「云海」，
// 且对存在云海的站点打出「有」，避免云海提示形同虚设。
func TestMarkdownDetailTableHasCloudSeaColumn(t *testing.T) {
	rows := []model.HourRow{
		mkCloudSeaRow("牵牛岗", "2026-08-12", 23, model.RATING_OK, "有"),
		mkCloudSeaRow("牵牛岗", "2026-08-12", 0, model.RATING_BAD, "有"),
		mkCloudSeaRow("太子尖", "2026-08-12", 23, model.RATING_BAD, "无"),
	}
	meta := testMeta()
	meta.Peak = model.Str("2026-08-12")
	meta.Sites = []model.Site{{Name: "牵牛岗"}, {Name: "太子尖"}}
	meta.Nights = []string{"2026-08-12"}
	text := BuildMarkdownReport(rows, nil, meta, config.Default())

	if !strings.Contains(text, "| 云海 |") {
		t.Fatalf("汇总表缺少「云海」列头：\n%s", text)
	}
	if !strings.Contains(text, "有") {
		t.Fatalf("汇总表未出现云海=有：\n%s", text)
	}
}
