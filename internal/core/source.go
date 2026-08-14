package core

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/prophetcro/astro-mountain/internal/config"
)

// Source 标识本次取数走哪条数据轨道。
type Source string

const (
	// SourceOpenMeteo 即 A 轨：Open-Meteo 气压层廓线。
	SourceOpenMeteo Source = "openmeteo"

	// SourceTomorrow 即 B 轨：Tomorrow.io 直接给云底高度。
	SourceTomorrow Source = "tomorrow"

	// SourceMeteoblue 即 C 轨：Meteoblue 山地高分辨率融合预报（Basic-1h + Clouds-1h）。
	SourceMeteoblue Source = "meteoblue"
)

// DefaultSource 是未指定 --source 时的默认数据源。
const DefaultSource = SourceOpenMeteo

// Tomorrow.io 免费额度的官方上限，用于生成给用户看的提示文案；
// 真正的限流与账本由 config 与 api/tomorrow 负责，配置可覆盖这些值。
const (
	TomorrowQuotaPerDay = 500

	TomorrowQuotaPerHour = 25

	TomorrowQuotaPerSecond = 3
)

var sourceLabels = map[Source]string{
	SourceOpenMeteo: "Open-Meteo",
	SourceTomorrow:  "Tomorrow.io",
	SourceMeteoblue: "Meteoblue",
}

var sourceHints = map[Source]string{
	SourceOpenMeteo: "多模式气压层廓线，可判脚下云海，无配额限制；孤立高峰首选",
	SourceTomorrow:  "直接给出云底高度，适合开阔平原；受免费配额限制",
	SourceMeteoblue: "山地高分辨率融合预报（分层云量/降水/能见度），不反演云海几何；需 API key",
}

// Label 返回数据源的展示名；未登记的取值原样返回。
func (s Source) Label() string {
	if l, ok := sourceLabels[s]; ok {
		return l
	}
	return string(s)
}

// Hint 返回数据源的一句话选择建议；未登记的取值返回空串。
func (s Source) Hint() string { return sourceHints[s] }

// IsDefault 判断是否为默认数据源；空串视为默认。
func (s Source) IsDefault() bool { return s == DefaultSource || s == "" }

// AllSources 返回全部可选数据源，顺序即菜单与帮助文本的展示顺序。
func AllSources() []Source { return []Source{SourceOpenMeteo, SourceTomorrow, SourceMeteoblue} }

// ParseSource 解析用户输入的数据源名（大小写与首尾空白无关）。
// 空串回落到 DefaultSource；无法识别时返回带可选项清单的错误。
func ParseSource(s string) (Source, error) {
	norm := Source(strings.ToLower(strings.TrimSpace(s)))
	if norm == "" {
		return DefaultSource, nil
	}
	for _, v := range AllSources() {
		if norm == v {
			return v, nil
		}
	}

	names := make([]string, 0, len(AllSources()))
	for _, v := range AllSources() {
		names = append(names, string(v))
	}
	sort.Strings(names)
	return "", fmt.Errorf("无法识别的数据源 %q，可选：%s（默认 %s）",
		s, strings.Join(names, " / "), DefaultSource)
}

// envTomorrowAPIKey 是 Tomorrow.io 密钥的环境变量名，优先于配置文件，
// 这样密钥不必落盘。
const envTomorrowAPIKey = "TOMORROW_API_KEY"

func tomorrowKeyConfigured(cfg config.Config) bool {
	if strings.TrimSpace(os.Getenv(envTomorrowAPIKey)) != "" {
		return true
	}
	return strings.TrimSpace(cfg.API.TomorrowAPIKey) != ""
}

// UseTomorrow 判断本次是否真的走 B 轨：必须选了 tomorrow、配置开关打开、
// 且密钥已配置，三者缺一即回落为 false。
func UseTomorrow(src Source, cfg config.Config) bool {
	if src != SourceTomorrow {
		return false
	}
	if !cfg.API.TomorrowEnabled {
		return false
	}
	return tomorrowKeyConfigured(cfg)
}

// TomorrowUnavailableReason 在用户点名要 B 轨却无法满足时，返回一句可执行的
// 原因说明；一切正常或压根没选 B 轨时返回空串。
//
// wired 表示 B 轨取数链路是否已接通（见 Engine.TomorrowDeliverable）。
// 调用方据此中止运行——宁可不出报告，也不拿 A 轨结果冒充用户点名的 B 轨。
func TomorrowUnavailableReason(src Source, cfg config.Config, wired bool) string {
	if src != SourceTomorrow {
		return ""
	}
	if !cfg.API.TomorrowEnabled {
		return "配置项 api.tomorrow_enabled 为 false，B 轨（Tomorrow.io）已被关闭；" +
			"请改配置或改用 --source openmeteo"
	}
	if !tomorrowKeyConfigured(cfg) {
		return "未配置 Tomorrow.io API key，B 轨无法取数；" +
			"请设置环境变量 " + envTomorrowAPIKey + "=<你的 key>（推荐，不落盘），" +
			"或填写 configs/config.json 的 api.tomorrow_api_key（会明文落盘）；" +
			"也可改用 --source openmeteo"
	}
	if !wired {
		return "本版尚未接通 Tomorrow.io（B 轨）的取数与报告链路，交付不出 B 轨报告" +
			"（构建或接线问题，不是你的配置错）；请改用 --source openmeteo，" +
			"并向维护者反馈"
	}
	return ""
}

// TomorrowQuotaNotice 返回 B 轨配额提示文案，供菜单与帮助信息展示。
func TomorrowQuotaNotice() string {
	return fmt.Sprintf("Tomorrow.io 免费配额：%d 次/天、%d 次/小时、%d 次/秒；"+
		"点位数 × 天数会成倍消耗，超额后当轮该点位判无数据",
		TomorrowQuotaPerDay, TomorrowQuotaPerHour, TomorrowQuotaPerSecond)
}

const envMeteoblueAPIKey = "METEOBLUE_API_KEY"

func meteoblueKeyConfigured(cfg config.Config) bool {
	if strings.TrimSpace(os.Getenv(envMeteoblueAPIKey)) != "" {
		return true
	}
	return strings.TrimSpace(cfg.API.MeteoblueAPIKey) != ""
}

// UseMeteoblue 判断本次是否真的走 C 轨：必须选了 meteoblue、配置开关打开、
// 且密钥已配置，三者缺一即回落为 false。
func UseMeteoblue(src Source, cfg config.Config) bool {
	if src != SourceMeteoblue {
		return false
	}
	if !cfg.API.MeteoblueEnabled {
		return false
	}
	return meteoblueKeyConfigured(cfg)
}

// MeteoblueUnavailableReason 在用户点名要 C 轨却无法满足时，返回一句可执行的
// 原因说明；一切正常或压根没选 C 轨时返回空串。
//
// wired 表示 C 轨取数链路是否已接通（见 Engine.MeteoblueDeliverable）。
// 调用方据此中止运行——宁可不出报告，也不拿 A 轨结果冒充用户点名的 C 轨。
func MeteoblueUnavailableReason(src Source, cfg config.Config, wired bool) string {
	if src != SourceMeteoblue {
		return ""
	}
	if !cfg.API.MeteoblueEnabled {
		return "配置项 api.meteoblue_enabled 为 false，C 轨（Meteoblue）已被关闭；" +
			"请改配置或改用 --source openmeteo"
	}
	if !meteoblueKeyConfigured(cfg) {
		return "未配置 Meteoblue API key，C 轨无法取数；" +
			"请设置环境变量 " + envMeteoblueAPIKey + "=<你的 key>（推荐，不落盘），" +
			"或填写 configs/config.json 的 api.meteoblue_api_key（会明文落盘）；" +
			"也可改用 --source openmeteo"
	}
	if !wired {
		return "本版尚未接通 Meteoblue（C 轨）的取数与报告链路，交付不出 C 轨报告" +
			"（构建或接线问题，不是你的配置错）；请改用 --source openmeteo，" +
			"并向维护者反馈"
	}
	return ""
}
