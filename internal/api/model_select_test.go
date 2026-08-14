package api

import "testing"

func TestResolveModel(t *testing.T) {
	cases := []struct {
		name     string
		region   string
		fallback string
		want     string
	}{
		{"日本站点升级到 jma_msm", "jp", "icon_seamless", "jma_msm"},
		{"韩国站点升级到 jma_msm", "kr", "icon_seamless", "jma_msm"},
		{"华东站点沿用默认", "cn", "icon_seamless", "icon_seamless"},
		{"未设区域沿用默认", "", "icon_seamless", "icon_seamless"},
		{"未知区域沿用默认", "us", "icon_seamless", "icon_seamless"},
		{"配置默认非 icon_seamless 也按区域升级", "jp", "jma_gsm", "jma_msm"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveModel(c.region, c.fallback); got != c.want {
				t.Errorf("ResolveModel(%q,%q) = %q, want %q", c.region, c.fallback, got, c.want)
			}
		})
	}
}
