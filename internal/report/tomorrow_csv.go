package report

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"

	"github.com/prophetcro/astro-mountain/internal/dualtrack"
	"github.com/prophetcro/astro-mountain/internal/model"
)

var TomorrowCSVFields = []string{
	"site", "night", "time", "time_utc",
	"rating", "relation", "no_data_reason", "no_data_label",
	"h_model_msl", "delta_h", "cloud_base_agl_model", "cloud_base_above_site",
	"terrain_fidelity", "terrain_fidelity_label", "sea_below_unknown",
	"track_active", "quota_exhausted", "next_available", "note",
}

var TomorrowFieldLabels = map[string]string{
	"site":                   "点位",
	"night":                  "观测夜",
	"time":                   "时间(ISO,北京)",
	"time_utc":               "时间(ISO,UTC)",
	"rating":                 "评级",
	"relation":               "云层状态",
	"no_data_reason":         "无数据归因",
	"no_data_label":          "无数据归因(中文)",
	"h_model_msl":            "模式地形高度(m,MSL)",
	"delta_h":                "ΔH=模式地形−机位(m)",
	"cloud_base_agl_model":   "云底相对模式地形(m)",
	"cloud_base_above_site":  "云底相对机位(m)",
	"terrain_fidelity":       "地形保真度",
	"terrain_fidelity_label": "地形保真度(中文)",
	"sea_below_unknown":      "脚下云海不可判",
	"track_active":           "本轨取到数据",
	"quota_exhausted":        "配额耗尽",
	"next_available":         "配额恢复时刻",
	"note":                   "判断说明",
}

var TomorrowFieldNotes = map[string]string{
	"site":     "机位名称",
	"night":    "观测夜编号（含次日凌晨，归到同一天），口径与 A 轨一致",
	"time":     "时次，ISO 格式（北京时间，Asia/Shanghai）",
	"time_utc": "同一时次的 UTC 时刻，来自 Tomorrow.io 原始响应，便于与接口日志对账",
	"rating": "评级：✅通透 / ⚠️风险 / 🔴不宜 / ❓无数据。" +
		"本轨永不产出「云海在脚下」类结论",
	"relation": "云层与机位关系：CLEAR / OVERHEAD / BASE_BELOW_UNKNOWN / NODATA。" +
		"本轨没有 SEA_BELOW 与 IN_CLOUD（缺云顶字段，无法区分）",
	"no_data_reason": "评级为❓无数据时的归因枚举值：ROUND_QUOTA_DOWN(配额耗尽) / " +
		"KEY_MISSING(关键缺失) / SEMANTIC_FAILURE(语义失效) / OUT_OF_HORIZON(超预报窗) / " +
		"AMBIGUOUS_BASE(云底低于机位不可判)。非无数据行为空",
	"no_data_label": "上一列的中文标签，即报告里方括号内的文字",
	"h_model_msl": "由气压反解出的模式地形高度（米，MSL）。逐时次独立计算，" +
		"缺测则本时次判无数据",
	"delta_h": "ΔH = 模式地形高度 − 机位海拔（米）。负值表示模式把山头抹平了；" +
		"诊断量，不参与评级",
	"cloud_base_agl_model": "接口原值：云底相对【模式地形】的高度（米）。不是相对机位",
	"cloud_base_above_site": "云底相对【真实机位】的高度（米）= 模式地形高度 + 云底相对模式地形 − 机位海拔。" +
		"本轨评级链唯一消费的高度量；≤0 落入不可判歧义桶",
	"terrain_fidelity": "地形保真度枚举：FAITHFUL / COARSE / FLATTENED / UNKNOWN。" +
		"按有符号 ΔH 分档，衡量本点位 B 轨云底可信到什么程度",
	"terrain_fidelity_label": "上一列的中文标签",
	"sea_below_unknown": "恒为 True：本轨没有云顶字段，脚下有没有云海一律不可判，" +
		"即便评级为✅通透（头顶通透不等于脚下无云）",
	"track_active":    "该点位本轮是否真的取到数据并参与产出；False 表示配额耗尽或窗口内无样本",
	"quota_exhausted": "该点位本轮是否因免费配额耗尽而未发起任何请求",
	"next_available":  "配额预计恢复时刻（本地时间）；未知或未耗尽时为空",
	"note":            "该时次的判断说明，串起云底关系、地形保真度等所有判据结论",
}

type tomorrowFlatRow struct {
	Track   *dualtrack.TrackResult
	Verdict dualtrack.HourVerdict
}

func flattenTomorrow(tracks []*dualtrack.TrackResult) []tomorrowFlatRow {
	out := make([]tomorrowFlatRow, 0, 128)
	for _, tr := range tracks {
		if tr == nil {
			continue
		}
		for _, v := range tr.Rows {
			out = append(out, tomorrowFlatRow{Track: tr, Verdict: v})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti := tomorrowLocalISO(out[i].Verdict.TimeLocal)
		tj := tomorrowLocalISO(out[j].Verdict.TimeLocal)
		if ti != tj {
			return ti < tj
		}
		return out[i].Track.SiteID < out[j].Track.SiteID
	})
	return out
}

func tomorrowCSVValue(r tomorrowFlatRow, field string) string {
	v, tr := r.Verdict, r.Track
	switch field {
	case "site":
		return tr.SiteID
	case "night":
		return TomorrowNightID(v.TimeLocal)
	case "time":
		return tomorrowLocalISO(v.TimeLocal)
	case "time_utc":
		return tomorrowUTCISO(v.TimeUTC)
	case "rating":
		return v.Rating
	case "relation":
		return v.Rel
	case "no_data_reason":
		return string(v.NoDataReason)
	case "no_data_label":
		if v.NoDataReason == dualtrack.NoDataNone {
			return ""
		}
		return v.NoDataReason.Label()
	case "h_model_msl":
		return csvInt(v.HModelM)
	case "delta_h":
		return csvInt(v.DeltaH)
	case "cloud_base_agl_model":
		return csvInt(v.CloudBaseAGLM)
	case "cloud_base_above_site":
		return csvInt(v.CloudBaseAboveSite)
	case "terrain_fidelity":
		return string(v.TerrainFidelity)
	case "terrain_fidelity_label":
		return v.TerrainFidelity.Label()
	case "sea_below_unknown":
		return model.FormatPyBool(v.SeaBelowUnknown)
	case "track_active":
		return model.FormatPyBool(tr.Active)
	case "quota_exhausted":
		return model.FormatPyBool(tr.QuotaExhausted)
	case "next_available":
		if tr.NextAvailable == nil || tr.NextAvailable.IsZero() {
			return ""
		}
		return tr.NextAvailable.Format(tomorrowStampLayout)
	case "note":
		return v.Note
	default:
		return ""
	}
}

func ExportTomorrowCSV(path string, tracks []*dualtrack.TrackResult) error {
	fh, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建 CSV 文件 %s 失败：%w", path, err)
	}
	defer fh.Close()

	if _, err := fh.Write(utf8BOM); err != nil {
		return fmt.Errorf("写入 CSV BOM 失败：%w", err)
	}

	w := csv.NewWriter(fh)
	w.UseCRLF = true

	header := make([]string, 0, len(TomorrowCSVFields))
	for _, f := range TomorrowCSVFields {
		label, ok := TomorrowFieldLabels[f]
		if !ok {
			label = f
		}
		header = append(header, label)
	}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("写入 CSV 表头失败：%w", err)
	}

	record := make([]string, len(TomorrowCSVFields))
	for _, r := range flattenTomorrow(tracks) {
		for i, f := range TomorrowCSVFields {
			record[i] = tomorrowCSVValue(r, f)
		}
		if err := w.Write(record); err != nil {
			return fmt.Errorf("写入 CSV 数据行失败：%w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("刷新 CSV 缓冲失败：%w", err)
	}
	return nil
}
