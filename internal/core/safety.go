package core

import (
	"fmt"

	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
)

// ProfileUsable 判断气压层廓线是否可用于评级：至少要有一层。
func ProfileUsable(levels []profile.Level) bool {
	return len(levels) > 0
}

// AllLevelsMissing 判断所有气压层的云量与相对湿度是否全部缺测。
// 只要有任意一层的 CC 或 RH 有效，即返回 false。
func AllLevelsMissing(levelValues map[int]model.RawLevel) bool {
	for _, raw := range levelValues {
		if raw.CC.Valid || raw.RH.Valid {
			return false
		}
	}
	return true
}

// AssertNoDataRow 校验一行无数据记录是否守住了「缺测安全红线」：
// 缺测行只能评为 RATING_NODATA，绝不能被渲染成通透或任何乐观结论。
// 传入 HasData 为 true 的行属调用错误，同样返回 error。
func AssertNoDataRow(row HourRow) error {
	if row.HasData {
		return fmt.Errorf("行 %s@%s 被标记为有数据，不应走无数据校验", row.Site, row.TimeISO)
	}
	if row.Rating != RATING_NODATA {
		return fmt.Errorf("缺测安全红线被破坏：行 %s@%s 无数据却评为 %q（期望 %q）",
			row.Site, row.TimeISO, row.Rating, RATING_NODATA)
	}
	if row.Rating == RATING_CLEAR {
		return fmt.Errorf("缺测安全红线被破坏：行 %s@%s 无数据却评为通透", row.Site, row.TimeISO)
	}
	return nil
}

// AuditRows 逐行自检评级结果的一致性，返回人类可读的问题描述列表；
// 全部合规时返回 nil。缺测行走 AssertNoDataRow，有数据行则要求
// 必须带关系分类、且不得评为无数据。
//
// 这是一道产出前的兜底护栏：问题以警告形式呈现，不中断本次运行。
func AuditRows(rows []HourRow) []string {
	var issues []string
	for i := range rows {
		row := rows[i]
		if !row.HasData {
			if err := AssertNoDataRow(row); err != nil {
				issues = append(issues, err.Error())
			}
			continue
		}

		if !row.Relation.Valid || row.Relation.V == "" {
			issues = append(issues, fmt.Sprintf(
				"行 %s@%s 标记为有数据却没有关系分类", row.Site, row.TimeISO))
		}
		if row.Rating == RATING_NODATA {
			issues = append(issues, fmt.Sprintf(
				"行 %s@%s 标记为有数据却评为无数据", row.Site, row.TimeISO))
		}
	}
	return issues
}

// SafeSpread 计算温度露点差（温度 − 露点）。任一输入缺测时结果即为缺测，
// 不会用 0 顶替。
func SafeSpread(temp, dew OptFloat) OptFloat {
	return model.Sub(temp, dew)
}

// SafeLCL 由温露差估算抬升凝结高度（离地高度，米），系数 124 m/℃。
// spread 缺测时结果为缺测。
func SafeLCL(spread OptFloat) OptFloat {
	return model.Scale(spread, 124.0)
}
