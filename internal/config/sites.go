package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/prophetcro/astro-mountain/internal/model"
)

// Site 是观测点位，直接复用 model.Site（类型别名，两处引用同一类型）。
type Site = model.Site

// SitesFileName 是点位配置的固定文件名。
const SitesFileName = "sites.json"

// SitesFile 是 sites.json 的对象格式，SaveSites 始终按此格式写回。
type SitesFile struct {
	Version int    `json:"version"`
	Updated string `json:"updated"`
	Sites   []Site `json:"sites"`
}

// SitesResult 是一次点位加载的结果。
type SitesResult struct {
	Sites  []Site
	Source string
	// Warnings 收集被跳过的无效点位等非致命问题，调用方应展示给用户。
	Warnings []string
}

// Enabled 返回参与计算的点位，即 enabled 字段缺省或为 true 的那些。
func (r SitesResult) Enabled() []Site {
	out := make([]Site, 0, len(r.Sites))
	for _, s := range r.Sites {
		if s.IsEnabled() {
			out = append(out, s)
		}
	}
	return out
}

// DefaultSites 返回内置的 19 个默认点位（华东站点 + 星辰山 + 白鹤尖 + 东白山 + 金华山·北山）。
//
// 内置数据在编译期嵌入，解析失败属于构建产物损坏，直接 panic 而非返回错误。
func DefaultSites() []Site {
	sites, _, err := parseSites(defaultSitesJSON)
	if err != nil {
		panic(fmt.Sprintf("内置默认 sites.json 解析失败：%v", err))
	}
	return sites
}

// LoadSites 按 explicit > ./configs/sites.json > 可执行文件同级 configs/sites.json >
// 内置默认的优先级加载点位。
//
// 容错策略分两级：文件不可读或整体 JSON 非法时返回错误；个别点位字段不合法只跳过该点位
// 并记入 Warnings，其余点位照常使用。若一个有效点位都没解析出来，回退到内置默认并给出
// 警告，保证 CLI 总有可算的点位。
func LoadSites(explicit string) (SitesResult, error) {
	path, err := resolvePath(explicit, SitesFileName)
	if err != nil {
		return SitesResult{}, err
	}
	if path == "" {
		return SitesResult{Sites: DefaultSites(), Source: BuiltinSource}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return SitesResult{}, fmt.Errorf("读取点位文件 %s 失败：%w", path, err)
	}
	sites, warnings, err := parseSites(data)
	if err != nil {
		return SitesResult{}, decorateJSONError(path, data, err)
	}
	if len(sites) == 0 {
		warnings = append(warnings,
			fmt.Sprintf("点位文件 %s 未解析出任何有效点位，已回退到内置默认点位", path))
		return SitesResult{Sites: DefaultSites(), Source: BuiltinSource, Warnings: warnings}, nil
	}
	return SitesResult{Sites: sites, Source: path, Warnings: warnings}, nil
}

// parseSites 解析点位数据，同时接受裸数组和 SitesFile 对象两种顶层格式，
// 并按首个非空白字符是否为 '[' 来区分。重名点位保留先出现的那个。
func parseSites(data []byte) ([]Site, []string, error) {
	trimmed := skipSpace(data)
	var raw []Site
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, nil, err
		}
	} else {
		var file SitesFile
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, nil, err
		}
		raw = file.Sites
	}

	var (
		sites    []Site
		warnings []string
		seen     = make(map[string]bool, len(raw))
	)
	for i, s := range raw {
		if err := ValidateSite(s); err != nil {
			warnings = append(warnings, fmt.Sprintf("跳过第 %d 个点位：%v", i+1, err))
			continue
		}
		if seen[s.Name] {
			warnings = append(warnings, fmt.Sprintf("跳过重名点位 %q（第 %d 个）", s.Name, i+1))
			continue
		}
		seen[s.Name] = true
		sites = append(sites, s)
	}
	return sites, warnings, nil
}

// skipSpace 跳过开头的空白，顺带跳过 UTF-8 BOM 的三个字节，
// 避免带 BOM 的文件被误判成对象格式。
func skipSpace(data []byte) []byte {
	for i, b := range data {
		switch b {
		case ' ', '\t', '\r', '\n', 0xEF, 0xBB, 0xBF:
			continue
		default:
			return data[i:]
		}
	}
	return nil
}

// ValidateSite 校验单个点位的字段取值范围，名字长度按 rune 计数以适配中文站名。
func ValidateSite(s Site) error {
	name := []rune(s.Name)
	if len(name) == 0 {
		return fmt.Errorf("name 不能为空")
	}
	if len(name) > 16 {
		return fmt.Errorf("name %q 超过 16 字符", s.Name)
	}
	if s.Lat < -90 || s.Lat > 90 {
		return fmt.Errorf("点位 %q 的 lat=%v 超出 -90~90", s.Name, s.Lat)
	}
	if s.Lon < -180 || s.Lon > 180 {
		return fmt.Errorf("点位 %q 的 lon=%v 超出 -180~180", s.Name, s.Lon)
	}
	if s.Alt < -500 || s.Alt > 9000 {
		return fmt.Errorf("点位 %q 的 alt=%v 超出 -500~9000", s.Name, s.Alt)
	}
	return nil
}

// SaveSites 把点位以对象格式写回 path（原子替换 + .bak 备份）。
//
// 写盘前先整体校验，任一点位不合法就整体放弃，不产生"写了一半"的文件。
func SaveSites(sites []Site, path string) error {
	for _, s := range sites {
		if err := ValidateSite(s); err != nil {
			return fmt.Errorf("写回前校验失败：%w", err)
		}
	}
	file := SitesFile{
		Version: 1,
		Updated: time.Now().Format("2006-01-02"),
		Sites:   sites,
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化点位失败：%w", err)
	}
	data = append(data, '\n')
	return atomicWrite(path, data)
}
