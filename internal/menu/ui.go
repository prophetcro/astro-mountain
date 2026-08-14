package menu

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/report"
)

const (
	maxInvalidTries = 3

	boxWidth = 68

	dateLayout = "2006-01-02"
)

var (
	errBack = errors.New("menu: 返回上级")

	errQuit = errors.New("menu: 用户退出")

	errEOF = errors.New("menu: 输入流结束")

	errCanceled = errors.New("menu: 已取消")
)

func isControl(err error) bool {
	return errors.Is(err, errBack) || errors.Is(err, errQuit) ||
		errors.Is(err, errEOF) || errors.Is(err, errCanceled)
}

func isTerminal(err error) bool {
	return errors.Is(err, errQuit) || errors.Is(err, errEOF) ||
		errors.Is(err, errCanceled)
}

type backMode int

const (
	backReturns backMode = iota

	backLiteral
)

type ui struct {
	ctx context.Context
	in  *bufio.Reader
	out io.Writer

	eof bool
}

func newUI(ctx context.Context, in io.Reader, out io.Writer) *ui {
	if ctx == nil {
		ctx = context.Background()
	}
	return &ui{ctx: ctx, in: bufio.NewReader(in), out: out}
}

func (u *ui) printf(format string, args ...any) {
	fmt.Fprintf(u.out, format, args...)
}

func (u *ui) println(args ...any) {
	fmt.Fprintln(u.out, args...)
}

func (u *ui) blank() {
	fmt.Fprintln(u.out)
}

func (u *ui) fail(reason string) {
	u.printf("  ✗ 无效输入：%s\n", reason)
}

func (u *ui) warn(msg string) {
	u.printf("  ⚠ %s\n", msg)
}

func (u *ui) ok(msg string) {
	u.printf("  ✓ %s\n", msg)
}

func (u *ui) info(msg string) {
	u.printf("  %s\n", msg)
}

type readResult struct {
	line string
	err  error
}

func (u *ui) readRaw() (string, error) {
	if u.eof {
		return "", errEOF
	}

	select {
	case <-u.ctx.Done():
		return "", errCanceled
	default:
	}

	ch := make(chan readResult, 1)
	go func() {
		line, err := u.in.ReadString('\n')
		ch <- readResult{line: line, err: err}
	}()

	select {
	case <-u.ctx.Done():
		return "", errCanceled
	case r := <-ch:
		text := strings.TrimSpace(strings.TrimRight(r.line, "\r\n"))
		if r.err != nil {
			if errors.Is(r.err, io.EOF) {
				u.eof = true

				if text != "" {
					return text, nil
				}
				return "", errEOF
			}
			return "", fmt.Errorf("读取输入失败：%w", r.err)
		}
		return text, nil
	}
}

func (u *ui) prompt(label, def string, back backMode) (string, error) {
	for {
		if def != "" {
			u.printf("  %s  (默认 %s) > ", label, def)
		} else {
			u.printf("  %s > ", label)
		}
		text, err := u.readRaw()
		if err != nil {
			return "", err
		}
		lower := strings.ToLower(text)
		if back == backReturns && (lower == "b" || lower == "back") {
			return "", errBack
		}
		switch lower {
		case "q", "quit", "exit":
			yes, cerr := u.confirmRaw("确定退出？", false)
			if cerr != nil {
				return "", cerr
			}
			if yes {
				return "", errQuit
			}
			continue
		}
		if text == "" {
			return def, nil
		}
		return text, nil
	}
}

func (u *ui) confirmRaw(label string, def bool) (bool, error) {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	for tries := 0; tries < maxInvalidTries; tries++ {
		u.printf("  %s %s > ", label, hint)
		text, err := u.readRaw()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(text) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			u.fail("请输入 y 或 n")
		}
	}

	u.warn("输入多次无效，已采用默认值")
	return def, nil
}

func (u *ui) confirm(label string, def bool) (bool, error) {
	return u.confirmRaw(label, def)
}

func (u *ui) choice(label string, valid []string, def string, back backMode) (string, error) {
	allow := make(map[string]string, len(valid))
	for _, v := range valid {
		allow[strings.ToLower(v)] = v
	}
	full := fmt.Sprintf("%s [%s]", label, strings.Join(valid, "/"))
	for tries := 0; tries < maxInvalidTries; tries++ {
		text, err := u.prompt(full, def, back)
		if err != nil {
			return "", err
		}
		if v, ok := allow[strings.ToLower(text)]; ok {
			return v, nil
		}
		u.fail(fmt.Sprintf("%q 不是可选项，请输入 %s 之一", text, strings.Join(valid, " / ")))
	}
	u.warn("输入多次无效，已返回上级")
	return "", errBack
}

// askChoice 显示编号候选菜单，用户用数字选择枚举项（如气象模式）。
// 回车沿用当前值（def）；输入数字选对应项；也可直接键入其它值（兼容未列出的模式名）。
func (u *ui) askChoice(label string, def string, options []string) (string, error) {
	for i, o := range options {
		mark := "  "
		if o == def {
			mark = "* "
		}
		u.printf("  [%d] %s%s\n", i+1, mark, o)
	}
	if len(options) > 0 {
		u.printf("  输入序号选择；直接键入其它模型名亦可（回车沿用当前 %s）\n", def)
	}
	for tries := 0; tries < maxInvalidTries; tries++ {
		text, err := u.prompt(label, def, backReturns)
		if err != nil {
			return "", err
		}
		if text == "" {
			return def, nil
		}
		if n, cerr := strconv.Atoi(text); cerr == nil && n >= 1 && n <= len(options) {
			return options[n-1], nil
		}
		// 非数字的输入按自由模型名处理，兼容未在列表中枚举的模式
		return text, nil
	}
	u.warn("输入多次无效，已返回上级")
	return "", errBack
}

// modelOptions 返回交互菜单里可选的常用数值模式（华东免费层可用）。
// 列表外仍可自由键入其它模式名（如 ecmwf_ifs025 等付费模式）。
//
// 注意：ecmwf_ifs04 曾列入菜单，但实测 Open-Meteo 免费层对其返回的
// 气压层云量/位势高/相对湿度全部为 null（地面变量亦如此），无法反演云海
// 几何，整站有效数据计数为 0。已移除，避免用户选到后全员 0/7。若日后
// Open-Meteo 免费层补齐其气压层数据，可重新加入。
var modelOptionsList = []string{
	"icon_seamless", // 默认：DWD 德国模式，华东稳定
	"gfs_seamless",  // 美国全球模式，更贴 Windy 实测
	"best_match",    // 平台自动挑选最优
}

func modelOptions() []string { return modelOptionsList }

// checkboxItem 表示一个复选框选项。
type checkboxItem struct {
	Label   string
	Checked bool
}

// askCheckbox 显示复选框菜单，用户输入逗号分隔的序号来切换勾选状态。
// 回车沿用当前状态；输入 0 或空返回当前 def 值；输入 b 返回 errBack。
// 返回 map[label]checked。
func (u *ui) askCheckbox(label string, items []checkboxItem) (map[string]bool, error) {
	checked := make(map[string]bool, len(items))
	for _, it := range items {
		checked[it.Label] = it.Checked
	}

	render := func() {
		u.blank()
		u.info(label)
		for i, it := range items {
			mark := "[ ]"
			if checked[it.Label] {
				mark = "[x]"
			}
			u.printf("   %s [%d] %s\n", mark, i+1, it.Label)
		}
		u.info("输入序号切换（逗号分隔多个，回车确认，b 返回）")
	}

	for tries := 0; tries < maxInvalidTries; tries++ {
		render()
		text, err := u.prompt("选择", "", backReturns)
		if err != nil {
			return nil, err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return checked, nil
		}
		if strings.ToLower(text) == "b" {
			return nil, errBack
		}

		parts := strings.Split(text, ",")
		invalid := []string{}
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			n, cerr := strconv.Atoi(p)
			if cerr != nil || n < 1 || n > len(items) {
				invalid = append(invalid, p)
				continue
			}
			lab := items[n-1].Label
			checked[lab] = !checked[lab]
		}
		if len(invalid) > 0 {
			u.fail(fmt.Sprintf("无效选项：%s，请输入 1~%d 的序号", strings.Join(invalid, ", "), len(items)))
			continue
		}
		return checked, nil
	}
	u.warn("输入多次无效，已返回上级")
	return nil, errBack
}

func (u *ui) askInt(label string, min, max int, def int, hasDef bool) (int, error) {
	defStr := ""
	if hasDef {
		defStr = strconv.Itoa(def)
	}
	for tries := 0; tries < maxInvalidTries; tries++ {
		text, err := u.prompt(label, defStr, backReturns)
		if err != nil {
			return 0, err
		}
		if text == "" {
			u.fail("不能为空")
			continue
		}
		n, cerr := strconv.Atoi(text)
		if cerr != nil {
			u.fail(fmt.Sprintf("%q 不是整数", text))
			continue
		}
		if n < min || n > max {
			u.fail(fmt.Sprintf("须在 %d ~ %d 之间", min, max))
			continue
		}
		return n, nil
	}
	u.warn("输入多次无效，已返回上级")
	return 0, errBack
}

func (u *ui) askFloat(label string, min, max float64, def string) (float64, error) {
	for tries := 0; tries < maxInvalidTries; tries++ {
		text, err := u.prompt(label, def, backReturns)
		if err != nil {
			return 0, err
		}
		if text == "" {
			u.fail("不能为空")
			continue
		}
		f, cerr := strconv.ParseFloat(text, 64)
		if cerr != nil {
			u.fail(fmt.Sprintf("%q 不是有效数字", text))
			continue
		}
		if f < min || f > max {
			u.fail(fmt.Sprintf("须在 %g ~ %g 之间", min, max))
			continue
		}
		return f, nil
	}
	u.warn("输入多次无效，已返回上级")
	return 0, errBack
}

func (u *ui) askText(label, def string, validate func(string) error) (string, error) {
	for tries := 0; tries < maxInvalidTries; tries++ {
		text, err := u.prompt(label, def, backReturns)
		if err != nil {
			return "", err
		}
		if text == "" {
			u.fail("不能为空")
			continue
		}
		if validate != nil {
			if verr := validate(text); verr != nil {
				u.fail(verr.Error())
				continue
			}
		}
		return text, nil
	}
	u.warn("输入多次无效，已返回上级")
	return "", errBack
}

func (u *ui) askOptionalText(label, def string) (string, error) {
	u.printf("  %s > ", label)
	text, err := u.readRaw()
	if err != nil {
		return "", err
	}
	switch strings.ToLower(text) {
	case "b", "back":
		return "", errBack
	case "q", "quit":
		yes, cerr := u.confirmRaw("确定退出？", false)
		if cerr != nil {
			return "", cerr
		}
		if yes {
			return "", errQuit
		}
		return def, nil
	}
	if text == "" {
		return def, nil
	}
	return text, nil
}

func (u *ui) askDate(label, def string) (string, error) {
	for tries := 0; tries < maxInvalidTries; tries++ {
		text, err := u.prompt(label, def, backReturns)
		if err != nil {
			return "", err
		}
		if text == "" {
			u.fail("日期不能为空")
			continue
		}
		if _, verr := ValidateDate(text); verr != nil {
			u.fail(verr.Error())
			continue
		}
		return text, nil
	}
	u.warn("输入多次无效，已返回上级")
	return "", errBack
}

func ValidateDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return time.Time{}, fmt.Errorf("日期格式应为 YYYY-MM-DD，例如 2026-08-13")
	}
	for i := 0; i < len(s); i++ {
		if i == 4 || i == 7 {
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return time.Time{}, fmt.Errorf("日期只能由数字与短横线组成，例如 2026-08-13")
		}
	}
	t, err := time.ParseInLocation(dateLayout, s, time.UTC)
	if err == nil {
		return t, nil
	}
	year, _ := strconv.Atoi(s[0:4])
	month, _ := strconv.Atoi(s[5:7])
	day, _ := strconv.Atoi(s[8:10])
	switch {
	case month < 1 || month > 12:
		return time.Time{}, fmt.Errorf("月份 %d 不存在（应为 1~12）", month)
	case day < 1:
		return time.Time{}, fmt.Errorf("日期 %d 不存在（应为 1~31）", day)
	default:
		return time.Time{}, fmt.Errorf("%d 年 %d 月没有 %d 日", year, month, day)
	}
}

func ParseIndexSpec(spec string, n int) ([]int, error) {
	spec = strings.TrimSpace(spec)
	switch strings.ToLower(spec) {
	case "all", "a":
		out := make([]int, 0, n)
		for i := 1; i <= n; i++ {
			out = append(out, i)
		}
		return out, nil
	case "none":
		return []int{}, nil
	case "":
		return []int{}, nil
	}

	fields := strings.FieldsFunc(spec, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '，'
	})
	seen := make(map[int]bool, len(fields))
	out := make([]int, 0, len(fields))
	appendIdx := func(i int) error {
		if i < 1 || i > n {
			return fmt.Errorf("序号 %d 超出范围（1~%d）", i, n)
		}
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
		return nil
	}
	for _, f := range fields {
		if lo, hi, ok := splitRange(f); ok {
			a, err1 := strconv.Atoi(lo)
			b, err2 := strconv.Atoi(hi)
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("%q 不是合法的序号区间（正确写法如 1-4）", f)
			}
			if a > b {
				a, b = b, a
			}
			for i := a; i <= b; i++ {
				if err := appendIdx(i); err != nil {
					return nil, err
				}
			}
			continue
		}
		v, err := strconv.Atoi(f)
		if err != nil {
			return nil, fmt.Errorf("%q 不是整数序号", f)
		}
		if err := appendIdx(v); err != nil {
			return nil, err
		}
	}
	sortInts(out)
	return out, nil
}

func splitRange(s string) (string, string, bool) {
	idx := strings.Index(s, "-")

	if idx <= 0 || idx == len(s)-1 {
		return "", "", false
	}
	return s[:idx], s[idx+1:], true
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

func ClipWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if report.DispWidth(s) <= max {
		return s
	}
	if max <= 2 {
		return strings.Repeat(".", max)
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		cw := report.CharWidth(r)
		if w+cw > max-2 {
			break
		}
		b.WriteRune(r)
		w += cw
	}
	b.WriteString("..")
	return b.String()
}

func (u *ui) rule(ch string, width int) {
	u.println(report.Repeat(ch, width))
}

func (u *ui) banner(title string) {
	head := "══ " + title + " "
	pad := boxWidth + 2 - report.DispWidth(head)
	if pad < 0 {
		pad = 0
	}
	u.blank()
	u.println(head + report.Repeat("═", pad))
}

func (u *ui) step(title string) {
	u.blank()
	u.printf(" ── %s ──\n", title)
}

func (u *ui) boxTop()    { u.println("┌" + report.Repeat("─", boxWidth) + "┐") }
func (u *ui) boxMid()    { u.println("├" + report.Repeat("─", boxWidth) + "┤") }
func (u *ui) boxBottom() { u.println("└" + report.Repeat("─", boxWidth) + "┘") }

func (u *ui) boxLine(text string) {
	u.println("│" + report.Pad(ClipWidth(text, boxWidth), boxWidth, report.AlignLeft) + "│")
}

func (u *ui) boxKV(key, value, extra string) {
	const (
		keyW   = 10
		valueW = 30
	)
	tailW := boxWidth - 2 - keyW - valueW
	line := "  " +
		report.Pad(ClipWidth(key, keyW), keyW, report.AlignLeft) +
		report.Pad(ClipWidth(value, valueW-2), valueW, report.AlignLeft) +
		ClipWidth(extra, tailW)
	u.boxLine(line)
}

func (u *ui) table(indent string, headers []string, aligns []string, rows [][]string) {
	if len(headers) == 0 {
		return
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = report.DispWidth(h)
	}
	for _, r := range rows {
		for i := 0; i < len(headers) && i < len(r); i++ {
			if w := report.DispWidth(r[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}
	line := func(cells []string) string {
		var b strings.Builder
		b.WriteString(indent)
		for i := range headers {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			align := report.AlignLeft
			if i < len(aligns) && aligns[i] == report.AlignRight {
				align = report.AlignRight
			}
			b.WriteString(report.Pad(cell, widths[i], align))
			if i != len(headers)-1 {
				b.WriteString("  ")
			}
		}
		return strings.TrimRight(b.String(), " ")
	}
	u.println(line(headers))
	total := -2
	for _, w := range widths {
		total += w + 2
	}
	if total < 0 {
		total = 0
	}
	u.println(indent + report.Repeat("─", total))
	for _, r := range rows {
		u.println(line(r))
	}
}

func checkbox(on bool) string {
	if on {
		return "[x]"
	}
	return "[ ]"
}

func (u *ui) multiSelect(labels []string, hints []string, selected []bool,
	atLeastOne bool, emptyMsg string) error {

	n := len(labels)
	if len(selected) != n {
		return fmt.Errorf("multiSelect: selected 长度 %d 与 labels 长度 %d 不符", len(selected), n)
	}
	invalid := 0
	for {

		u.blank()
		for i := 0; i < n; i++ {
			hint := ""
			if i < len(hints) {
				hint = hints[i]
			}
			u.println(strings.TrimRight(fmt.Sprintf("   [%d] %s %s   %s",
				i+1, report.Pad(labels[i], 20, report.AlignLeft), checkbox(selected[i]), hint), " "))
		}
		count := 0
		for _, s := range selected {
			if s {
				count++
			}
		}
		text, err := u.prompt(
			fmt.Sprintf("输入序号切换选中（如 1,3 或 1-3）；all=全选 none=全不选；回车确认（当前已选 %d 项）", count),
			"", backReturns)
		if err != nil {
			return err
		}
		if text == "" {
			if atLeastOne && count == 0 {
				u.fail(emptyMsg)
				invalid++
				if invalid >= maxInvalidTries {
					u.warn("输入多次无效，已返回上级")
					return errBack
				}
				continue
			}
			return nil
		}
		idx, perr := ParseIndexSpec(text, n)
		if perr != nil {
			u.fail(perr.Error())
			invalid++
			if invalid >= maxInvalidTries {
				u.warn("输入多次无效，已返回上级")
				return errBack
			}
			continue
		}
		invalid = 0
		switch strings.ToLower(text) {
		case "all", "a":
			for i := range selected {
				selected[i] = true
			}
		case "none":
			for i := range selected {
				selected[i] = false
			}
		default:
			for _, i := range idx {
				selected[i-1] = !selected[i-1]
			}
		}
	}
}

func (u *ui) pause() error {
	u.blank()
	u.printf("  按回车返回主菜单 > ")
	_, err := u.readRaw()
	if err != nil && !errors.Is(err, errBack) {
		return err
	}
	return nil
}
