package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
)

type parityRecordIndex struct {
	Records []struct {
		Name            string  `json:"name"`
		Lat             float64 `json:"lat"`
		Lon             float64 `json:"lon"`
		File            string  `json:"file"`
		IncludeOptional bool    `json:"include_optional"`
	} `json:"records"`
}

func loadParityRecords(t *testing.T) (parityRecordIndex, string) {
	t.Helper()

	dir := filepath.Join(moduleRoot(t), ".parity_records")
	raw, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Skipf("没有 .parity_records 录制（%v），跳过真实数据回放守卫", err)
	}

	var idx parityRecordIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		t.Fatalf("解析 .parity_records/index.json 失败：%v", err)
	}
	if len(idx.Records) == 0 {
		t.Skip("录制索引为空，跳过")
	}
	return idx, dir
}

func TestCloudBaseSignContractOnRecordedData(t *testing.T) {
	idx, dir := loadParityRecords(t)

	cfg := config.Default()

	var targetNights map[string]bool

	var (
		totalRows  int
		negAGLRows int
	)

	for _, rec := range idx.Records {
		raw, err := os.ReadFile(filepath.Join(dir, rec.File))
		if err != nil {
			t.Fatalf("读取录制 %s 失败：%v", rec.File, err)
		}

		resp, err := api.ParseResponse(raw, api.BuildHourlyVars(rec.IncludeOptional))
		if err != nil {
			t.Fatalf("[%s] 解析录制响应失败：%v", rec.Name, err)
		}

		site := Site{Name: rec.Name, Lat: rec.Lat, Lon: rec.Lon, Alt: resp.Elevation}

		for _, row := range AnalyseSite(site, resp, targetNights, cfg) {
			if !row.HasData || !row.CloudBaseMSL.Valid || !row.CloudBaseAGL.Valid {
				continue
			}
			totalRows++

			msl := row.CloudBaseMSL.V
			agl := row.CloudBaseAGL.V

			if msl < 0 {
				t.Errorf(`[%s %s] CloudBaseMSL = %.0f < 0：
云底海拔不可能为负。这个字段是**海拔**（MSL），不是相对机位高度；
出现负值说明它被写成了相对量（很可能与 CloudBaseAGL 搞混了）。`,
					rec.Name, row.TimeISO, msl)
			}

			const roundTol = 1.0
			switch {
			case agl < -roundTol && msl > site.Alt+roundTol:
				t.Errorf(`[%s %s] 符号自相矛盾：CloudBaseAGL = %.0f < 0（云底在机位下方），
但 CloudBaseMSL = %.0f > siteAlt = %.0f（云底在机位上方）。
两者必须同号：AGL < 0 ⟺ MSL < siteAlt。
这一般是「符号取反」或「基准搞混（MSL/AGL 互换）」造成的。`,
					rec.Name, row.TimeISO, agl, msl, site.Alt)
			case agl > roundTol && msl < site.Alt-roundTol:
				t.Errorf(`[%s %s] 符号自相矛盾：CloudBaseAGL = %.0f > 0（云底在机位上方），
但 CloudBaseMSL = %.0f < siteAlt = %.0f（云底在机位下方）。`,
					rec.Name, row.TimeISO, agl, msl, site.Alt)
			}

			if agl < 0 {
				negAGLRows++
			}
		}
	}

	if totalRows == 0 {
		t.Fatal("录制数据里一条有效云底记录都没有——" +
			"本守卫等于没跑，请检查录制是否与当前配置匹配")
	}

	if negAGLRows == 0 {
		t.Errorf(`录制数据里没有任何 CloudBaseAGL < 0 的时次（共 %d 条有效记录）。
实测括苍山 310 时次中 262 条为负，云海（机位在云上）是常态。
全为非负说明 AGL 很可能被误钳到了 0，或基准被换成了 MSL。`, totalRows)
	}

	t.Logf("云底符号契约核验通过：%d 条有效记录，其中 %d 条 CloudBaseAGL < 0（云海时次）",
		totalRows, negAGLRows)
}

func TestCloudBaseJSONTagsAreFrozen(t *testing.T) {

	raw, err := json.Marshal(HourRow{})
	if err != nil {
		t.Fatalf("序列化 HourRow 失败：%v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}

	frozen := []string{
		"cloud_base_msl",
		"cloud_base_agl",
		"cloud_top_msl",
		"cloud_top_agl",
		"cloud_thickness",
	}
	for _, key := range frozen {
		if _, ok := got[key]; !ok {
			t.Errorf(`JSON 键 %q 不见了。
model/types.go 里的 tag 被改过，parity 的 compare_json.py 是按这个键名逐字段
对拍的，改名会让 432×39 对拍静默失配（两边都取不到键 → 误判相等）。
展示名（中文表头）想怎么改都行，但 JSON tag 必须原样保留。`, key)
		}
	}
}
