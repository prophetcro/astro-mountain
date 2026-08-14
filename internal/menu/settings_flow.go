package menu

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/report"
)

type settingItem struct {
	group string
	label string
	show  func(c *config.Config) string
	edit  func(u *ui, c *config.Config) error
}

func (s *state) settingsFlow() error {
	u := s.u
	items := settingItems()

	for {
		u.banner("[4] 运行参数设置")
		u.printf("  配置文件：%s%s\n", s.configPath, dirtyHint(s.cfgDirty))

		lastGroup := ""
		for i, it := range items {
			if it.group != lastGroup {
				u.blank()
				u.printf("  ── %s ──\n", it.group)
				lastGroup = it.group
			}
			u.printf("   [%s] %s %s\n",
				report.Pad(strconv.Itoa(i+1), 2, report.AlignRight),
				report.Pad(it.label, 22, report.AlignLeft),
				it.show(&s.cfg))
		}

		u.blank()
		u.info("[a] 保存并返回      [s] 保存到文件(留在本页)    [o] 仅本次生效")
		u.info("[r] 恢复默认值      [b] 放弃修改并返回")

		text, err := u.prompt(fmt.Sprintf("请选择要修改的项 1~%d，或 a/s/o/r/b", len(items)),
			"b", backReturns)
		if err != nil {
			if errors.Is(err, errBack) {
				if lerr := s.leaveSettings(); lerr != nil {
					return lerr
				}
				continue
			}
			return err
		}

		switch strings.ToLower(text) {
		case "b":

			if lerr := s.leaveSettings(); lerr != nil {
				return lerr
			}
			continue
		case "o":
			u.blank()
			u.ok("已保留内存中的修改，仅本次运行生效（未写入文件）")
			return errBack
		case "a":

			if serr := s.saveConfig(); serr != nil {
				if isTerminal(serr) {
					return serr
				}

				u.printf("  ✗ %v\n", serr)
				continue
			}
			if s.cfgDirty {

				if lerr := s.leaveSettings(); lerr != nil {
					return lerr
				}
				continue
			}
			return errBack
		case "s":
			if serr := s.saveConfig(); serr != nil {
				if isTerminal(serr) {
					return serr
				}
				u.printf("  ✗ %v\n", serr)
			}
			continue
		case "r":
			yes, cerr := u.confirm("确定把全部运行参数恢复为内置默认值？", false)
			if cerr != nil {
				return cerr
			}
			if yes {
				src := s.cfg.Source
				s.cfg = config.Default()
				s.cfg.Source = src
				s.cfgDirty = true
				s.fontPath, s.fontErr = s.probeFont(s.cfg.Douyin.FontPath)
				u.ok("已恢复内置默认值（内存态，需 [a] 或 [s] 才写回文件）")
			}
			continue
		}

		idx, cerr := strconv.Atoi(text)
		if cerr != nil || idx < 1 || idx > len(items) {
			u.fail(fmt.Sprintf("%q 不是可选项（1~%d / a / s / o / r / b）", text, len(items)))
			continue
		}
		before := items[idx-1].show(&s.cfg)
		eerr := items[idx-1].edit(u, &s.cfg)
		switch {
		case eerr == nil:
			if after := items[idx-1].show(&s.cfg); after != before {
				s.cfgDirty = true
				u.ok(fmt.Sprintf("%s：%s → %s", items[idx-1].label, before, after))

				if strings.Contains(items[idx-1].label, "字体") {
					s.fontPath, s.fontErr = s.probeFont(s.cfg.Douyin.FontPath)
					if s.fontErr != nil {
						u.warn("字体仍不可用：" + firstLine(s.fontErr.Error()))
					} else {
						u.ok("字体探测成功：" + s.fontPath)
					}
				}
			}
		case errors.Is(eerr, errBack):

		case isTerminal(eerr):
			return eerr
		default:
			u.printf("  ✗ %v\n", eerr)
		}
	}
}

func (s *state) leaveSettings() error {
	if !s.cfgDirty {
		return errBack
	}
	s.u.blank()
	s.u.warn("运行参数有未保存的修改。")

	pick, err := s.u.choice("如何处理？（y/s=保存并返回 o=仅本次生效 d=放弃修改 b=留在本页）",
		[]string{"y", "s", "o", "d", "b"}, "y", backLiteral)
	if err != nil {
		return err
	}
	switch pick {
	case "y", "s":
		if serr := s.saveConfig(); serr != nil {
			return serr
		}
		return errBack
	case "o":
		s.u.ok("已保留在内存中，仅本次运行生效")
		return errBack
	case "d":
		cfg, lerr := config.Load(s.opts.ConfigPath)
		if lerr != nil {
			s.u.warn("重新载入配置失败，保留当前内存值：" + lerr.Error())
			return errBack
		}
		s.cfg = cfg
		s.cfgDirty = false
		s.fontPath, s.fontErr = s.probeFont(s.cfg.Douyin.FontPath)
		s.u.ok("已放弃修改，恢复为文件中的值")
		return errBack
	default:
		s.u.info("已留在参数设置页")
		return nil
	}
}

func (s *state) saveConfig() error {
	u := s.u
	path := s.configPath
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
	if serr := config.Save(s.cfg, path); serr != nil {
		return fmt.Errorf("保存配置失败：%w", serr)
	}
	s.cfgDirty = false
	s.cfg.Source = path
	u.ok("已保存到 " + path)
	return nil
}

func settingItems() []settingItem {
	return []settingItem{

		{
			group: "气象数据",
			label: "气象模式",
			show: func(c *config.Config) string {
				if c.API.CrossModel == "" {
					return c.API.Models
				}
				return fmt.Sprintf("%s + %s 交叉", c.API.Models, c.API.CrossModel)
			},
			edit: func(u *ui, c *config.Config) error {
				v, err := u.askChoice("气象模式", c.API.Models, modelOptions())
				if err != nil {
					return err
				}
				c.API.Models = v

				items := []checkboxItem{
					{Label: "同时用 GFS 交叉验证", Checked: c.API.CrossModel != ""},
				}
				checked, cerr := u.askCheckbox("双模型交叉对比", items)
				if cerr != nil {
					return cerr
				}
				if checked["同时用 GFS 交叉验证"] {
					if c.API.Models == "gfs_seamless" {
						c.API.CrossModel = "icon_seamless"
					} else {
						c.API.CrossModel = "gfs_seamless"
					}
				} else {
					c.API.CrossModel = ""
				}
				return nil
			},
		},
		{
			group: "气象数据",
			label: "HTTP 缓存",
			show: func(c *config.Config) string {
				return fmt.Sprintf("%s / %d 秒", onOff(c.API.CacheEnabled), c.API.CacheExpireS)
			},
			edit: func(u *ui, c *config.Config) error {
				on, err := u.confirm("启用磁盘缓存？", c.API.CacheEnabled)
				if err != nil {
					return err
				}
				c.API.CacheEnabled = on
				if !on {
					return nil
				}
				sec, err := u.askInt("缓存有效期（秒）60 ~ 86400", 60, 86400, c.API.CacheExpireS, true)
				if err != nil {
					return err
				}
				c.API.CacheExpireS = sec
				return nil
			},
		},
		{
			group: "气象数据",
			label: "重试次数 / 退避因子",
			show: func(c *config.Config) string {
				return fmt.Sprintf("%d / %.1f", c.API.Retries, c.API.BackoffFactor)
			},
			edit: func(u *ui, c *config.Config) error {
				n, err := u.askInt("重试次数 0 ~ 10", 0, 10, c.API.Retries, true)
				if err != nil {
					return err
				}
				f, err := u.askFloat("退避因子 0 ~ 5", 0, 5,
					strconv.FormatFloat(c.API.BackoffFactor, 'f', -1, 64))
				if err != nil {
					return err
				}
				c.API.Retries, c.API.BackoffFactor = n, f
				return nil
			},
		},

		{
			group: "时间窗口（北京时间）",
			label: "夜间窗口",
			show: func(c *config.Config) string {
				return fmt.Sprintf("%02d:00 – %02d:00", c.Window.NightStartHour, c.Window.NightEndHour)
			},
			edit: func(u *ui, c *config.Config) error {
				a, err := u.askInt("夜间起始小时 0 ~ 23", 0, 23, c.Window.NightStartHour, true)
				if err != nil {
					return err
				}
				b, err := u.askInt("夜间结束小时 0 ~ 23（次日）", 0, 23, c.Window.NightEndHour, true)
				if err != nil {
					return err
				}
				c.Window.NightStartHour, c.Window.NightEndHour = a, b
				return nil
			},
		},
		{
			group: "时间窗口（北京时间）",
			label: "核心窗口",
			show: func(c *config.Config) string {
				return fmt.Sprintf("%02d:00 – %02d:00", c.Window.CoreStartHour, c.Window.CoreEndHour)
			},
			edit: func(u *ui, c *config.Config) error {
				a, err := u.askInt("核心起始小时 0 ~ 23", 0, 23, c.Window.CoreStartHour, true)
				if err != nil {
					return err
				}
				b, err := u.askInt("核心结束小时 0 ~ 23（次日）", 0, 23, c.Window.CoreEndHour, true)
				if err != nil {
					return err
				}
				c.Window.CoreStartHour, c.Window.CoreEndHour = a, b
				return nil
			},
		},

		{
			group: "云 / 雾判据阈值",
			label: "云量阈值",
			show:  func(c *config.Config) string { return fmt.Sprintf("%.0f %%", c.Thresh.CloudCoverThreshold) },
			edit: func(u *ui, c *config.Config) error {
				v, err := u.askFloat("云量阈值（%）0 ~ 100", 0, 100,
					strconv.FormatFloat(c.Thresh.CloudCoverThreshold, 'f', -1, 64))
				if err != nil {
					return err
				}
				c.Thresh.CloudCoverThreshold = v
				return nil
			},
		},
		{
			group: "云 / 雾判据阈值",
			label: "雾 / 轻雾能见度",
			show: func(c *config.Config) string {
				return fmt.Sprintf("%.0f m / %.0f m", c.Thresh.FogVisibilityM, c.Thresh.HazeVisibilityM)
			},
			edit: func(u *ui, c *config.Config) error {
				fog, err := u.askFloat("雾能见度阈值（米）0 ~ 20000", 0, 20000,
					strconv.FormatFloat(c.Thresh.FogVisibilityM, 'f', -1, 64))
				if err != nil {
					return err
				}
				haze, err := u.askFloat("轻雾能见度阈值（米）0 ~ 50000", 0, 50000,
					strconv.FormatFloat(c.Thresh.HazeVisibilityM, 'f', -1, 64))
				if err != nil {
					return err
				}
				c.Thresh.FogVisibilityM, c.Thresh.HazeVisibilityM = fog, haze
				return nil
			},
		},
		{
			group: "云 / 雾判据阈值",
			label: "头顶云严重阈值",
			show:  func(c *config.Config) string { return fmt.Sprintf("%.0f %%", c.Thresh.OverheadSevereCC) },
			edit: func(u *ui, c *config.Config) error {
				v, err := u.askFloat("头顶云判「不宜」的云量阈值（%）0 ~ 100", 0, 100,
					strconv.FormatFloat(c.Thresh.OverheadSevereCC, 'f', -1, 64))
				if err != nil {
					return err
				}
				c.Thresh.OverheadSevereCC = v
				return nil
			},
		},
		{
			group: "云 / 雾判据阈值",
			label: "温露差结露提示",
			show:  func(c *config.Config) string { return fmt.Sprintf("%.1f ℃", c.Thresh.DewSpreadC) },
			edit: func(u *ui, c *config.Config) error {
				v, err := u.askFloat("温露差结露提示阈值（℃）0 ~ 20", 0, 20,
					strconv.FormatFloat(c.Thresh.DewSpreadC, 'f', -1, 64))
				if err != nil {
					return err
				}
				c.Thresh.DewSpreadC = v
				return nil
			},
		},

		{
			group: "输出",
			label: "输出目录",
			show:  func(c *config.Config) string { return c.Output.OutDir },
			edit: func(u *ui, c *config.Config) error {
				v, err := u.askText("产物输出目录", c.Output.OutDir, nil)
				if err != nil {
					return err
				}
				c.Output.OutDir = v
				return nil
			},
		},
		{
			group: "输出",
			label: "抖音图输出目录",
			show:  func(c *config.Config) string { return c.Output.DouyinDir },
			edit: func(u *ui, c *config.Config) error {
				v, err := u.askText("抖音图输出目录", c.Output.DouyinDir, nil)
				if err != nil {
					return err
				}
				c.Output.DouyinDir = v
				return nil
			},
		},
		{
			group: "输出",
			label: "默认天数",
			show:  func(c *config.Config) string { return fmt.Sprintf("%d 天", c.Output.DefaultDays) },
			edit: func(u *ui, c *config.Config) error {
				v, err := u.askInt("极大日往前推的默认天数 0 ~ 16", 0, 16, c.Output.DefaultDays, true)
				if err != nil {
					return err
				}
				c.Output.DefaultDays = v
				return nil
			},
		},
		{
			group: "输出",
			label: "报告后自动出图",
			show:  func(c *config.Config) string { return onOff(c.Output.AutoDouyin) },
			edit: func(u *ui, c *config.Config) error {
				v, err := u.confirm("生成报告后自动渲染抖音图？", c.Output.AutoDouyin)
				if err != nil {
					return err
				}
				c.Output.AutoDouyin = v
				return nil
			},
		},
		{
			group: "输出",
			label: "默认导出 CSV",
			show:  func(c *config.Config) string { return onOff(c.Output.ExportCSV) },
			edit: func(u *ui, c *config.Config) error {
				v, err := u.confirm("默认导出 CSV 明细？", c.Output.ExportCSV)
				if err != nil {
					return err
				}
				c.Output.ExportCSV = v
				return nil
			},
		},
		{
			group: "输出",
			label: "默认导出 JSON",
			show:  func(c *config.Config) string { return onOff(c.Output.ExportJSON) },
			edit: func(u *ui, c *config.Config) error {
				v, err := u.confirm("默认导出 JSON 明细？", c.Output.ExportJSON)
				if err != nil {
					return err
				}
				c.Output.ExportJSON = v
				return nil
			},
		},
		{
			group: "输出",
			label: "中文字体路径",
			show: func(c *config.Config) string {
				if strings.TrimSpace(c.Douyin.FontPath) == "" {
					return "（自动探测）"
				}
				return c.Douyin.FontPath
			},
			edit: func(u *ui, c *config.Config) error {
				u.info("留空表示按内置候选表自动探测；输入 - 可清空当前设置。")
				v, err := u.askOptionalText("字体文件路径（.ttf / .ttc / .otf）", c.Douyin.FontPath)
				if err != nil {
					return err
				}
				v = strings.TrimSpace(v)
				if v == "-" {
					c.Douyin.FontPath = ""
					return nil
				}
				if v == "" {
					c.Douyin.FontPath = ""
					return nil
				}
				if verr := validateFontFile(v); verr != nil {
					u.fail(verr.Error())
					return errBack
				}
				c.Douyin.FontPath = v
				return nil
			},
		},
	}
}
