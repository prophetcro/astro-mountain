package menu

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/report"
)

const (
	siteNameMaxRunes = 16
	siteLatMin       = -90.0
	siteLatMax       = 90.0
	siteLonMin       = -180.0
	siteLonMax       = 180.0
	siteAltMin       = -500.0
	siteAltMax       = 9000.0
)

func (s *state) siteManager() error {
	u := s.u
	for {
		u.banner("[3] 点位配置管理")
		enabled := len(s.enabledSites())
		u.printf("  配置文件：%s      （%d 个点位，%d 启用）%s\n",
			s.sitesPath, len(s.sites), enabled, dirtyHint(s.sitesDirty))
		s.printSiteTable()

		u.blank()
		u.info("[a] 新增点位     [e] 编辑点位     [d] 删除点位     [t] 启用/停用")
		u.info("[c] 校验配置     [r] 重新载入     [x] 导出内置默认点位到文件")
		u.info("[s] 保存写回文件(留在本页)   [w] 保存并返回   [b] 返回主菜单")

		pick, err := u.choice("请选择",
			[]string{"a", "e", "d", "t", "c", "r", "s", "w", "x", "b"}, "b", backReturns)
		if err != nil {
			if errors.Is(err, errBack) {
				if lerr := s.leaveSiteManager(); lerr != nil {
					return lerr
				}
				continue
			}
			return err
		}

		var ferr error
		switch pick {
		case "a":
			ferr = s.addSite()
		case "e":
			ferr = s.editSite()
		case "d":
			ferr = s.deleteSite()
		case "t":
			ferr = s.toggleSite()
		case "c":
			s.validateAllSites()
		case "r":
			ferr = s.reloadSites()
		case "s":
			ferr = s.saveSites()
		case "w":

			if serr := s.saveSites(); serr != nil {
				if isTerminal(serr) {
					return serr
				}

				u.printf("  ✗ %v\n", serr)
				continue
			}
			if s.sitesDirty {

				if lerr := s.leaveSiteManager(); lerr != nil {
					return lerr
				}
				continue
			}
			return errBack
		case "x":
			ferr = s.exportDefaultSites()
		case "b":

			if lerr := s.leaveSiteManager(); lerr != nil {
				return lerr
			}
			continue
		}
		switch {
		case ferr == nil, errors.Is(ferr, errBack):
			continue
		case isTerminal(ferr):
			return ferr
		default:
			u.printf("  ✗ %v\n", ferr)
		}
	}
}

func dirtyHint(dirty bool) string {
	if dirty {
		return "  ← 有未保存修改"
	}
	return ""
}

func (s *state) leaveSiteManager() error {
	if !s.sitesDirty {
		return errBack
	}
	s.u.blank()
	s.u.warn("有未保存的点位修改，返回后仍保留在内存中，但退出程序会丢失。")

	pick, err := s.u.choice("现在保存到文件吗？（y/s=保存并返回 n=先不保存 b=留在本页）",
		[]string{"y", "s", "n", "b"}, "y", backLiteral)
	if err != nil {
		return err
	}
	switch pick {
	case "y", "s":
		if serr := s.saveSites(); serr != nil {
			return serr
		}
		return errBack
	case "n":
		return errBack
	default:
		s.u.info("已留在点位配置管理页")
		return nil
	}
}

func (s *state) printSiteTable() {
	if len(s.sites) == 0 {
		s.u.blank()
		s.u.warn("当前没有任何点位，请用 [a] 新增或 [x] 导出内置默认点位")
		return
	}
	rows := make([][]string, 0, len(s.sites))
	for i, site := range s.sites {
		state := "否"
		if site.IsEnabled() {
			state = "是"
		}
		rows = append(rows, []string{
			strconv.Itoa(i + 1),
			site.Name,
			fmt.Sprintf("%.4f", site.Lat),
			fmt.Sprintf("%.4f", site.Lon),
			fmt.Sprintf("%.1f", site.Alt),
			state,
			ClipWidth(site.Note, 28),
		})
	}
	s.u.blank()
	s.u.table("   ",
		[]string{"序号", "点位", "纬度", "经度", "海拔(m)", "启用", "备注"},
		[]string{report.AlignRight, report.AlignLeft, report.AlignRight,
			report.AlignRight, report.AlignRight, report.AlignLeft, report.AlignLeft},
		rows)
}

func (s *state) addSite() error {
	u := s.u
	u.blank()
	u.println(" ── 新增点位 ──")

	name, err := u.askText(
		fmt.Sprintf("点位名称  1~%d 字符，不可与现有重复", siteNameMaxRunes), "",
		func(v string) error { return s.validateSiteName(v, -1) })
	if err != nil {
		return err
	}
	lat, err := u.askFloat("纬度      -90 ~ 90，建议 4 位小数以上", siteLatMin, siteLatMax, "")
	if err != nil {
		return err
	}
	lon, err := u.askFloat("经度      -180 ~ 180", siteLonMin, siteLonMax, "")
	if err != nil {
		return err
	}
	alt, err := u.askFloat("海拔(米)  -500 ~ 9000，填主峰海拔(MSL)", siteAltMin, siteAltMax, "")
	if err != nil {
		return err
	}
	note, err := u.askOptionalText("备注      可选，回车跳过", "")
	if err != nil {
		return err
	}
	on, err := u.confirm("启用该点位？", true)
	if err != nil {
		return err
	}

	site := config.Site{Name: name, Lat: lat, Lon: lon, Alt: alt, Note: note}
	site.Enabled = boolPtr(on)

	if verr := config.ValidateSite(site); verr != nil {
		u.fail(verr.Error())
		return nil
	}

	u.blank()
	u.info(fmt.Sprintf("待新增：%s  (%.4f, %.4f)  %.1f m  启用=%s",
		site.Name, site.Lat, site.Lon, site.Alt, yesNo(on)))
	yes, err := u.confirm("确认新增该点位？", true)
	if err != nil {
		return err
	}
	if !yes {
		u.info("已取消")
		return nil
	}
	s.sites = append(s.sites, site)
	s.sitesDirty = true
	u.ok(fmt.Sprintf("已加入内存列表（共 %d 个）。注意：需选 [s] 或 [w] 才会写回文件。", len(s.sites)))
	return nil
}

func (s *state) editSite() error {
	u := s.u
	idx, err := s.pickSiteIndex("请输入要编辑的点位序号")
	if err != nil {
		return err
	}
	cur := s.sites[idx]

	u.blank()
	u.println(" ── 编辑点位（直接回车保留原值）──")

	name, err := u.askText("点位名称", cur.Name,
		func(v string) error { return s.validateSiteName(v, idx) })
	if err != nil {
		return err
	}
	lat, err := u.askFloat("纬度", siteLatMin, siteLatMax, fmt.Sprintf("%.6f", cur.Lat))
	if err != nil {
		return err
	}
	lon, err := u.askFloat("经度", siteLonMin, siteLonMax, fmt.Sprintf("%.6f", cur.Lon))
	if err != nil {
		return err
	}
	alt, err := u.askFloat("海拔(米)", siteAltMin, siteAltMax, fmt.Sprintf("%.1f", cur.Alt))
	if err != nil {
		return err
	}
	note, err := u.askOptionalText("备注（回车保留原值，输入 - 清空）", cur.Note)
	if err != nil {
		return err
	}
	if note == "-" {
		note = ""
	}
	on, err := u.confirm("启用该点位？", cur.IsEnabled())
	if err != nil {
		return err
	}

	updated := config.Site{Name: name, Lat: lat, Lon: lon, Alt: alt, Note: note}
	updated.Enabled = boolPtr(on)
	if verr := config.ValidateSite(updated); verr != nil {
		u.fail(verr.Error())
		return nil
	}
	s.sites[idx] = updated
	s.sitesDirty = true
	u.ok(fmt.Sprintf("已更新点位 %s（内存态，需 [s] 或 [w] 才写回文件）", updated.Name))
	return nil
}

func (s *state) deleteSite() error {
	u := s.u
	idx, err := s.pickSiteIndex("请输入要删除的点位序号")
	if err != nil {
		return err
	}
	target := s.sites[idx]

	u.blank()
	u.warn(fmt.Sprintf("即将删除：%s  (%.4f, %.4f)  %.1f m",
		target.Name, target.Lat, target.Lon, target.Alt))

	yes, err := u.confirm(fmt.Sprintf("确定删除点位「%s」？此操作不可撤销", target.Name), false)
	if err != nil {
		return err
	}
	if !yes {
		u.info("已取消删除")
		return nil
	}
	s.sites = append(s.sites[:idx], s.sites[idx+1:]...)
	s.sitesDirty = true
	u.ok(fmt.Sprintf("已从内存列表删除（剩余 %d 个）。需选 [s] 或 [w] 才会写回文件。", len(s.sites)))
	return nil
}

func (s *state) toggleSite() error {
	u := s.u
	idx, err := s.pickSiteIndex("请输入要切换启用状态的点位序号")
	if err != nil {
		return err
	}
	now := !s.sites[idx].IsEnabled()
	s.sites[idx].Enabled = boolPtr(now)
	s.sitesDirty = true
	u.ok(fmt.Sprintf("点位「%s」已%s", s.sites[idx].Name, enableWord(now)))
	return nil
}

func enableWord(on bool) string {
	if on {
		return "启用"
	}
	return "停用"
}

func (s *state) validateAllSites() {
	u := s.u
	u.blank()
	var problems []string
	seen := make(map[string]int, len(s.sites))
	for i, site := range s.sites {
		if err := config.ValidateSite(site); err != nil {
			problems = append(problems, fmt.Sprintf("第 %d 条：%v", i+1, err))
		}
		if prev, dup := seen[site.Name]; dup {
			problems = append(problems,
				fmt.Sprintf("第 %d 条：点位名「%s」与第 %d 条重复", i+1, site.Name, prev+1))
		} else {
			seen[site.Name] = i
		}
	}
	if len(s.enabledSites()) == 0 {
		problems = append(problems, "没有任何启用的点位，评估流程将无法执行")
	}
	if len(problems) == 0 {
		u.ok(fmt.Sprintf("配置校验通过（%d 个点位，%d 启用）",
			len(s.sites), len(s.enabledSites())))
		return
	}
	u.printf("  ✗ 发现 %d 个问题：\n", len(problems))
	for _, p := range problems {
		u.info("  · " + p)
	}
}

func (s *state) reloadSites() error {
	u := s.u
	if s.sitesDirty {
		yes, err := u.confirm("有未保存的修改，重新载入会丢弃它们。确定继续？", false)
		if err != nil {
			return err
		}
		if !yes {
			u.info("已取消")
			return nil
		}
	}
	res, err := config.LoadSites(s.opts.SitesPath)
	if err != nil {
		return fmt.Errorf("重新载入失败：%w", err)
	}
	s.sites = append([]config.Site(nil), res.Sites...)
	s.sitesSrc = res.Source
	s.sitesDirty = false
	for _, w := range res.Warnings {
		u.warn(w)
	}
	u.ok(fmt.Sprintf("已从 %s 重新载入 %d 个点位", res.Source, len(res.Sites)))
	return nil
}

func (s *state) saveSites() error {
	u := s.u
	if len(s.sites) == 0 {
		u.fail("点位列表为空，拒绝写回（否则会把配置清空）")
		return nil
	}

	for i, site := range s.sites {
		if err := config.ValidateSite(site); err != nil {
			u.fail(fmt.Sprintf("第 %d 条点位不合法，已中止保存：%v", i+1, err))
			return nil
		}
	}
	path := s.sitesPath
	u.blank()
	u.info("将写回：" + path)
	u.info("（写入前会自动把原文件备份为 " + path + ".bak）")
	yes, err := u.confirm("确认保存？", true)
	if err != nil {
		return err
	}
	if !yes {
		u.info("已取消保存")
		return nil
	}
	if serr := config.SaveSites(s.sites, path); serr != nil {
		return fmt.Errorf("保存失败：%w", serr)
	}
	s.sitesDirty = false
	s.sitesSrc = path
	u.ok(fmt.Sprintf("已保存 %d 个点位到 %s", len(s.sites), path))
	return nil
}

func (s *state) exportDefaultSites() error {
	u := s.u
	u.blank()
	u.info("把内置的 20 个默认点位导出到文件，用于把配置改坏后一键恢复。")
	path, err := u.askText("导出到", s.sitesPath, nil)
	if err != nil {
		return err
	}
	yes, err := u.confirm("目标文件若已存在将被覆盖（原文件会备份为 .bak）。确认导出？", false)
	if err != nil {
		return err
	}
	if !yes {
		u.info("已取消")
		return nil
	}
	defaults := config.DefaultSites()
	if serr := config.SaveSites(defaults, path); serr != nil {
		return fmt.Errorf("导出失败：%w", serr)
	}
	u.ok(fmt.Sprintf("已导出 %d 个内置默认点位到 %s", len(defaults), path))

	yes, err = u.confirm("是否立刻把它载入为当前点位列表？", true)
	if err != nil {
		return err
	}
	if yes {
		s.sites = append([]config.Site(nil), defaults...)
		s.sitesSrc = path
		s.sitesPath = path
		s.sitesDirty = false
		u.ok("已载入")
	}
	return nil
}

func (s *state) pickSiteIndex(label string) (int, error) {
	if len(s.sites) == 0 {
		s.u.fail("当前没有任何点位")
		return 0, errBack
	}
	n, err := s.u.askInt(fmt.Sprintf("%s  1~%d", label, len(s.sites)), 1, len(s.sites), 0, false)
	if err != nil {
		return 0, err
	}
	return n - 1, nil
}

func (s *state) validateSiteName(name string, excludeIdx int) error {
	name = strings.TrimSpace(name)
	runes := []rune(name)
	if len(runes) == 0 {
		return fmt.Errorf("点位名不能为空")
	}
	if len(runes) > siteNameMaxRunes {
		return fmt.Errorf("点位名 %q 超过 %d 字符", name, siteNameMaxRunes)
	}
	for i, site := range s.sites {
		if i == excludeIdx {
			continue
		}
		if site.Name == name {
			return fmt.Errorf("点位名「%s」已存在（第 %d 条）", name, i+1)
		}
	}
	return nil
}

func boolPtr(v bool) *bool { return &v }

func yesNo(v bool) string {
	if v {
		return "是"
	}
	return "否"
}
