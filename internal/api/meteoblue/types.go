package meteoblue

import (
	"os"
	"strings"
)

// EnvAPIKey 是 Meteoblue API key 的环境变量名，优先于配置文件（不落盘）。
const EnvAPIKey = "METEOBLUE_API_KEY"

// MetoResponse 是 Meteoblue Forecast API 的 JSON 响应（仅取所需字段）。
// 顶层还含 metadata / units 对象，本工具不需要，解码时由 encoding/json 自动忽略。
//
// Meteoblue 依包组合不同，会返回多个分辨率块：
//   basic-1h + clouds-1h → data_1h          （付费；1 小时间隔）
//   basic-3h + clouds-3h → data_3h          （免费档默认；3 小时间隔）
//   亦可能同返 data_1h + data_3h（如 mixed 组合）。
// DataBlockOf 自动挑「含云量字段」的块来用，免费/付费组合都能直接跑。
type MetoResponse struct {
	Data1h  *DataBlock `json:"data_1h"`
	Data3h  *DataBlock `json:"data_3h"`
	Data6h  *DataBlock `json:"data_6h"`
	DataDay *DataBlock `json:"data_day"`
}

// DataBlockOf 选出要评估的分辨率块：
// 优先取「含云量字段（lowclouds/totalcloudcover）」且分辨率最细的块；
// 免费档默认 basic-3h+clouds-3h 只会给单个 data_3h；付费 basic-1h+clouds-1h 给 data_1h。
// 若没有任何块带云量（极端缺字段），再退回任意存在的块（云量按缺测得，由 evaluate 容忍）。
func (r *MetoResponse) DataBlockOf() *DataBlock {
	if r == nil {
		return nil
	}
	blocks := []*DataBlock{r.Data1h, r.Data3h, r.Data6h, r.DataDay}
	for _, b := range blocks {
		if b == nil {
			continue
		}
		if len(b.LowClouds) > 0 || len(b.TotalCloudCover) > 0 {
			return b
		}
	}
	for _, b := range blocks {
		if b != nil {
			return b
		}
	}
	return nil
}

// DataBlock 是一条分辨率块的逐小时数据；各变量均为数组，索引对齐 Time。
// 用 *float64/*int 是为了容忍缺测——Meteoblue 对缺测时刻返回 null 而非跳过。
//
// JSON tag 严格对齐 Meteoblue 真实字段名（已对照 openapi.yml 校验，非 Open-Meteo
// 的 snake_case 命名）：
//   basic 包   → temperature / relativehumidity / precipitation / precipitation_probability / windspeed / winddirection / pictocode
//   clouds 包  → totalcloudcover / lowclouds / midclouds / highclouds / visibility / fog_probability
type DataBlock struct {
	Time              []string   `json:"time"`
	Temperature       []*float64 `json:"temperature"`
	RelativeHumidity []*float64 `json:"relativehumidity"`
	Precipitation     []*float64 `json:"precipitation"`
	PrecipProbability []*float64 `json:"precipitation_probability"`
	WindSpeed10m      []*float64 `json:"windspeed"`
	WindDirection     []*float64 `json:"winddirection"`
	Visibility        []*float64 `json:"visibility"`
	TotalCloudCover   []*float64 `json:"totalcloudcover"`
	LowClouds         []*float64 `json:"lowclouds"`
	MidClouds         []*float64 `json:"midclouds"`
	HighClouds        []*float64 `json:"highclouds"`
	FogProbability    []*float64 `json:"fog_probability"`
	Pictocode         []*int     `json:"pictocode"`
}

// ResolveAPIKey 优先取环境变量，其次配置文件；返回 (key, source, ok)。
// source 仅用于日志标注密钥来源，不影响取数。
func ResolveAPIKey(envKey, cfgKey string) (string, string, bool) {
	if s := strings.TrimSpace(envKey); s != "" {
		return s, "env", true
	}
	if s := strings.TrimSpace(cfgKey); s != "" {
		return s, "config", true
	}
	return "", "", false
}

// keyConfigured 报告是否已配置任一来源的密钥。
func keyConfigured(cfgKey string) bool {
	return strings.TrimSpace(os.Getenv(EnvAPIKey)) != "" ||
		strings.TrimSpace(cfgKey) != ""
}
