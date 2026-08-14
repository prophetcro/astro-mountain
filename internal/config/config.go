// Package config 负责 CLI 的配置与点位加载：把内置默认值、仓库 configs/ 下的
// config.json / sites.json 以及命令行显式指定的路径归并成一份运行时配置。
//
// 设计上"内置默认值永远可用"——所有外部文件都是可选覆盖层，缺失或损坏时
// CLI 仍能以默认配置跑起来，只是把问题以 warning 或 error 暴露给用户。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// APIConfig 是数据源相关配置：Open-Meteo 主源与 Tomorrow.io 副源。
type APIConfig struct {
	Endpoint          string  `json:"endpoint"`
	ElevationEndpoint string  `json:"elevation_endpoint"`
	Models            string  `json:"models"`
	CrossModel        string  `json:"cross_model"` // 双模型交叉对比的第二个模式；非空即默认开启对比（固定与 models 并列拉取 ICON/GFS）
	Timezone          string  `json:"timezone"`
	CacheEnabled      bool    `json:"cache_enabled"`
	CacheDir          string  `json:"cache_dir"`
	CacheExpireS      int     `json:"cache_expire_s"`
	Retries           int     `json:"retries"`
	BackoffFactor     float64 `json:"backoff_factor"`

	// TomorrowEnabled 控制是否启用 Tomorrow.io 副源。
	TomorrowEnabled bool `json:"tomorrow_enabled"`

	TomorrowEndpoint string `json:"tomorrow_endpoint"`

	TomorrowAPIKey string `json:"tomorrow_api_key"`

	// TomorrowCloudBaseUnit 与 TomorrowCloudBaseDatum 描述副源云底的单位与基准面
	// （海拔 MSL 还是地面 AGL），两者决定了云底数值如何换算到本项目统一口径。
	TomorrowCloudBaseUnit string `json:"tomorrow_cloud_base_unit"`

	TomorrowCloudBaseDatum string `json:"tomorrow_cloud_base_datum"`

	TomorrowTimeoutS int `json:"tomorrow_timeout_s"`

	TomorrowCacheExpireS int `json:"tomorrow_cache_expire_s"`

	// 以下三项是副源的配额与限流参数，用于避免打爆免费额度。
	TomorrowQuotaPerHour int `json:"tomorrow_quota_per_hour"`

	TomorrowQuotaPerDay int `json:"tomorrow_quota_per_day"`

	TomorrowMinIntervalMS int `json:"tomorrow_min_interval_ms"`

	// Meteoblue 副源（C 轨）：山地高分辨率融合预报，免费层含 Basic-1h + Clouds-1h。
	MeteoblueEnabled bool `json:"meteoblue_enabled"`

	MeteoblueEndpoint string `json:"meteoblue_endpoint"`

	MeteoblueAPIKey string `json:"meteoblue_api_key"`
}

// WindowConfig 定义夜间时段与"核心时段"的小时边界，用于筛选参与评分的时刻。
type WindowConfig struct {
	NightStartHour int `json:"night_start_hour"`
	NightEndHour   int `json:"night_end_hour"`
	CoreStartHour  int `json:"core_start_hour"`
	CoreEndHour    int `json:"core_end_hour"`

	// SunriseWindowBeforeMin / SunriseWindowAfterMin 定义「日出拍摄窗口」相对日出的前后余量（分钟）。
	// 报告「日出窗云海」列只统计落在该窗口内的时次，反映日出前后机位下方云海状况。
	SunriseWindowBeforeMin int `json:"sunrise_window_before_min"`
	SunriseWindowAfterMin  int `json:"sunrise_window_after_min"`
}

// Thresholds 汇总所有判定阈值：云量、湿度、能见度、云层几何、露点与天文条件。
// 这些值全部可由用户在 config.json 中覆盖，是调参的唯一入口。
type Thresholds struct {
	CloudCoverThreshold   float64 `json:"cloud_cover_threshold"`
	RHThresholdLow        float64 `json:"rh_threshold_low"`
	RHThresholdHigh       float64 `json:"rh_threshold_high"`
	RHLowLayerPressureMin int     `json:"rh_low_layer_pressure_min"`

	FogVisibilityM  float64 `json:"fog_visibility_m"`
	HazeVisibilityM float64 `json:"haze_visibility_m"`
	FogCalmWindMS   float64 `json:"fog_calm_wind_ms"`
	FogProxyRHHigh  float64 `json:"fog_proxy_rh_high"`
	FogProxyRHWarn  float64 `json:"fog_proxy_rh_warn"`

	OverheadSevereCC        float64 `json:"overhead_severe_cc"`
	LayerMinHalfSpanFrac    float64 `json:"layer_min_half_span_frac"`
	MinLevelHeightMSL       float64 `json:"min_level_height_msl"`
	CloudSeaMaxDepthM       float64 `json:"cloud_sea_max_depth_m"`
	ProfileLowcloudCrossChk float64 `json:"profile_lowcloud_crosscheck"`
	CloudSeaSuspectLowcloud float64 `json:"cloud_sea_suspect_lowcloud"`

	MidCloudVeilCC float64 `json:"mid_cloud_veil_cc"`

	HighCloudThinVeilCC float64 `json:"high_cloud_thin_veil_cc"`

	DewSpreadC   float64 `json:"dew_spread_c"`
	LCLWarnAGLM  float64 `json:"lcl_warn_agl_m"`
	LCLAlertAGLM float64 `json:"lcl_alert_agl_m"`

	AstroDarkSunAlt float64 `json:"astro_dark_sun_alt"`
	MoonBrightIllum float64 `json:"moon_bright_illum"`

	CloudCrosscheckDeltaM float64 `json:"cloud_crosscheck_delta_m"`
}

// OutputConfig 控制产物输出：报告目录、抖音图目录与默认预报天数。
type OutputConfig struct {
	OutDir      string `json:"out_dir"`
	DouyinDir   string `json:"douyin_dir"`
	AutoDouyin  bool   `json:"auto_douyin"`
	ExportCSV   bool   `json:"export_csv"`
	ExportJSON  bool   `json:"export_json"`
	DefaultDays int    `json:"default_days"`
}

// DouyinConfig 是竖屏长图的排版参数：画布尺寸、分页与字体候选。
type DouyinConfig struct {
	Width               int      `json:"width"`
	Height              int      `json:"height"`
	SafeBottom          int      `json:"safe_bottom"`
	Sections            []string `json:"sections"`
	PageRows            int      `json:"page_rows"`
	TableSplitThreshold int      `json:"table_split_threshold"`
	HardFloorScale      float64  `json:"hard_floor_scale"`
	FontPath            string   `json:"font_path"`
	FontCandidates      []string `json:"font_candidates"`
}

// Config 是整个 CLI 的运行时配置。
type Config struct {
	Version int          `json:"version"`
	API     APIConfig    `json:"api"`
	Window  WindowConfig `json:"window"`
	Thresh  Thresholds   `json:"thresholds"`
	Output  OutputConfig `json:"output"`
	Douyin  DouyinConfig `json:"douyin"`

	// Source 记录配置来自哪里（文件路径或 BuiltinSource），仅用于向用户展示，
	// 不参与序列化，以免写回时污染配置文件。
	Source string `json:"-"`
}

// ConfigFileName 是主配置的固定文件名。
const ConfigFileName = "config.json"

// BuiltinSource 是 Config.Source / SitesResult.Source 在使用内置默认值时的取值。
const BuiltinSource = "内置默认"

// Default 返回内置默认配置。
//
// 默认值以嵌入的 config.json 为唯一真相源而非 Go 结构体字面量，这样默认配置和
// 用户导出的模板天然一致；解析失败说明构建产物损坏，直接 panic。
func Default() Config {
	var cfg Config
	if err := json.Unmarshal(defaultConfigJSON, &cfg); err != nil {

		panic(fmt.Sprintf("内置默认 config.json 解析失败：%v", err))
	}
	cfg.Source = BuiltinSource
	return cfg
}

// Load 在内置默认值之上叠加外部 config.json。
//
// 采用"先填默认再 Unmarshal 覆盖"的方式，因此配置文件只需写出想改的字段，
// 未出现的字段保持默认而不会退化成 Go 零值。出错时返回的 Config 仍是可用的默认配置。
func Load(explicit string) (Config, error) {
	cfg := Default()

	path, err := resolvePath(explicit, ConfigFileName)
	if err != nil {
		return cfg, err
	}
	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("读取配置文件 %s 失败：%w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, decorateJSONError(path, data, err)
	}
	cfg.Source = path
	return cfg, nil
}

// Save 把配置写回 path（原子替换 + .bak 备份）。
func Save(cfg Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败：%w", err)
	}
	data = append(data, '\n')
	return atomicWrite(path, data)
}

// resolvePath 定位配置文件：显式路径优先且必须存在（不存在即报错，避免用户以为
// 生效了其实在用默认值）；否则依次尝试工作目录和可执行文件同级的 configs/。
// 都找不到时返回空串，表示应使用内置默认。
func resolvePath(explicit, filename string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("显式指定的配置文件 %s 不可用：%w", explicit, err)
		}
		return explicit, nil
	}
	candidates := []string{filepath.Join("configs", filename)}
	if exe, err := os.Executable(); err == nil {
		// 解析软链接，保证从 symlink 启动时也能找到真实安装目录下的 configs/。
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "configs", filename))
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, nil
		}
	}
	return "", nil
}

// decorateJSONError 把 encoding/json 的字节偏移错误翻译成行列号，
// 让用户能直接定位到配置文件里写错的那一行。
func decorateJSONError(path string, data []byte, err error) error {
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		line, col := offsetToLineCol(data, int(syn.Offset))
		return fmt.Errorf("配置文件 %s 第 %d 行第 %d 列 JSON 语法错误：%w", path, line, col, err)
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		line, col := offsetToLineCol(data, int(typeErr.Offset))
		return fmt.Errorf("配置文件 %s 第 %d 行第 %d 列字段 %q 类型错误（期望 %s）：%w",
			path, line, col, typeErr.Field, typeErr.Type, err)
	}
	return fmt.Errorf("配置文件 %s 解析失败：%w", path, err)
}

// offsetToLineCol 把字节偏移换算成 1 基的行列号，偏移越界时被夹到合法范围。
func offsetToLineCol(data []byte, offset int) (int, int) {
	if offset > len(data) {
		offset = len(data)
	}
	if offset < 0 {
		offset = 0
	}
	line := 1 + strings.Count(string(data[:offset]), "\n")
	lastNL := strings.LastIndex(string(data[:offset]), "\n")
	col := offset - lastNL
	if lastNL < 0 {
		col = offset + 1
	}
	return line, col
}

// atomicWrite 通过"临时文件 + rename"原子地写入，避免进程中断留下半截文件。
//
// 写盘前先把数据回解一遍做自校验，宁可报错也不要覆盖出一个不可解析的配置；
// 覆盖已有文件前会留一份 .bak，备份失败不阻断主流程。
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建目录失败：%w", err)
	}
	var probe any
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("写回前自校验失败（生成的 JSON 不可解析）：%w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写临时文件失败：%w", err)
	}
	if _, err := os.Stat(path); err == nil {
		if old, err := os.ReadFile(path); err == nil {

			_ = os.WriteFile(path+".bak", old, 0o644)
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("原子替换 %s 失败：%w", path, err)
	}
	return nil
}
