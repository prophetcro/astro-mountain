package report

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"

	"github.com/prophetcro/astro-mountain/internal/model"
)

var CSVFields = []string{
	"site", "alt", "night", "time", "has_data", "rating", "relation", "cloud_sea",
	"cloud_base_agl", "cloud_base_msl", "cloud_top_agl", "cloud_top_msl",
	"cloud_thickness", "layer_max_cc", "cloud_low", "cloud_mid", "cloud_high",
	"visibility", "temp", "dew", "spread", "wind_ms",
	"boundary_layer_height", "freezing_level_height", "lcl_agl_est",
	"sun_alt", "moon_alt", "moon_illum", "gc_alt", "astro_dark", "note",
}

var FieldLabels = map[string]string{
	"site":                  "点位",
	"alt":                   "机位海拔(m)",
	"night":                 "观测夜",
	"time":                  "时间(ISO)",
	"has_data":              "有数据",
	"rating":                "评级",
	"relation":              "云层状态",
	"cloud_sea":             "云海(有无)",
	"cloud_base_agl":        "云底相对机位(m)",
	"cloud_base_msl":        "云底海拔(m)",
	"cloud_top_agl":         "云顶相对机位(m)",
	"cloud_top_msl":         "云顶海拔(m)",
	"cloud_thickness":       "云厚(m)",
	"layer_max_cc":          "最大层云量(%)",
	"cloud_low":             "低云量(%)",
	"cloud_mid":             "中云量(%)",
	"cloud_high":            "高云量(%)",
	"visibility":            "能见度(m)",
	"temp":                  "气温(°C)",
	"dew":                   "露点(°C)",
	"spread":                "温露差(°C)",
	"wind_ms":               "风速(m/s)",
	"boundary_layer_height": "边界层高度(m)",
	"freezing_level_height": "冻结层高度(m)",
	"lcl_agl_est":           "LCL估算高度(m)",
	"sun_alt":               "太阳高度(°)",
	"moon_alt":              "月亮高度(°)",
	"moon_illum":            "月相照度(%)",
	"gc_alt":                "银心高度(°)",
	"astro_dark":            "天文暗夜",
	"note":                  "判断说明",
}

var FieldNotes = map[string]string{
	"site":                  "机位名称",
	"alt":                   "机位海拔，单位米（MSL）",
	"night":                 "观测夜编号（含次日凌晨，归到同一天）",
	"time":                  "时次，ISO 格式（北京时间，Asia/Shanghai）",
	"has_data":              "该时次是否有有效气象数据；True/False。False 多因超出预报时效或缺测",
	"rating":                "评级：✅通透 / ⚠️风险 / 🔴不宜 / ❓无数据",
	"relation":              "云层与机位关系：云海在脚下 / 机位在云中 / 云在头顶 / 通透 / 无数据",
	"cloud_sea":             "有无云海：有=机位下方存在连续云面（云海在脚下几何成立）；无=无。只看几何，与是否起雾/降水无关",
	"cloud_base_agl":        "云底相对机位的高度（AGL，米）。负=云底在机位之下、0附近=机位在云中",
	"cloud_base_msl":        "云底海拔（MSL，米）",
	"cloud_top_agl":         "云顶相对机位的高度（AGL，米）。负=云顶在机位之下（云海在脚下）",
	"cloud_top_msl":         "云顶海拔（MSL，米）",
	"cloud_thickness":       "云层厚度=云顶−云底，单位米",
	"layer_max_cc":          "该云层内最大云量（%），取自气压层剖面反演",
	"cloud_low":             "低云量（%），优先用模式 cloud_cover_low，缺则取 2.5km 以下层最大云量",
	"cloud_mid":             "中云量（%），模式实测产品",
	"cloud_high":            "高云量（%），模式实测产品",
	"visibility":            "能见度（米）。本区域 ICON 常不提供，缺测时为 -",
	"temp":                  "气温（°C）",
	"dew":                   "露点温度（°C）",
	"spread":                "温露差=气温−露点（°C），越小越易结露起雾",
	"wind_ms":               "风速（m/s，10m 高度）",
	"boundary_layer_height": "边界层高度（米），模式产品，缺测时为 -",
	"freezing_level_height": "冻结层高度（米），模式产品，缺测时为 -",
	"lcl_agl_est":           "抬升凝结高度估算值（米）=124×温露差，仅作辐射雾辅助指标，非云底观测值",
	"sun_alt":               "太阳高度角（°），≤-18° 判为天文暗夜",
	"moon_alt":              "月亮高度角（°），>0 表示月亮在地平线之上",
	"moon_illum":            "月相照度（%），近似算法，0=新月、100=满月",
	"gc_alt":                "银心（Sgr A*）高度角（°），越高银河越亮、越利于银河构图",
	"astro_dark":            "是否天文暗夜（太阳高度≤-18°）；True/False",
	"note":                  "该时次的判断说明，串起云层关系、雾、温露差等所有判据结论",
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func csvValue(row model.HourRow, field string) string {
	switch field {
	case "site":
		return row.Site
	case "alt":
		return model.FormatPyFloat(row.Alt)
	case "night":
		return row.Night
	case "time":
		return row.TimeISO
	case "has_data":
		return model.FormatPyBool(row.HasData)
	case "rating":
		return row.Rating
	case "relation":
		return csvStr(row.Relation)
	case "cloud_sea":
		return row.CloudSea
	case "cloud_base_agl":
		return csvInt(row.CloudBaseAGL)
	case "cloud_base_msl":
		return csvInt(row.CloudBaseMSL)
	case "cloud_top_agl":
		return csvInt(row.CloudTopAGL)
	case "cloud_top_msl":
		return csvInt(row.CloudTopMSL)
	case "cloud_thickness":
		return csvInt(row.CloudThickness)
	case "layer_max_cc":
		return csvInt(row.LayerMaxCC)
	case "cloud_low":
		return csvInt(row.CloudLow)
	case "cloud_mid":
		return csvInt(row.CloudMid)
	case "cloud_high":
		return csvInt(row.CloudHigh)
	case "visibility":
		return csvInt(row.Visibility)
	case "temp":
		return csvFloat1(row.Temp)
	case "dew":
		return csvFloat1(row.Dew)
	case "spread":
		return csvFloat1(row.Spread)
	case "wind_ms":
		return csvFloat1(row.WindMS)
	case "boundary_layer_height":
		return csvInt(row.BoundaryLayerHeight)
	case "freezing_level_height":
		return csvInt(row.FreezingLevelHeight)
	case "lcl_agl_est":
		return csvInt(row.LCLAGLEst)
	case "sun_alt":
		return csvFloat1(row.SunAlt)
	case "moon_alt":
		return csvFloat1(row.MoonAlt)
	case "moon_illum":
		return csvInt(row.MoonIllum)
	case "gc_alt":
		return csvFloat1(row.GCAlt)
	case "astro_dark":
		return model.FormatPyBool(row.AstroDark)
	case "note":
		return row.Note
	default:
		return ""
	}
}

func ExportCSV(path string, rows []model.HourRow) error {
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

	header := make([]string, 0, len(CSVFields))
	for _, f := range CSVFields {
		label, ok := FieldLabels[f]
		if !ok {
			label = f
		}
		header = append(header, label)
	}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("写入 CSV 表头失败：%w", err)
	}

	sorted := append([]model.HourRow(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].TimeISO != sorted[j].TimeISO {
			return sorted[i].TimeISO < sorted[j].TimeISO
		}
		return sorted[i].Site < sorted[j].Site
	})

	record := make([]string, len(CSVFields))
	for _, row := range sorted {
		for i, f := range CSVFields {
			record[i] = csvValue(row, f)
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
