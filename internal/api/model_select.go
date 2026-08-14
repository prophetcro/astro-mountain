package api

// jmaRegionModels 记录需要改用区域模式的站点区域。
// 日韩一带用 JMA MSM 的分辨率与表现优于全球模式默认值。
var jmaRegionModels = map[string]string{
	"jp": "jma_msm",
	"kr": "jma_msm",
}

// ResolveModel 按站点区域挑预报模式，未登记的区域用 fallback。
func ResolveModel(region, fallback string) string {
	if m, ok := jmaRegionModels[region]; ok {
		return m
	}
	return fallback
}
