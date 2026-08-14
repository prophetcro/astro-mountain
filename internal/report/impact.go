package report

import (
	"fmt"

	"github.com/prophetcro/astro-mountain/internal/astro"
	"github.com/prophetcro/astro-mountain/internal/config"
)

const (
	SrcAuto = "脚本自动计算"

	SrcManual = "需人工/外部数据"
)

type ImpactFactor struct {
	Name   string
	Effect string
	Level  string
	Weight int
	Source string
	Reason string
}

func (f ImpactFactor) IsAuto() bool {
	return len(f.Source) >= len(SrcAuto) && f.Source[:len(SrcAuto)] == SrcAuto
}

func ImpactFactors(cfg config.Config) []ImpactFactor {
	t := cfg.Thresh
	return []ImpactFactor{
		{
			Name: "低云云底高度与云量",
			Effect: "决定性因素。云底低于机位→机位在云中，直接废片；云在头顶→星空被成片遮挡；" +
				"云顶低于机位→云海在脚下，头顶通透且低云还挡住了山下城镇的地面灯光，是最佳条件。",
			Level:  "极高",
			Weight: 30,
			Source: SrcAuto + "（icon_seamless 1000–700hPa 气压层剖面反演云底/云顶 MSL，" +
				"并与模式 cloud_cover_low 交叉校验）",
			Reason: "唯一能一票否决的因素：其余条件再完美，头顶盖云就是 0 张可用片。",
		},
		{
			Name: "月光 / 月相",
			Effect: "亮月把天空背景整体提亮 2~4 个星等，暗弱流星与银河细节被直接洗掉；" +
				"满月夜银河基本不可见，流星计数常只剩无月夜的 1/3。",
			Level:  "极高",
			Weight: 20,
			Source: SrcAuto + "（内联近似算法：月龄/照度 + 月亮地平高度，误差约 0.3°）",
			Reason: "仅次于云：可预测、不可改变，且一旦亮月整夜在天就没有补救手段，只能改日期。",
		},
		{
			Name: "能见度 / 雾",
			Effect: "雾（能见度<1000m）等价于机位泡在云里，前景与星空全糊；" +
				"轻雾/霾（1000~5000m）压低对比度、把地面灯光散射成光幕。",
			Level:  "高",
			Weight: 12,
			Source: SrcAuto + fmt.Sprintf("（模式 visibility；ICON 在东亚不提供该量时，"+
				"退化为近地 RH≥%s%% 的代理判据，可靠性低一档）",
				FormatFixed(t.FogProxyRHHigh, 0)),
			Reason: "与云并列的硬性遮挡，但山顶辐射雾常有时段性（后半夜最重），" +
				"尚有等待窗口的余地，故低于云与月光。",
		},
		{
			Name: "光污染",
			Effect: "决定极限星等与银河可拍摄程度。Bortle 2 与 Bortle 5 之间，" +
				"可记录到的流星数量可差一倍以上；也决定单张曝光的天空背景亮度上限。",
			Level:  "高",
			Weight: 12,
			Source: SrcManual + "（本脚本不计算。请查 lightpollutionmap.info 的 " +
				"VIIRS/Bortle 图层或实测 SQM）",
			Reason: "影响幅度接近月光，但它是机位的固有属性、同一机位每晚相同，" +
				"属于选点阶段一次性确定的量，不随预报变化。",
		},
		{
			Name: "天文暗夜时长 / 暮光",
			Effect: "太阳高度 > -18° 时天空仍有暮光背景，暗弱流星拍不到；" +
				"夏季高纬度夜短，实际可用的全黑时段可能只有 4~5 小时。",
			Level:  "中",
			Weight: 8,
			Source: SrcAuto + fmt.Sprintf("（太阳高度角 ≤ %s° 判为天文暗夜）",
				FormatFixed(t.AstroDarkSunAlt, 0)),
			Reason: "决定「能拍多久」而非「能不能拍」，是时长维度的乘数因子。",
		},
		{
			Name: "银心高度 / 银河",
			Effect: "对流星拍摄影响小（流星辐射点与银心无关），但决定银河能否入画、" +
				"以及「流星+银河」这类构图能否成立。银心低于 10° 时被大气消光严重压暗。",
			Level:  "中",
			Weight: 6,
			Source: SrcAuto + fmt.Sprintf("（Sgr A* 赤经 %s° / 赤纬 %s° 换算地平高度）",
				FormatFixed(astro.GCRADeg, 2), FormatFixed(astro.GCDecDeg, 2)),
			Reason: "只影响构图上限而非成片率；若只拍流星可把此项权重转移给月光与云。",
		},
		{
			Name: "温度露点差 / 镜头结露",
			Effect: fmt.Sprintf("温露差 < %s℃ 时前镜组会在 1~2 小时内起雾，"+
				"整段素材报废且事后无法修复；也是辐射雾即将生成的先兆。",
				FormatFixed(t.DewSpreadC, 0)),
			Level:  "中",
			Weight: 5,
			Source: SrcAuto + "（temperature_2m − dew_point_2m，" +
				"并给出经验式 LCL≈124×温露差 作为辐射雾辅助指标）",
			Reason: "属可控风险：带加热带即可基本消除，故权重低于不可控的天气项，" +
				"但不带装备时它的实际杀伤力等同于云。",
		},
		{
			Name: "风",
			Effect: fmt.Sprintf("静风（< %s m/s）利于长曝稳定，"+
				"但正是辐射雾的生成条件（天亮前最重）；有风则多为平流云/低云压顶，"+
				"成片且持续时间长，同时抖动三脚架。", FormatFixed(t.FogCalmWindMS, 0)),
			Level:  "低",
			Weight: 3,
			Source: SrcAuto + "（wind_speed_10m，单位 m/s）",
			Reason: "自身很少直接决定成败，主要价值是帮助判断雾/云的成因与持续性，" +
				"属于修正项。",
		},
		{
			Name: "视宁度 (seeing)",
			Effect: "影响星点锐度。对广角流星摄影（14~35mm）几乎不可察觉，" +
				"远不如对行星/深空长焦敏感；只在拍摄星野特写时才需要关注。",
			Level:  "低",
			Weight: 4,
			Source: SrcManual + "（本脚本不计算。可参考 meteoblue seeing 预报" +
				"或气象站的高空风切变数据）",
			Reason: "广角焦距下单像素对应角尺度远大于典型 seeing 盘（1~3″），" +
				"故给最低权重；换长焦时应上调。",
		},
	}
}

func ImpactWeightTotal(factors []ImpactFactor) int {
	total := 0
	for _, f := range factors {
		total += f.Weight
	}
	return total
}
