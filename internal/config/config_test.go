package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("取当前目录失败：%v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("切换到 %s 失败：%v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatalf("恢复工作目录到 %s 失败：%v", orig, err)
		}
	})
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写 %s 失败：%v", path, err)
	}
	return path
}

func configWithModels(models string) string {
	return `{"version":1,"api":{"models":"` + models + `"}}`
}

var lineColRe = regexp.MustCompile(`第 (\d+) 行第 (\d+) 列`)

func parseLineCol(t *testing.T, msg string) (int, int) {
	t.Helper()
	m := lineColRe.FindStringSubmatch(msg)
	if m == nil {
		t.Fatalf("错误信息里没有「第 N 行第 M 列」：%s", msg)
	}
	line, _ := strconv.Atoi(m[1])
	col, _ := strconv.Atoi(m[2])
	return line, col
}

func TestLoadFallsBackToBuiltin(t *testing.T) {
	chdir(t, t.TempDir())

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("无配置文件时不应报错：%v", err)
	}
	if cfg.Source != BuiltinSource {
		t.Errorf("Source = %q，期望 %q", cfg.Source, BuiltinSource)
	}
	want := Default()
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("内置默认被改动了：\n got %+v\nwant %+v", cfg, want)
	}

	if cfg.API.Models == "" {
		t.Error("内置默认 api.models 为空")
	}
	if cfg.Output.DefaultDays <= 0 {
		t.Errorf("内置默认 output.default_days = %d，应为正数", cfg.Output.DefaultDays)
	}
}

func TestLoadPrefersCurrentDirConfigs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, filepath.Join("configs", ConfigFileName), configWithModels("cwd_model"))
	chdir(t, dir)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load 失败：%v", err)
	}
	if cfg.API.Models != "cwd_model" {
		t.Errorf("api.models = %q，期望取自 ./configs 的 %q", cfg.API.Models, "cwd_model")
	}
	if cfg.Source != filepath.Join("configs", ConfigFileName) {
		t.Errorf("Source = %q，期望 %q", cfg.Source, filepath.Join("configs", ConfigFileName))
	}
}

func TestLoadPrefersExplicitOverEverything(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, filepath.Join("configs", ConfigFileName), configWithModels("cwd_model"))
	explicit := writeFile(t, dir, "my.json", configWithModels("explicit_model"))
	chdir(t, dir)

	cfg, err := Load(explicit)
	if err != nil {
		t.Fatalf("Load 失败：%v", err)
	}
	if cfg.API.Models != "explicit_model" {
		t.Errorf("api.models = %q，显式路径没有压过 ./configs", cfg.API.Models)
	}
	if cfg.Source != explicit {
		t.Errorf("Source = %q，期望 %q", cfg.Source, explicit)
	}
}

func TestLoadPrefersExeDirOverBuiltin(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("拿不到可执行文件路径，跳过第 3 级验证：%v", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	exeConfigDir := filepath.Join(filepath.Dir(exe), "configs")
	if err := os.MkdirAll(exeConfigDir, 0o755); err != nil {
		t.Skipf("可执行文件目录不可写，跳过第 3 级验证：%v", err)
	}
	exeConfig := filepath.Join(exeConfigDir, ConfigFileName)
	if _, err := os.Stat(exeConfig); err == nil {
		t.Skip("可执行文件目录已存在 configs/config.json，跳过以免破坏现场")
	}
	if err := os.WriteFile(exeConfig, []byte(configWithModels("exedir_model")), 0o644); err != nil {
		t.Skipf("写可执行文件目录配置失败，跳过：%v", err)
	}
	t.Cleanup(func() { _ = os.Remove(exeConfig) })

	chdir(t, t.TempDir())

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load 失败：%v", err)
	}
	if cfg.API.Models != "exedir_model" {
		t.Errorf("api.models = %q，期望取自可执行文件目录的 %q", cfg.API.Models, "exedir_model")
	}

	dir2 := t.TempDir()
	writeFile(t, dir2, filepath.Join("configs", ConfigFileName), configWithModels("cwd_model"))
	chdir(t, dir2)
	cfg2, err := Load("")
	if err != nil {
		t.Fatalf("Load 失败：%v", err)
	}
	if cfg2.API.Models != "cwd_model" {
		t.Errorf("api.models = %q，./configs 没有压过可执行文件目录", cfg2.API.Models)
	}
}

func TestLoadMissingExplicitFileIsAnError(t *testing.T) {
	chdir(t, t.TempDir())

	_, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("显式路径不存在时应报错，实际静默通过")
	}
	if !strings.Contains(err.Error(), "nope.json") {
		t.Errorf("错误信息里没提到出问题的路径：%v", err)
	}
}

func TestLoadFillsMissingFieldsFromBuiltin(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, filepath.Join("configs", ConfigFileName), configWithModels("partial"))
	chdir(t, dir)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load 失败：%v", err)
	}
	def := Default()
	if cfg.API.Models != "partial" {
		t.Errorf("显式写的字段没生效：api.models = %q", cfg.API.Models)
	}
	if cfg.API.Endpoint != def.API.Endpoint {
		t.Errorf("未写的 api.endpoint = %q，期望兜底为 %q", cfg.API.Endpoint, def.API.Endpoint)
	}
	if cfg.Thresh.CloudCoverThreshold != def.Thresh.CloudCoverThreshold {
		t.Errorf("未写的 thresholds.cloud_cover_threshold = %v，期望兜底为 %v",
			cfg.Thresh.CloudCoverThreshold, def.Thresh.CloudCoverThreshold)
	}
	if cfg.Window.NightStartHour != def.Window.NightStartHour {
		t.Errorf("未写的 window.night_start_hour = %v，期望兜底为 %v",
			cfg.Window.NightStartHour, def.Window.NightStartHour)
	}
	if len(cfg.Douyin.FontCandidates) == 0 {
		t.Error("未写的 douyin.font_candidates 变成了空切片，字体探测会全线失败")
	}
}

func TestOffsetToLineCol(t *testing.T) {
	data := []byte("abc\ndefg\n\nhi")
	cases := []struct {
		offset    int
		line, col int
		note      string
	}{
		{0, 1, 1, "文件开头"},
		{3, 1, 4, "首行末尾"},
		{4, 2, 1, "第二行开头"},
		{8, 2, 5, "第二行末尾"},
		{9, 3, 1, "空行"},
		{10, 4, 1, "第四行开头"},
		{len(data), 4, 3, "文件末尾"},
		{len(data) + 100, 4, 3, "越界应夹到末尾"},
		{-5, 1, 1, "负偏移应夹到开头"},
	}
	for _, c := range cases {
		line, col := offsetToLineCol(data, c.offset)
		if line != c.line || col != c.col {
			t.Errorf("%s：offsetToLineCol(%d) = (%d,%d)，期望 (%d,%d)",
				c.note, c.offset, line, col, c.line, c.col)
		}
	}
}

func TestLoadSyntaxErrorReportsLineAndColumn(t *testing.T) {

	bad := "{\n" +
		"  \"version\": 1,\n" +
		"  \"api\": {\n" +
		"    \"models\": \"icon_seamless\",,\n" +
		"    \"retries\": 5\n" +
		"  }\n" +
		"}\n"
	dir := t.TempDir()
	path := writeFile(t, dir, "bad.json", bad)
	chdir(t, dir)

	_, err := Load(path)
	if err == nil {
		t.Fatal("语法错误的配置应报错，实际静默通过")
	}
	msg := err.Error()
	if !strings.Contains(msg, "bad.json") {
		t.Errorf("错误信息里没有文件名：%s", msg)
	}
	if !strings.Contains(msg, "JSON 语法错误") {
		t.Errorf("错误信息没标明是语法错误：%s", msg)
	}
	line, col := parseLineCol(t, msg)
	if line != 4 {
		t.Errorf("报告的行号 = %d，期望 4（多余逗号所在行）；完整信息：%s", line, msg)
	}
	if col <= 0 {
		t.Errorf("报告的列号 = %d，应为正数；完整信息：%s", col, msg)
	}
}

func TestLoadSyntaxErrorPointsAtTheRightLineInLongFile(t *testing.T) {
	for _, badLine := range []int{2, 5, 9} {
		lines := []string{"{"}
		for i := 0; i < 9; i++ {
			lines = append(lines, "  \"k"+strconv.Itoa(i)+"\": 1,")
		}
		lines = append(lines, "  \"version\": 1", "}")

		lines[badLine-1] = "  \"broken\" 1,"

		dir := t.TempDir()
		path := writeFile(t, dir, "bad.json", strings.Join(lines, "\n")+"\n")

		_, err := Load(path)
		if err == nil {
			t.Fatalf("第 %d 行坏掉却没报错", badLine)
		}
		line, _ := parseLineCol(t, err.Error())
		if line != badLine {
			t.Errorf("坏行在第 %d 行，报告的却是第 %d 行：%v", badLine, line, err)
		}
	}
}

func TestLoadTypeErrorReportsFieldAndPosition(t *testing.T) {
	bad := "{\n" +
		"  \"version\": 1,\n" +
		"  \"api\": {\n" +
		"    \"retries\": \"five\"\n" +
		"  }\n" +
		"}\n"
	dir := t.TempDir()
	path := writeFile(t, dir, "type.json", bad)

	_, err := Load(path)
	if err == nil {
		t.Fatal("类型错误的配置应报错，实际静默通过")
	}
	msg := err.Error()
	if !strings.Contains(msg, "类型错误") {
		t.Errorf("错误信息没标明是类型错误：%s", msg)
	}
	if !strings.Contains(msg, "retries") {
		t.Errorf("错误信息没指出出错字段 retries：%s", msg)
	}
	line, _ := parseLineCol(t, msg)
	if line != 4 {
		t.Errorf("报告的行号 = %d，期望 4；完整信息：%s", line, msg)
	}
}

func TestLoadSitesSkipsIllegalSiteAndKeepsRest(t *testing.T) {
	sites := `{"version":1,"sites":[
	  {"name":"海坨山","lat":40.5573,"lon":115.8407,"alt":2241},
	  {"name":"珠峰超限","lat":27.9881,"lon":86.9250,"alt":99999},
	  {"name":"灵山","lat":39.9836,"lon":115.4869,"alt":2303}
	]}`
	dir := t.TempDir()
	path := writeFile(t, dir, "sites.json", sites)

	got, err := LoadSites(path)
	if err != nil {
		t.Fatalf("单个点位非法不应导致整体失败：%v", err)
	}
	if len(got.Sites) != 2 {
		t.Fatalf("解析出 %d 个点位，期望 2 个（跳过越界的那个）：%+v", len(got.Sites), got.Sites)
	}
	for _, s := range got.Sites {
		if s.Name == "珠峰超限" {
			t.Error("alt=99999 的非法点位没有被跳过")
		}
	}
	if got.Source != path {
		t.Errorf("Source = %q，期望 %q（不该回落到内置默认）", got.Source, path)
	}

	if len(got.Warnings) != 1 {
		t.Fatalf("warning 数 = %d，期望 1 条：%v", len(got.Warnings), got.Warnings)
	}
	if !strings.Contains(got.Warnings[0], "第 2 个点位") {
		t.Errorf("warning 没有指出是第几个点位：%q", got.Warnings[0])
	}
	if !strings.Contains(got.Warnings[0], "alt") {
		t.Errorf("warning 没有指出是哪个字段越界：%q", got.Warnings[0])
	}
}

func TestLoadSitesAllIllegalFallsBackToBuiltin(t *testing.T) {
	sites := `{"version":1,"sites":[
	  {"name":"","lat":0,"lon":0,"alt":0},
	  {"name":"纬度越界","lat":9999,"lon":0,"alt":0}
	]}`
	dir := t.TempDir()
	path := writeFile(t, dir, "sites.json", sites)

	got, err := LoadSites(path)
	if err != nil {
		t.Fatalf("全部非法应降级而非报错：%v", err)
	}
	if got.Source != BuiltinSource {
		t.Errorf("Source = %q，期望回落到 %q", got.Source, BuiltinSource)
	}
	if len(got.Sites) != len(DefaultSites()) {
		t.Errorf("点位数 = %d，期望等于内置默认的 %d", len(got.Sites), len(DefaultSites()))
	}
	joined := strings.Join(got.Warnings, "\n")
	if !strings.Contains(joined, "回退") {
		t.Errorf("没有告知用户已回退到内置默认：%v", got.Warnings)
	}
}

func TestLoadSitesSyntaxErrorIsFatal(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "sites.json", "{\n  \"sites\": [\n    {\"name\": \"x\",,}\n  ]\n}\n")

	_, err := LoadSites(path)
	if err == nil {
		t.Fatal("点位文件语法错误时应报错，实际静默通过")
	}
	if !strings.Contains(err.Error(), "语法错误") {
		t.Errorf("错误信息没标明是语法错误：%v", err)
	}
	line, _ := parseLineCol(t, err.Error())
	if line != 3 {
		t.Errorf("报告的行号 = %d，期望 3：%v", line, err)
	}
}

func TestLoadSitesAcceptsBareArray(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "sites.json",
		`[{"name":"海坨山","lat":40.5573,"lon":115.8407,"alt":2241}]`)

	got, err := LoadSites(path)
	if err != nil {
		t.Fatalf("裸数组格式应被接受：%v", err)
	}
	if len(got.Sites) != 1 || got.Sites[0].Name != "海坨山" {
		t.Fatalf("裸数组解析结果不对：%+v", got.Sites)
	}
}

func TestLoadSitesSkipsDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "sites.json", `[
	  {"name":"灵山","lat":39.9836,"lon":115.4869,"alt":2303},
	  {"name":"灵山","lat":40.0,"lon":115.5,"alt":2000}
	]`)

	got, err := LoadSites(path)
	if err != nil {
		t.Fatalf("LoadSites 失败：%v", err)
	}
	if len(got.Sites) != 1 {
		t.Fatalf("重名点位没有被跳过：%+v", got.Sites)
	}
	if got.Sites[0].Alt != 2303 {
		t.Errorf("保留的应是先出现的那个（alt=2303），实际 alt=%v", got.Sites[0].Alt)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "重名") {
		t.Errorf("重名跳过没有留下 warning：%v", got.Warnings)
	}
}

func TestValidateSiteBoundaries(t *testing.T) {
	base := Site{Name: "x", Lat: 0, Lon: 0, Alt: 0}
	with := func(f func(*Site)) Site {
		s := base
		f(&s)
		return s
	}
	cases := []struct {
		name    string
		site    Site
		wantErr bool
	}{
		{"正常点位", base, false},
		{"name 为空", with(func(s *Site) { s.Name = "" }), true},
		{"name 恰好 16 字符", with(func(s *Site) { s.Name = strings.Repeat("山", 16) }), false},
		{"name 超过 16 字符", with(func(s *Site) { s.Name = strings.Repeat("山", 17) }), true},
		{"lat 下边界 -90", with(func(s *Site) { s.Lat = -90 }), false},
		{"lat 上边界 90", with(func(s *Site) { s.Lat = 90 }), false},
		{"lat 越下界", with(func(s *Site) { s.Lat = -90.0001 }), true},
		{"lat 越上界", with(func(s *Site) { s.Lat = 90.0001 }), true},
		{"lon 下边界 -180", with(func(s *Site) { s.Lon = -180 }), false},
		{"lon 上边界 180", with(func(s *Site) { s.Lon = 180 }), false},
		{"lon 越界", with(func(s *Site) { s.Lon = 180.0001 }), true},
		{"alt 下边界 -500", with(func(s *Site) { s.Alt = -500 }), false},
		{"alt 上边界 9000", with(func(s *Site) { s.Alt = 9000 }), false},
		{"alt 越上界", with(func(s *Site) { s.Alt = 9000.0001 }), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateSite(c.site)
			if c.wantErr && err == nil {
				t.Errorf("期望校验失败，实际通过：%+v", c.site)
			}
			if !c.wantErr && err != nil {
				t.Errorf("期望校验通过，实际失败：%v", err)
			}
		})
	}
}

func TestEnabledFiltersDisabledSites(t *testing.T) {
	no, yes := false, true
	r := SitesResult{Sites: []Site{
		{Name: "缺省启用"},
		{Name: "显式启用", Enabled: &yes},
		{Name: "显式停用", Enabled: &no},
	}}
	got := r.Enabled()
	if len(got) != 2 {
		t.Fatalf("Enabled() 返回 %d 个，期望 2 个：%+v", len(got), got)
	}
	for _, s := range got {
		if s.Name == "显式停用" {
			t.Error("enabled=false 的点位不该参与计算")
		}
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "configs", ConfigFileName)

	cfg := Default()
	cfg.API.Models = "round_trip"
	cfg.Output.DefaultDays = 7
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save 失败：%v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失败：%v", err)
	}
	if got.API.Models != "round_trip" || got.Output.DefaultDays != 7 {
		t.Errorf("往返后字段丢失：models=%q days=%d", got.API.Models, got.Output.DefaultDays)
	}

	cfg.API.Models = "second"
	if err := Save(cfg, path); err != nil {
		t.Fatalf("第二次 Save 失败：%v", err)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("没有生成 .bak 备份：%v", err)
	}
	if !strings.Contains(string(bak), "round_trip") {
		t.Errorf(".bak 里不是上一版内容：%s", string(bak))
	}
}

func TestSaveSitesRejectsIllegalBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sites.json")

	err := SaveSites([]Site{
		{Name: "好点位", Lat: 40, Lon: 116, Alt: 1000},
		{Name: "坏点位", Lat: 999, Lon: 116, Alt: 1000},
	}, path)
	if err == nil {
		t.Fatal("含非法点位时应拒绝写回")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("拒绝写回后不该留下文件")
	}
}

func TestDefaultAssetsAreParseable(t *testing.T) {
	var probe map[string]any
	if err := json.Unmarshal(DefaultConfigJSON(), &probe); err != nil {
		t.Fatalf("内嵌 config.json 不可解析：%v", err)
	}
	if err := json.Unmarshal(DefaultSitesJSON(), &probe); err != nil {
		t.Fatalf("内嵌 sites.json 不可解析：%v", err)
	}
	if len(DefaultSites()) == 0 {
		t.Fatal("内置默认点位为空")
	}
	for i, s := range DefaultSites() {
		if err := ValidateSite(s); err != nil {
			t.Errorf("内置第 %d 个默认点位不合法：%v", i+1, err)
		}
	}
}

func TestDefaultConfigJSONCoversEveryAPIField(t *testing.T) {
	var raw struct {
		API map[string]json.RawMessage `json:"api"`
	}
	if err := json.Unmarshal(DefaultConfigJSON(), &raw); err != nil {
		t.Fatalf("解析内嵌 config.json 失败：%v", err)
	}

	data, err := json.Marshal(Default().API)
	if err != nil {
		t.Fatalf("序列化 APIConfig 失败：%v", err)
	}
	var want map[string]json.RawMessage
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatalf("解析 APIConfig 失败：%v", err)
	}
	for field := range want {
		if _, ok := raw.API[field]; !ok {
			t.Errorf("内嵌默认 config.json 的 api 段缺字段 %q，它会静默变成 Go 零值", field)
		}
	}
}

func TestDocumentedSiteCountMatchesReality(t *testing.T) {
	want := len(DefaultSites())

	countRe := regexp.MustCompile(`内置(?:的)? (\d+) 个(?:默认)?点位`)
	for _, rel := range []string{
		filepath.Join("..", "..", "README.md"),
		"sites.go",
	} {
		data, err := os.ReadFile(rel)
		if err != nil {
			t.Fatalf("读 %s 失败：%v", rel, err)
		}
		matches := countRe.FindAllStringSubmatch(string(data), -1)
		if len(matches) == 0 {
			t.Errorf("%s 里找不到「内置 N 个点位」的表述，无法校验文档漂移", rel)
			continue
		}
		for _, m := range matches {
			got, _ := strconv.Atoi(m[1])
			if got != want {
				t.Errorf("%s 写着「%s」，但内置点位实际有 %d 个", rel, m[0], want)
			}
		}
	}
}
