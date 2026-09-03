package profile

import "github.com/prophetcro/astro-mountain/internal/model"

// 云与机位的相对关系标签，转发自 model。
const (
	REL_CLEAR              = model.REL_CLEAR
	REL_SEA_BELOW          = model.REL_SEA_BELOW
	REL_IN_CLOUD           = model.REL_IN_CLOUD
	REL_OVERHEAD           = model.REL_OVERHEAD
	REL_NODATA             = model.REL_NODATA
	REL_SEA_BELOW_IN_CLOUD = model.REL_SEA_BELOW_IN_CLOUD
)

// ClassifySite 判断机位与云层的相对关系，并给出决定该关系的关键层。
//
// 判定优先级（先命中先返回）：机位夹在某层内 → 头顶有云（取最低的一层）→
// 脚下云海（取最高的一层）→ 无云。没有关键层时第二个返回值为 nil。
//
// 返回的指针指向入参 layers 的元素，调用方不应通过它改写切片内容。
func ClassifySite(siteAlt float64, layers []CloudLayer) (string, *CloudLayer) {
	if len(layers) == 0 {
		return REL_CLEAR, nil
	}
	// 云底/云顶来自插值，机位恰好落在边界上时按「在云中」算，宁可保守。
	const eps = 1e-6

	for i := range layers {
		if layers[i].BaseMSL-eps <= siteAlt && siteAlt <= layers[i].TopMSL+eps {
			return REL_IN_CLOUD, &layers[i]
		}
	}

	// 头顶有云：取云底最低的那层，它最先遮住星空。
	overheadIdx := -1
	for i := range layers {
		if layers[i].BaseMSL > siteAlt {
			if overheadIdx < 0 || layers[i].BaseMSL < layers[overheadIdx].BaseMSL {
				overheadIdx = i
			}
		}
	}
	if overheadIdx >= 0 {
		return REL_OVERHEAD, &layers[overheadIdx]
	}

	// 脚下云海：取云顶最高的那层，它离机位最近、最有可能顶上来。
	beneathIdx := -1
	for i := range layers {
		if layers[i].TopMSL < siteAlt {
			if beneathIdx < 0 || layers[i].TopMSL > layers[beneathIdx].TopMSL {
				beneathIdx = i
			}
		}
	}
	if beneathIdx >= 0 {
		return REL_SEA_BELOW, &layers[beneathIdx]
	}
	return REL_CLEAR, nil
}

// HighestBeneath 返回严格低于机位的最高云顶（米，海拔）。
// 机位下方没有云层时第二个返回值为 false，此时第一个返回值无意义。
func HighestBeneath(siteAlt float64, layers []CloudLayer) (float64, bool) {
	l, ok := HighestBeneathLayer(siteAlt, layers)
	if !ok {
		return 0.0, false
	}
	return l.TopMSL, true
}

// HighestBeneathLayer 返回严格低于机位的云顶最高的那一层。
// 机位下方没有云层时第二个返回值为 false，此时第一个返回值为 nil。
//
// 返回的指针指向入参 layers 的元素，调用方不应通过它改写切片内容。
func HighestBeneathLayer(siteAlt float64, layers []CloudLayer) (*CloudLayer, bool) {
	best, found := -1, false
	for i := range layers {
		if layers[i].TopMSL < siteAlt {
			if !found || layers[i].TopMSL > layers[best].TopMSL {
				best, found = i, true
			}
		}
	}
	if !found {
		return nil, false
	}
	return &layers[best], true
}
