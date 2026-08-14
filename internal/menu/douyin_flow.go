package menu

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/report"
)

type reportFile struct {
	Path    string
	Name    string
	ModTime time.Time
	Size    int64
}

const maxListedReports = 10

func (s *state) douyinFlow() error {
	u := s.u
	u.banner("[2] 仅生成抖音图片")

	mdPath, err := s.pickReport()
	if err != nil {
		return err
	}

	sections, err := s.pickSections()
	if err != nil {
		return err
	}

	outDir, fontPath, err := s.pickOutputAndFont()
	if err != nil {
		return err
	}

	u.blank()
	u.println(" ── 确认 " + report.Repeat("─", boxWidth-6))
	u.printf("   报告   %s\n", mdPath)
	u.printf("   小节   %s\n", strings.ReplaceAll(sections, ",", " / "))
	u.printf("   输出   %s\n", outDir)
	switch {
	case fontPath != "":
		u.printf("   字体   %s（配置指定）\n", fontPath)
	case s.fontPath != "":
		u.printf("   字体   %s（自动探测）\n", s.fontPath)
	default:
		u.printf("   字体   自动探测\n")
	}
	u.println(" " + report.Repeat("─", boxWidth+1))
	u.info("Y=执行   n=重选   b=返回主菜单")

	pick, err := u.choice("确认执行？", []string{"Y", "n", "b"}, "Y", backReturns)
	if err != nil {
		return err
	}
	switch pick {
	case "b":
		return errBack
	case "n":
		return s.douyinFlow()
	}

	u.blank()
	u.info("正在渲染抖音竖版图…（首次加载字体需要一两秒）")
	started := time.Now()
	paths, rerr := s.renderReport(mdPath, outDir, sections, fontPath)
	u.blank()
	if rerr != nil {
		u.printf("  ✗ 出图失败：%v\n", rerr)
		u.info("  常见原因：中文字体不可用（到 [4] 指定 douyin.font_path）、报告里没有匹配的小节")
		return u.pause()
	}
	u.printf("  执行完毕，耗时 %.1f 秒\n", time.Since(started).Seconds())
	u.blank()
	u.println(" ── 产出 " + report.Repeat("─", boxWidth-6))
	u.printf("   共 %d 张，输出目录 %s\n", len(paths), outDir)
	for i, p := range paths {
		if i >= 8 {
			u.printf("     …（其余 %d 张省略）\n", len(paths)-8)
			break
		}
		u.printf("     %s\n", filepath.Base(p))
	}
	if len(paths) == 0 {
		u.warn("报告中没有匹配到任何可渲染的小节，请检查小节选择")
	}
	return u.pause()
}

func (s *state) pickReport() (string, error) {
	u := s.u
	u.step("步骤 1/3：选择报告")

	dirs := s.reportSearchDirs()
	files := scanReports(dirs)

	if len(files) == 0 {
		u.warn("未在以下目录找到任何 astro_report_*.md：")
		for _, d := range dirs {
			u.info("  · " + d)
		}
		u.info("请先执行 [1] 生成评估报告，或在下一步手动输入报告路径。")
		yes, err := u.confirm("手动输入报告路径？", false)
		if err != nil {
			return "", err
		}
		if !yes {
			return "", errBack
		}
		return s.askReportPath()
	}

	if len(files) > maxListedReports {
		files = files[:maxListedReports]
	}
	rows := make([][]string, 0, len(files))
	for i, f := range files {
		tag := ""
		if i == 0 {
			tag = "← 最新"
		}
		rows = append(rows, []string{
			fmt.Sprintf("[%d]", i+1),
			f.Name,
			f.ModTime.Format("2006-01-02 15:04"),
			fmt.Sprintf("%.1f KB", float64(f.Size)/1024),
			tag,
		})
	}
	u.info(fmt.Sprintf("按修改时间倒序列出最近 %d 份报告：", len(files)))
	u.blank()
	u.table("   ",
		[]string{"序号", "文件名", "修改时间", "大小", ""},
		[]string{report.AlignRight, report.AlignLeft, report.AlignLeft,
			report.AlignRight, report.AlignLeft},
		rows)
	u.blank()
	u.info("[p] 手动输入报告路径     [b] 返回主菜单")

	valid := make([]string, 0, len(files)+2)
	for i := range files {
		valid = append(valid, fmt.Sprintf("%d", i+1))
	}
	valid = append(valid, "p", "b")

	pick, err := u.choice("请选择", valid, "1", backReturns)
	if err != nil {
		return "", err
	}
	switch pick {
	case "b":
		return "", errBack
	case "p":
		return s.askReportPath()
	}
	idx := 0
	fmt.Sscanf(pick, "%d", &idx)
	if idx < 1 || idx > len(files) {
		return "", errBack
	}
	chosen := files[idx-1]
	u.ok("已选报告：" + chosen.Path)
	return chosen.Path, nil
}

func (s *state) askReportPath() (string, error) {
	u := s.u
	for tries := 0; tries < maxInvalidTries; tries++ {
		path, err := u.askText("报告路径（.md 文件）", "", nil)
		if err != nil {
			return "", err
		}
		info, serr := os.Stat(path)
		if serr != nil {
			u.fail("文件不存在或不可读：" + path)
			continue
		}
		if info.IsDir() {
			u.fail(path + " 是一个目录，请指定 .md 文件")
			continue
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			u.fail("扩展名必须是 .md")
			continue
		}
		if !looksLikeReport(path) {
			u.warn("该文件不像本工具生成的报告（正文未找到「低云海拔评估」标题）")
			yes, cerr := u.confirm("仍要继续？", false)
			if cerr != nil {
				return "", cerr
			}
			if !yes {
				continue
			}
		}
		u.ok("已选报告：" + path)
		return path, nil
	}
	u.warn("输入多次无效，已返回上级")
	return "", errBack
}

func looksLikeReport(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	head := string(buf[:n])
	return strings.Contains(head, "低云海拔") || strings.Contains(head, "山地星空") ||
		strings.Contains(head, "astro_report")
}

func (s *state) reportSearchDirs() []string {
	cands := []string{s.outDir(), "reports", "./reports"}
	if d := filepath.Dir(s.douyinDir()); d != "" && d != "." {
		cands = append(cands, d)
	}
	seen := make(map[string]bool, len(cands))
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		abs, err := filepath.Abs(c)
		if err != nil {
			abs = c
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, c)
	}
	return out
}

func scanReports(dirs []string) []reportFile {
	var files []reportFile
	seen := make(map[string]bool)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasPrefix(name, "astro_report") ||
				!strings.EqualFold(filepath.Ext(name), ".md") {
				continue
			}
			full := filepath.Join(dir, name)
			abs, aerr := filepath.Abs(full)
			if aerr != nil {
				abs = full
			}
			if seen[abs] {
				continue
			}
			info, ierr := e.Info()
			if ierr != nil {
				continue
			}
			seen[abs] = true
			files = append(files, reportFile{
				Path:    full,
				Name:    name,
				ModTime: info.ModTime(),
				Size:    info.Size(),
			})
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].ModTime.After(files[j].ModTime)
	})
	return files
}

func (s *state) pickSections() (string, error) {
	u := s.u
	u.step("步骤 2/3：渲染小节")

	labels := append([]string(nil), s.cfg.Douyin.Sections...)
	if len(labels) == 0 {
		labels = []string{"点位列表", "天文条件", "核心窗口", "低云海拔评估明细"}
	}
	hints := make([]string, len(labels))
	for i, l := range labels {
		if strings.Contains(l, "明细") {
			hints[i] = "（按夜自动拆图 + 分页）"
		}
	}
	selected := make([]bool, len(labels))
	for i := range selected {
		selected[i] = true
	}
	if err := u.multiSelect(labels, hints, selected, true, "请至少选择一个小节"); err != nil {
		return "", err
	}
	var picked []string
	for i, on := range selected {
		if on {
			picked = append(picked, labels[i])
		}
	}
	u.ok("小节：" + strings.Join(picked, " / "))
	return strings.Join(picked, ","), nil
}

func (s *state) pickOutputAndFont() (outDir, fontPath string, err error) {
	u := s.u
	u.step("步骤 3/3：输出与字体")

	outDir, err = u.askText("输出目录", s.douyinDir(), nil)
	if err != nil {
		if errors.Is(err, errBack) {
			return "", "", errBack
		}
		return "", "", err
	}

	fontPath = strings.TrimSpace(s.cfg.Douyin.FontPath)
	switch {
	case s.fontErr != nil:
		u.warn("中文字体探测失败：" + firstLine(s.fontErr.Error()))
		u.info("  没有可用中文字体时出图会渲染出豆腐块，建议先指定一个字体文件。")
		yes, cerr := u.confirm("现在手动指定字体路径？", true)
		if cerr != nil {
			return "", "", cerr
		}
		if yes {
			p, aerr := u.askText("字体文件路径（.ttf / .ttc / .otf）", fontPath, validateFontFile)
			if aerr != nil {
				if errors.Is(aerr, errBack) {
					return "", "", errBack
				}
				return "", "", aerr
			}
			fontPath = p
		}
	default:
		shown := s.fontPath
		if shown == "" {
			shown = "（自动探测）"
		}
		u.info("中文字体  " + shown)
		yes, cerr := u.confirm("是否改用其它字体？", false)
		if cerr != nil {
			return "", "", cerr
		}
		if yes {
			p, aerr := u.askText("字体文件路径（.ttf / .ttc / .otf）", fontPath, validateFontFile)
			if aerr != nil {
				if errors.Is(aerr, errBack) {
					return "", "", errBack
				}
				return "", "", aerr
			}
			fontPath = p
		}
	}
	return outDir, fontPath, nil
}

func validateFontFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("文件不存在或不可读：%s", path)
	}
	if info.IsDir() {
		return fmt.Errorf("%s 是一个目录，请指定字体文件", path)
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".ttf", ".ttc", ".otf":
		return nil
	default:
		return fmt.Errorf("扩展名 %s 不是常见字体格式（应为 .ttf / .ttc / .otf）", ext)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
