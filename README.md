# 山地星野 · 低云海拔评估工具（astro-mountain）

> 上山拍星空之前，先算清楚**云在你头顶还是脚下**。

一个零依赖的单文件 Go 命令行工具：拉取 Open-Meteo 的多层气压面气象数据，反演出  
**云底 / 云顶的海拔高度**，再和你的机位海拔一比，直接告诉你这一夜到底能不能拍。

---

## 目录

- [为什么需要它](#为什么需要它)
- [核心功能](#核心功能)
- [快速开始](#快速开始)
- [从源码构建](#从源码构建)
- [配置文件说明](#配置文件说明)
- [命令行参数](#命令行参数)
- [使用示例](#使用示例)
- [输出产物说明](#输出产物说明)
- [评级含义](#评级含义)
- [常见问题](#常见问题)

---

## 为什么需要它

普通天气预报只告诉你「低云量 80%」。但对山顶机位来说，这个数字**几乎没有意义**：

| 同样是低云量 80%                 | 实际拍摄体验                   |
| -------------------------- | ------------------------ |
| 云顶 900m，机位 1500m           | ☁️ 脚下一片云海，**大片**         |
| 云底 1200m，云顶 2000m，机位 1500m | 🌫️ 你正泡在云里，能见度十几米，**白跑** |
| 云底 1800m，机位 1500m          | 🌑 头顶一层盖子，**啥也拍不到**      |

三种情况在预报 App 里长得一模一样。决定成败的不是云量，而是**云所在的海拔区间与你机位海拔的相对关系**。

本工具请求 Open-Meteo 的 8 个气压层（1000 / 975 / 950 / 925 / 900 / 850 / 800 / 700 hPa）  
的云量、位势高度与相对湿度，自下而上切分出连续云层、线性插值算出云底云顶的海拔，  
再做站点三分类，最后综合雾、能见度、温露差、月相、天文暗夜等因素给出四级评级。

**安全红线**：数据缺测时输出 `❓无数据`，**绝不**降级成「晴朗」。宁可说不知道，也不能骗你上山。

---

## 核心功能

- **低云海拔反演** — 8 个气压层剖面组装 → 云层切分 → 云底/云顶插值 → 站点三分类  
  （云海在脚下 / 机位在云中 / 云在头顶）
- **四级评级** — ✅通透 / ⚠️风险 / 🔴不宜 / ❓无数据，综合云层关系、雾判据、  
  低云量交叉校验、温露差结露提示、LCL 经验估算
- **天文条件** — 月相月龄与照度、月亮高度、银心（Sgr A*）高度、天文暗夜判定  
  （太阳高度 ≤ −18°）。纯计算，不下载星历
- **终端交互菜单** — 无参数启动即进入中文菜单，不用记任何 flag
- **多格式输出** — 终端 ASCII 表格 + Markdown 报告 + CSV（30 字段）+ JSON（含完整剖面）
- **抖音竖版出图** — 1080×1920 深色星空风 PNG，自动字号缩放与分页
- **配置驱动** — 观测点位与全部阈值外置为 JSON，改文件或走菜单即可，无需重新编译
- **零运行时依赖** — 单个可执行文件，`CGO_ENABLED=0` 编译，不需要装 Python / Node / 任何库

---

## 快速开始

### Windows x64

1. 下载 `astro-mountain_<version>_windows_amd64.zip`，右键「全部解压缩」到任意目录，  
   会得到一个 `astro-mountain-windows-amd64\` 文件夹
2. 进入该文件夹，双击 `astro-mountain.exe`；或在 PowerShell 里 `cd` 进去后执行  
   `.\astro-mountain.exe`
3. 进入中文菜单，按数字选择功能

> 若终端出现中文乱码，先执行 `chcp 65001` 切到 UTF-8 代码页。

### macOS (Apple Silicon)

```bash
tar -xzf astro-mountain_<version>_darwin_arm64.tar.gz
cd astro-mountain-darwin-arm64

# 本项目未做代码签名与公证，首次运行需解除 Gatekeeper 隔离属性
xattr -d com.apple.quarantine ./astro-mountain

chmod +x ./astro-mountain
./astro-mountain
```

> 如果 `xattr` 提示 `No such xattr`，说明系统没有给它打隔离标记，直接运行即可。

### 目录结构

解压后应当是这样：

```
astro-mountain-darwin-arm64/
├── astro-mountain          # 可执行文件
├── README.md
└── configs/
    ├── config.json         # 运行参数
    └── sites.json          # 观测点位
```

`configs/` 缺失也能跑：程序内置了同一份默认配置（`go:embed` 编译进二进制），  
只会在状态栏提示「内置默认」。

---

## 从源码构建

需要 **Go 1.22+**，无需 CGO、无需任何系统库。

```bash
git clone <repo-url>
cd wather-forecast

# 单平台构建（当前机器）
go generate ./internal/config && CGO_ENABLED=0 go build -o astro-mountain ./cmd/astro-mountain

# 跑测试
go test ./...

# 新人友好：make 会自动先跑 go generate 生成内置默认配置，再编译/测试，
# 免去「忘了生成 internal/config/defaults/*.json 导致裸 go build 失败」的坑。
make build   # 等价于上面的 go generate && go build
make test
```

### 一键交叉编译发布包

```bash
./build.sh                  # 构建 Windows x64 + macOS ARM，产物在 dist/
VERSION=v1.2.3 ./build.sh   # 显式指定版本号（默认取 git describe，兜底 dev）
./build.sh clean            # 删除 dist/
```

`build.sh` 会依次完成：

1. 校验仓库根 `configs/` 与内置 embed 默认（`internal/config/defaults/`）**逐字节一致**，  
   不一致直接失败——防止「改了发布包却忘了改内置默认」
2. 以 `CGO_ENABLED=0` 交叉编译 `windows/amd64` 与 `darwin/arm64`
3. 通过 `-ldflags "-X main.Version=..."` 注入版本号（`--version` 可读出）
4. 把可执行文件 + `configs/` + `README.md` 收进 `dist/astro-mountain-<goos>-<goarch>/`
5. 把每个目录压成可分发的归档（**目录与压缩包并存**，目录留着本地调试）
6. 回读刚生成的压缩包，断言里面确实有可执行文件、`configs/config.json`、  
   `configs/sites.json`、`README.md` 四样，少一样就构建失败

### 产物清单

`VERSION=v1.2.3 ./build.sh` 之后 `dist/` 长这样：

```
dist/
├── astro-mountain-windows-amd64/          # 未压缩，便于本地调试
│   ├── astro-mountain.exe
│   ├── README.md
│   └── configs/{config.json,sites.json}
├── astro-mountain-darwin-arm64/
│   ├── astro-mountain
│   ├── README.md
│   └── configs/{config.json,sites.json}
├── astro-mountain_v1.2.3_windows_amd64.zip      # 发布用
└── astro-mountain_v1.2.3_darwin_arm64.tar.gz    # 发布用
```

压缩包内的顶层目录名与未压缩目录一致（`astro-mountain-<goos>-<goarch>/`），  
所以[快速开始](#快速开始)里的 `cd astro-mountain-darwin-arm64` 解压后就能直接用。

| 平台          | 归档格式      | 为什么                                             |
| ----------- | --------- | ----------------------------------------------- |
| Windows x64 | `.zip`    | 资源管理器原生支持，右键即可解压，不用装第三方工具                       |
| macOS ARM   | `.tar.gz` | 保留 Unix 权限位，解压出来就是 `-rwxr-xr-x`，不用手动 `chmod +x` |

脚本结尾会打印每个压缩包的大小与 SHA-256，方便发版时贴到 Release 说明里。

---

## 配置文件说明

### 加载优先级

两份配置都遵循同一套优先级，**先命中先用**：

```
1. 命令行显式路径      --sites <path> / --config <path>
2. 当前工作目录        ./configs/sites.json   ./configs/config.json
3. 可执行文件同级目录  <exe_dir>/configs/sites.json   <exe_dir>/configs/config.json
4. 内置默认            go:embed 编译进二进制的那一份
```

失败处理约定：

- **文件不存在** → 用内置默认继续跑，状态栏标注「内置默认」
- **JSON 语法错误** → 报错退出，错误信息包含**文件路径 + 行号 + 列号**，  
  **不静默回退**（避免你以为自己的配置生效了其实没有），也**不覆盖你的原文件**
- **语法正确但缺字段** → 该字段沿用内置默认值，其余字段照常生效
- **个别点位字段非法** → 只跳过该点位并打印 warning，其余点位照常使用

### `configs/sites.json` — 观测点位

完整可用示例：

```json
{
  "version": 1,
  "updated": "2026-08-06",
  "sites": [
    {
      "name": "牵牛岗",
      "lat": 30.0260,
      "lon": 119.0070,
      "alt": 1489.9,
      "enabled": true,
      "note": "大明山最高峰；DEM 1340"
    },
    {
      "name": "括苍山",
      "lat": 28.8101,
      "lon": 120.9221,
      "alt": 1382.6,
      "enabled": true,
      "note": "主峰米筛浪；DEM 1347"
    },
    {
      "name": "备用机位",
      "lat": 30.4694,
      "lon": 119.5978,
      "alt": 958.4,
      "enabled": false,
      "note": "暂不参与计算"
    }
  ]
}
```

顶层字段：

| 字段        | 类型     | 默认值       | 说明            |
| --------- | ------ | --------- | ------------- |
| `version` | int    | `1`       | 配置格式版本，目前恒为 1 |
| `updated` | string | 写回时自动填当天  | 最后更新日期，仅供人看   |
| `sites`   | array  | 内置 20 个点位 | 点位数组          |

`sites[]` 每个元素：

| 字段        | 类型     | 必填 | 取值范围         | 说明                            |
| --------- | ------ | -- | ------------ | ----------------------------- |
| `name`    | string | 是  | 1~16 字符，不可重名 | 点位名称，出现在所有报表里                 |
| `lat`     | float  | 是  | −90 ~ 90     | 纬度，**建议 4 位小数以上**             |
| `lon`     | float  | 是  | −180 ~ 180   | 经度                            |
| `alt`     | float  | 是  | −500 ~ 9000  | 机位海拔（MSL 米），填**主峰海拔**         |
| `enabled` | bool   | 否  | —            | 缺省视为 `true`；`false` 时该点位不参与计算 |
| `note`    | string | 否  | —            | 备注，只在点位列表里展示                  |

> **兼容性**：也接受**裸数组**形态 `[{"name":...}, ...]`（即 Python 版 `--sites` 使用的格式），  
> 解析时自动识别顶层是对象还是数组，老配置无需改动。

> ⚠️ **坐标校准很重要**：`lat/lon` 要指向**主峰 / 山脊地标本身**，不是山脚停车场。  
> 本工具按机位经纬度取数值模式的网格柱，坐标落在谷地会让「云在头顶 / 机位在云中 /  
> 云海在脚下」的判定整体失效。`alt` 填你实际站立位置的海拔。

### `configs/config.json` — 运行参数

完整可用示例：

```json
{
  "version": 1,
  "api": {
    "endpoint": "https://api.open-meteo.com/v1/forecast",
    "elevation_endpoint": "https://api.open-meteo.com/v1/elevation",
    "models": "icon_seamless",
    "timezone": "Asia/Shanghai",
    "cache_enabled": true,
    "cache_dir": ".cache_astro",
    "cache_expire_s": 1800,
    "retries": 5,
    "backoff_factor": 0.2,

    "tomorrow_enabled": true,
    "tomorrow_endpoint": "https://api.tomorrow.io/v4/weather/forecast",
    "tomorrow_api_key": "",
    "tomorrow_cloud_base_unit": "km",
    "tomorrow_cloud_base_datum": "agl",
    "tomorrow_timeout_s": 20,
    "tomorrow_cache_expire_s": 21600,
    "tomorrow_quota_per_day": 500,
    "tomorrow_quota_per_hour": 25,
    "tomorrow_min_interval_ms": 400,

    "meteoblue_enabled": true,
    "meteoblue_endpoint": "https://my.meteoblue.com/packages",
    "meteoblue_api_key": ""
  },
  "window": {
    "night_start_hour": 22,
    "night_end_hour": 6,
    "core_start_hour": 23,
    "core_end_hour": 5
  },
  "thresholds": {
    "cloud_cover_threshold": 40.0,
    "rh_threshold_low": 90.0,
    "rh_threshold_high": 80.0,
    "rh_low_layer_pressure_min": 850,
    "fog_visibility_m": 1000.0,
    "haze_visibility_m": 5000.0,
    "fog_calm_wind_ms": 2.0,
    "fog_proxy_rh_high": 98.0,
    "fog_proxy_rh_warn": 95.0,
    "overhead_severe_cc": 70.0,
    "mid_cloud_veil_cc": 80.0,
    "high_cloud_thin_veil_cc": 80.0,
    "layer_min_half_span_frac": 0.25,
    "min_level_height_msl": 0.0,
    "cloud_sea_max_depth_m": 1500.0,
    "profile_lowcloud_crosscheck": 40.0,
    "cloud_sea_suspect_lowcloud": 85.0,
    "dew_spread_c": 3.0,
    "lcl_warn_agl_m": 300.0,
    "lcl_alert_agl_m": 100.0,
    "astro_dark_sun_alt": -18.0,
    "moon_bright_illum": 0.30
  },
  "output": {
    "out_dir": "./reports",
    "douyin_dir": "./reports/douyin",
    "auto_douyin": true,
    "export_csv": false,
    "export_json": false,
    "default_days": 5
  },
  "douyin": {
    "width": 1080,
    "height": 1920,
    "safe_bottom": 1900,
    "sections": ["点位列表", "天文条件", "核心窗口", "低云海拔评估明细"],
    "page_rows": 4,
    "table_split_threshold": 6,
    "hard_floor_scale": 0.4,
    "font_path": "",
    "font_candidates": [
      "/System/Library/Fonts/Hiragino Sans GB.ttc",
      "/System/Library/Fonts/Supplemental/Songti.ttc",
      "/System/Library/Fonts/PingFang.ttc",
      "/Library/Fonts/Arial Unicode.ttf",
      "C:/Windows/Fonts/msyh.ttc",
      "C:/Windows/Fonts/msyhbd.ttc",
      "C:/Windows/Fonts/simhei.ttf",
      "C:/Windows/Fonts/simsun.ttc",
      "C:/Windows/Fonts/Deng.ttf",
      "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
      "/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
      "/usr/share/fonts/opentype/noto/NotoSansCJKsc-Regular.otf"
    ]
  }
}
```

#### `api` — 数据源与网络

| 字段                   | 类型     | 默认值                                       | 说明                                       |
| -------------------- | ------ | ----------------------------------------- | ---------------------------------------- |
| `endpoint`           | string | `https://api.open-meteo.com/v1/forecast`  | 预报接口地址                                   |
| `elevation_endpoint` | string | `https://api.open-meteo.com/v1/elevation` | DEM 高程复核接口                               |
| `models`             | string | `icon_seamless`                           | 数值模式；可换 `best_match` / `ecmwf_ifs04` 等   |
| `timezone`           | string | `Asia/Shanghai`                           | 请求时区，决定返回时间戳的本地化                         |
| `cache_enabled`      | bool   | `true`                                    | 是否启用磁盘缓存；与 `--no-cache` 是「与」关系，任一为关即不走缓存 |
| `cache_dir`          | string | `.cache_astro`                            | 缓存目录                                     |
| `cache_expire_s`     | int    | `1800`                                    | 缓存有效期（秒），默认 30 分钟                        |
| `retries`            | int    | `5`                                       | 请求失败重试次数                                 |
| `backoff_factor`     | float  | `0.2`                                     | 重试退避因子，间隔按其递增                            |

以上字段服务于 A 轨（Open-Meteo）。下面这组只在 `--source tomorrow`（B 轨）时才读，
不给 `--source` 时**完全不生效、也不会发起任何 Tomorrow.io 请求**：

| 字段                          | 类型     | 默认值                                            | 说明                                                    |
| --------------------------- | ------ | ---------------------------------------------- | ----------------------------------------------------- |
| `tomorrow_enabled`          | bool   | `true`                                         | B 轨总开关。设为 `false` 后 `--source tomorrow` 会以退出码 `2` 中止  |
| `tomorrow_endpoint`         | string | `https://api.tomorrow.io/v4/weather/forecast`  | 预报接口地址                                                |
| `tomorrow_api_key`          | string | `""`                                           | 兜底密钥。**优先级低于环境变量 `TOMORROW_API_KEY`**，建议留空走环境变量       |
| `tomorrow_cloud_base_unit`  | string | `km`                                           | 厂商 `cloudBase` 字段单位：`auto` / `m` / `ft` / `km`         |
| `tomorrow_cloud_base_datum` | string | `agl`                                          | 云底高度基准：`agl`（离地）/ `msl`（海拔）。填错会让云-机位关系整体判反            |
| `tomorrow_timeout_s`        | int    | `20`                                           | 单次请求超时秒数                                              |
| `tomorrow_cache_expire_s`   | int    | `21600`                                        | 响应缓存有效期（秒），默认 6 小时；`<=0` 表示不缓存。缓存命中不扣配额              |
| `tomorrow_quota_per_day`    | int    | `500`                                          | 每天请求上限（Free 档实测值）；`<=0` 表示不设限                         |
| `tomorrow_quota_per_hour`   | int    | `25`                                           | 每小时请求上限（Free 档实测值）；`<=0` 表示不设限                        |
| `tomorrow_min_interval_ms`  | int    | `400`                                          | 两次请求最小间隔（毫秒），用于压住每秒 3 次的硬限，出厂值留了余量                   |

> `tomorrow_cloud_base_datum` 是这组里唯一会**静默算错**的字段：单位填错通常会撞出量级
> 离谱的数被判为语义失效，基准填错却能得到一个「看起来合理」的错误结论。改它之前请先
> 确认厂商返回的到底是离地高度还是海拔。

以下这组只在 `--source meteoblue`（C 轨）时才读，不给 `--source` 或给别的源时
**完全不生效、也不会发起任何 Meteoblue 请求**。C 轨只有 `Basic-1h`（降水/温度/风向）
+ `Clouds-1h`（低/中/高/总云量、能见度）两包，没有气压层，**不反演云海几何**：

| 字段                   | 类型     | 默认值                              | 说明                                                                       |
| -------------------- | ------ | -------------------------------- | ------------------------------------------------------------------------ |
| `meteoblue_enabled`  | bool   | `true`                           | C 轨总开关。设为 `false` 后 `--source meteoblue` 会以退出码 `2` 中止                  |
| `meteoblue_endpoint` | string | `https://my.meteoblue.com/packages` | 融合预报接口地址                                                                 |
| `meteoblue_api_key`  | string | `""`                             | 兜底密钥。**优先级低于环境变量 `METEOBLUE_API_KEY`**，建议留空走环境变量                     |

#### `window` — 时间窗口（北京时间，闭区间，跨零点）

| 字段                 | 类型  | 默认值  | 说明           |
| ------------------ | --- | ---- | ------------ |
| `night_start_hour` | int | `22` | 夜间窗口起始小时     |
| `night_end_hour`   | int | `6`  | 夜间窗口结束小时（次日） |
| `core_start_hour`  | int | `23` | 核心窗口起始小时     |
| `core_end_hour`    | int | `5`  | 核心窗口结束小时（次日） |

> **所有统计口径（通透小时数、最优机位）都用核心窗口**。观测夜编号把整夜含  
> 次日凌晨归到同一天：`2026-08-13` 夜 = 08-13 22:00 → 08-14 06:00。

#### `thresholds` — 判据阈值

| 字段                            | 类型    | 默认值      | 说明                                               |
| ----------------------------- | ----- | -------- | ------------------------------------------------ |
| `cloud_cover_threshold`       | float | `40.0`   | 气压层云量 ≥ 该值（%）判为「有云层」                             |
| `rh_threshold_low`            | float | `90.0`   | 云量缺测时的 RH 兜底阈值（气压 ≥ `rh_low_layer_pressure_min`） |
| `rh_threshold_high`           | float | `80.0`   | 云量缺测时的 RH 兜底阈值（更高层）                              |
| `rh_low_layer_pressure_min`   | int   | `850`    | 区分「低层 / 高层」RH 兜底的气压分界（hPa）                       |
| `fog_visibility_m`            | float | `1000.0` | 能见度 < 该值（米）判「雾」                                  |
| `haze_visibility_m`           | float | `5000.0` | 能见度 < 该值判「轻雾」                                    |
| `fog_calm_wind_ms`            | float | `2.0`    | 风速 < 该值（m/s）判辐射雾，否则平流雾                           |
| `fog_proxy_rh_high`           | float | `98.0`   | 无能见度数据时，近地 RH ≥ 该值代理判「雾」                         |
| `fog_proxy_rh_warn`           | float | `95.0`   | 无能见度数据时，近地 RH ≥ 该值代理判「轻雾」                        |
| `overhead_severe_cc`          | float | `70.0`   | 头顶云量 ≥ 该值（%）判 🔴不宜，40~70% 判 ⚠️风险                 |
| `mid_cloud_veil_cc`          | float | `80.0`   | **薄云兜底·中云**：地表中云量（3–8km，`cloud_cover_mid`）≥ 该值（%）判 ⚠️风险，成片中云盖顶、星野受损。剖面看不到 3km 以上，故作兜底 |
| `high_cloud_thin_veil_cc`    | float | `80.0`   | **薄云兜底·高云**：地表高云量（8km+ 卷云，`cloud_cover_high`）≥ 该值（%）判 ⚠️风险，头顶薄卷云、星野略受损。与中云是**互不重叠的两个独立判据** |
| `layer_min_half_span_frac`    | float | `0.25`   | 单层云的最小半跨度比例，防止插值出零厚度层                            |
| `min_level_height_msl`        | float | `0.0`    | 位势高度低于该值的层视为地下外推假值，剔除                            |
| `cloud_sea_max_depth_m`       | float | `1500.0` | 云海厚度超过该值时措辞降级（不再说「薄云海」）                          |
| `profile_lowcloud_crosscheck` | float | `40.0`   | 地表低云量交叉校验阈值（%）                                   |
| `cloud_sea_suspect_lowcloud`  | float | `85.0`   | 低云量 ≥ 该值时对「云海在脚下」结论存疑                            |
| `dew_spread_c`                | float | `3.0`    | 温露差 < 该值（℃）提示结露风险                                |
| `lcl_warn_agl_m`              | float | `300.0`  | LCL 估算高度 < 该值（米 AGL）给出提示                         |
| `lcl_alert_agl_m`             | float | `100.0`  | LCL 估算高度 < 该值给出强提示                               |
| `astro_dark_sun_alt`          | float | `-18.0`  | 天文暗夜判据：太阳高度 ≤ 该值（度）                              |
| `moon_bright_illum`           | float | `0.30`   | 月光干扰判据：月相照度 ≥ 该值（0~1）视为亮月                        |

> LCL 提示**不参与主评级**，只作为辅助信息出现在 `note` 字段里。

#### 两套「头顶有云」机制，别混淆

本工具判 ⚠️风险 / 🔴不宜 时有**两条互不相干**的「头顶云」来源，阈值也分属不同字段：

| 机制 | 数据来源 | 阈值字段 | 物理含义 |
| --- | --- | --- | --- |
| **剖面反演的头顶云** | 气压层剖面（1000→700hPa，约 0–3km）插值出的云层 | `overhead_severe_cc` | 剖面能看到的云（机位附近、云海在脚下等），靠反演 |
| **薄云兜底（地表分层云量）** | 数值模式直接给出的 `cloud_cover_mid` / `cloud_cover_high` | `mid_cloud_veil_cc` / `high_cloud_thin_veil_cc` | 剖面**看不到** 3km 以上（中云 3–8km、高云 8km+），靠地表产品补盲 |

为什么需要兜底：气压层剖面顶到 ~700hPa≈3km，**物理上永远看不到 3km 以上的云**。当剖面判定「全层无云」却头顶实际有中云/高云盖顶时，薄云兜底会把评级降到 ⚠️风险，避免「剖面说通透、实际头顶一层盖子」的误判。

薄云兜底里 `mid`（3–8km 中云）与 `high`（8km+ 卷云）是**互不重叠的两个独立高度层**，因此拆成两条独立判据、各自阈值、各自出提示：

- 中云过阈 → 提示「中云量 X%（3–8km，剖面之外），成片中云盖顶，星野受损」——**实质遮挡，建议换点或放弃**
- 高云过阈 → 提示「高云量 X%（8km 以上卷云），头顶薄卷云，星野略受损」——**仅减光，通常还能拍**

两者同时过阈时两条提示都出现。两个阈值当前同为 `80.0` 只是默认值巧合，可按需分别调整（例如把高云阈值调高，让薄卷云只提示不降级）。

#### `output` — 产物输出

| 字段             | 类型     | 默认值                | 说明                                                             |
| -------------- | ------ | ------------------ | -------------------------------------------------------------- |
| `out_dir`      | string | `./reports`        | Markdown / CSV / JSON 输出目录；被 `--out-dir` 覆盖                    |
| `douyin_dir`   | string | `./reports/douyin` | 抖音竖版图输出目录；**给了 `--out-dir` 时该字段失效**，图片改落到 `<out-dir>/douyin`   |
| `auto_douyin`  | bool   | `true`             | 生成报告后是否自动出图；`--douyin` / `--no-douyin` 可覆盖，`--no-douyin` 优先级最高 |
| `export_csv`   | bool   | `false`            | 是否默认导出 CSV；与 `--csv` 是「或」关系——设为 `true` 后**没有 `--no-csv` 能关掉它** |
| `export_json`  | bool   | `false`            | 是否默认导出 JSON；与 `--json` 同样是「或」关系                                |
| `default_days` | int    | `5`                | `--peak` 未配 `--days` 时往前推的天数；也是无任何日期参数时的默认区间长度                 |

#### `douyin` — 竖版出图

| 字段                      | 类型       | 默认值    | 说明                       |
| ----------------------- | -------- | ------ | ------------------------ |
| `width`                 | int      | `1080` | 画布宽度（px）                 |
| `height`                | int      | `1920` | 画布高度（px），9:16            |
| `safe_bottom`           | int      | `1900` | 底部安全线，内容不得越过             |
| `sections`              | []string | 见示例    | 默认渲染的小节关键词               |
| `page_rows`             | int      | `4`    | 超长表分页时每页保留的数据行数          |
| `table_split_threshold` | int      | `6`    | 行数超过该值的表格才考虑分页           |
| `hard_floor_scale`      | float    | `0.4`  | 字号自适应缩放的硬下限              |
| `font_path`             | string   | `""`   | **强制指定**中文字体路径；留空则按候选表探测 |
| `font_candidates`       | []string | 见示例    | 字体探测候选表，按序取第一个可加载的       |

---

## 命令行参数

`astro-mountain --help` 会打印同一份清单。下表是**全部** 21 个选项（外加 `-h` 简写），  
与 `internal/cli/flags.go` 一一对应，没有隐藏参数。

**「默认值」列的读法**：写「取配置 xxx」的，表示不给这个参数时值来自配置文件，  
括号里是发布默认配置的取值；写 `false` 的都是开关，不给就是关。

### 时间范围

`--peak` 与 `--start/--end` **互斥**，同时给出会以退出码 `2` 报错。  
两组都不给时，退化成「自今天起 `output.default_days` 个观测夜」。

| 参数                   | 默认值                            | 说明                                                      |
| -------------------- | ------------------------------ | ------------------------------------------------------- |
| `--peak YYYY-MM-DD`  | 无                              | 流星雨极大日                                                  |
| `--days N`           | 取配置 `output.default_days`（`5`） | 在极大日基础上额外向前包含 N 天，`N ≥ 1`。**只在配合 `--peak` 时有意义**，单独给会报错 |
| `--start YYYY-MM-DD` | 无                              | 起始日期，必须与 `--end` 成对出现                                   |
| `--end YYYY-MM-DD`   | 无                              | 结束日期，不得早于 `--start`                                     |

> 日期只认严格的 `YYYY-MM-DD`。`2026-8-12` 这种单位数写法会被拒绝——它和报告 /  
> CSV 里的日期键对不上，收下了反而一条都匹配不到。

### 数据源

| 参数               | 默认值                                      | 说明                                                                            |
| ---------------- | ---------------------------------------- | ----------------------------------------------------------------------------- |
| `--source NAME`  | `openmeteo`                              | 用哪个气象源来算，`openmeteo`（A 轨）、`tomorrow`（B 轨）或 `meteoblue`（C 轨）。详见下方[数据源选择](#数据源选择a-轨--b-轨--c-轨)          |
| `--models NAMES` | 取配置 `api.models`（`icon_seamless`）        | Open-Meteo 数值模式，可换 `best_match` / `ecmwf_ifs04` 等。**只对 A 轨生效**                |
| `--sites PATH`   | 按[加载优先级](#加载优先级)查找 `configs/sites.json`  | 点位 JSON 路径                                                                    |
| `--config PATH`  | 按[加载优先级](#加载优先级)查找 `configs/config.json` | 运行参数 JSON 路径                                                                  |
| `--no-cache`     | `false`                                  | 禁用磁盘缓存，强制重新请求。**没有 `--cache` 反向参数**，想常开缓存请设配置 `api.cache_enabled: true`（默认已开） |

#### 数据源选择（A 轨 / B 轨 / C 轨）

本工具有三条**互斥**的数据轨，一次运行只走一条，报告里只会出现这一条的结论：

| 取值          | 轨   | 数据源                      | 需要 key | 说明                                        |
| ----------- | --- | ------------------------ | ------ | ----------------------------------------- |
| `openmeteo` | A 轨 | Open-Meteo 免费 API        | 否      | **默认**。多气压层廓线反演云底，不限量                     |
| `tomorrow`  | B 轨 | Tomorrow.io Free 档       | 是      | 直接读厂商给的 `cloudBase` 字段，无需反演；受配额限制         |
| `meteoblue` | C 轨 | Meteoblue 融合预报          | 是      | 山地高分辨率分层云量+降水+能见度；**不反演云海几何**（无气压层） |

```bash
# A 轨（不给 --source 时的默认行为，与老版本完全一致）
astro-mountain --peak 2026-08-13 --days 3

# B 轨
export TOMORROW_API_KEY=你的密钥
astro-mountain --source tomorrow --peak 2026-08-13 --days 3

# C 轨
export METEOBLUE_API_KEY=你的密钥
astro-mountain --source meteoblue --peak 2026-08-13 --days 3
```

**密钥从哪来**：优先读环境变量 `TOMORROW_API_KEY` / `METEOBLUE_API_KEY`，其次读配置  
`api.tomorrow_api_key` / `api.meteoblue_api_key`。推荐用环境变量——配置文件容易连着密钥一起被提交进 git。

> **不会替你偷偷换轨**。`--source tomorrow` 或 `--source meteoblue` 只要有一项不满足  
> （配置 `api.tomorrow_enabled` / `api.meteoblue_enabled` 为 `false` / 没有密钥 /  
> 本次构建未注入取数器），程序**以退出码 `2` 中止**并说明是哪一层拦下的，**不会**用 A 轨替你出一份你没要的报告。
>
> 「你要某轨、我给了 A 轨、还不告诉你」是本工具定义的最坏失败模式——数值口径完全不同，  
> 拿错口径的结论上山比拿不到结论危险得多。所以这里选择**拒绝，而不是静默算错**。

##### C 轨边界（务必先读）

C 轨的 Meteoblue `Basic-1h` + `Clouds-1h` 数据**只有分层云量、降水、能见度**，**没有气压层廓线**，  
因此本工具对它**不反演云海几何**：

- 报告里**不会**出现「云海在脚下 / 机位在云中 / 头顶薄云」这类几何结论；
- 它只判**通透度（云量遮挡）**、**降水**、**能见度**，以及日出日落前后雾/轻雾对机位的遮蔽；
- 想看云海高程判定，请改用 `--source openmeteo`（A 轨）。

这条边界是 C 轨接入时写死的硬规则：`metaSourceOf` 把它标记为 `Meteoblue`，报告署名与免责声明  
都明确标注「不反演云海几何」，**绝不**把 A 轨的云海几何结论冒充到 C 轨报告里。

##### B 轨配额

Tomorrow.io Free 档的三重限速，工具内置了本地账本，会在发请求前先预算：

| 维度  | 上限        | 对应配置                        |
| --- | --------- | --------------------------- |
| 每天  | **500 次** | `api.tomorrow_quota_per_day`  |
| 每小时 | **25 次**  | `api.tomorrow_quota_per_hour` |
| 每秒  | **3 次**   | `api.tomorrow_min_interval_ms`（出厂 `400` ms，即 2.5 次/秒，留了余量） |

**消耗按「点位数」计**，不按天数——一次请求就把该点位整段预报窗都拿回来了。  
所以 20 个点位跑一轮 = 20 次请求，每小时最多跑 1 轮（25 次上限）。

配额打满时**不中止、也不换轨**：该轮所有小时判为 `❓无数据` 并标注 `[配额耗尽]`，  
报告表头照常写「Tomorrow.io（B 轨）」，退出码 `0`。理由同上——宁可如实说没算出来，  
也不能拿 A 轨的数冒充 B 轨的结论。响应默认缓存 6 小时  
（`api.tomorrow_cache_expire_s`），同一段预报窗内反复跑不会重复扣配额。

### 输出

| 参数               | 默认值                               | 说明                                                          |
| ---------------- | --------------------------------- | ----------------------------------------------------------- |
| `--out-dir PATH` | 取配置 `output.out_dir`（`./reports`） | 产物输出目录。给了它之后抖音图也跟着走 `<out-dir>/douyin`，配置里的 `douyin_dir` 让位 |
| `--csv`          | 取配置 `output.export_csv`（`false`）  | 额外导出 CSV。与配置是「或」关系，**没有 `--no-csv`**                        |
| `--json`         | 取配置 `output.export_json`（`false`） | 额外导出 JSON。同样没有 `--no-json`                                  |
| `--no-report`    | `false`                           | 跳过 Markdown 报告。**没有 `--report`**，不给就是要报告                    |
| `--douyin`       | 取配置 `output.auto_douyin`（`true`）  | 强制生成抖音竖图                                                    |
| `--no-douyin`    | 同上                                | 强制跳过抖音竖图                                                    |

> `--douyin` 与 `--no-douyin` 是一对，同时给会报错。三态优先级：  
> **`--no-douyin` > `--douyin` > 配置 `output.auto_douyin`**。  
> 显式关排在最前面——用户说了不要，就绝不产出。

### 运行方式

| 参数              | 默认值     | 说明                                        |
| --------------- | ------- | ----------------------------------------- |
| `--menu`        | 见下方进入规则 | 强制进入交互菜单                                  |
| `--no-menu`     | 同上      | 强制不进菜单，直接执行                               |
| `--quiet`       | `false` | 不打印终端报表（文件照常产出）。**没有 `--loud`**           |
| `--verbose`     | `false` | 打印调试日志到 stderr：每次 HTTP 请求、缓存命中、字体探测       |
| `--version`     | `false` | 打印版本号后退出（版本号由 `build.sh` 用 `-ldflags` 注入） |
| `--help` / `-h` | `false` | 显示帮助后退出                                   |

> `--menu` 与 `--no-menu` 是一对，同时给会报错。  
> `--quiet` 与 `--verbose` **不冲突**：前者管终端报表，后者管 stderr 调试日志，  
> 可以一起用（安静跑但留下排障日志）。

### 是否进入交互菜单

按顺序判断，先命中先决定：

| 优先级 | 条件             | 结果          |
| --- | -------------- | ----------- |
| 1   | 给了 `--no-menu` | 不进，直接执行     |
| 2   | 给了 `--menu`    | 进           |
| 3   | 给了任一**业务参数**   | 不进，直接执行     |
| 4   | 以上都不满足         | stdin 是终端才进 |

第 3 条的「业务参数」指：`--peak` `--days` `--start` `--end` `--source` `--models`  
`--sites` `--out-dir` `--csv` `--json` `--no-report` `--no-cache` `--douyin` `--no-douyin`。  
给了其中任意一个，说明你已经把要跑什么说清楚了，不该再被菜单拦一道。

`--config` `--quiet` `--verbose` `--menu` `--no-menu` `--version` `--help`  
**不算**业务参数：前三个只是运行方式修饰，后四个是元操作。  
所以 `astro-mountain --verbose` 仍然会进菜单，只是菜单里多打调试日志。

### 退出码

| 码   | 含义                                       |
| --- | ---------------------------------------- |
| `0` | 成功（交互菜单正常退出、`--version` / `--help` 也是 0） |
| `1` | 运行期失败：无有效数据 / 导出失败                       |
| `2` | 参数或配置错误：日期格式错、互斥参数冲突、配置 JSON 语法错、`--source tomorrow` 不可用 |

出图失败、单点取数失败这类只降级不致命的情况仍返回 `0`，详见  
[Q8. 退出码是什么含义](#q8-退出码是什么含义)。

---

## 使用示例

以下命令均可直接复制执行（`astro-mountain` 换成你的可执行文件路径）。

### 1. 进入交互菜单（推荐新手）

```bash
astro-mountain
```

无参数且 stdin 是终端时自动进入中文菜单。也可以用 `--menu` 强制进入：

```bash
astro-mountain --menu
```

### 2. 流星雨极大日模式

英仙座极大日是 2026-08-13，评估极大日往前 5 天（共 6 夜）：

```bash
astro-mountain --peak 2026-08-13 --days 5
```

产出 `reports/astro_report_peak-2026-08-13.md`，并按 `auto_douyin` 自动出图。

### 3. 自定义起止日期区间

```bash
astro-mountain --start 2026-08-10 --end 2026-08-14
```

产出 `reports/astro_report_2026-08-10_2026-08-14.md`。

> `--peak` 与 `--start/--end` 互斥，同时给出会报错退出。

### 4. 同时导出 CSV + JSON 明细

```bash
astro-mountain --peak 2026-08-13 --days 5 --csv --json --out-dir ./out
```

产出 `out/astro_report_peak-2026-08-13.md` / `.csv` / `.json` 三份文件  
（CSV 与 JSON 与报告同名，只是扩展名不同）。

### 5. 只要报告不要图（跳过出图）

```bash
astro-mountain --peak 2026-08-13 --days 5 --no-douyin
```

字体缺失、跑在无 GUI 的服务器上时用这个最省事。

### 6. 只要数据不要报告

```bash
astro-mountain --start 2026-08-10 --end 2026-08-12 --csv --no-report --no-douyin
```

适合接进定时任务只取结构化数据。

### 7. 指定自己的点位文件 + 换数值模式

```bash
astro-mountain --peak 2026-08-13 --sites ~/my-sites.json --models best_match --verbose
```

### 8. 强制重新拉取（绕过 30 分钟缓存）

```bash
astro-mountain --peak 2026-08-13 --no-cache
```

### 9. 接入 crontab

```bash
# 每天 18:00 评估未来 3 天并导出 CSV
0 18 * * * cd /opt/astro && ./astro-mountain --no-menu --start $(date +\%F) --end $(date -d '+3 days' +\%F) --csv --no-douyin >> run.log 2>&1
```

> **无人值守场景请显式加 `--no-menu`。** 它是唯一不依赖环境探测的保证，  
> 一定不会进菜单、一定不会等输入。
>
> 不加也通常没事：上面这条命令已经给了 `--start/--end/--csv` 等业务参数，  
> 按[进入规则](#是否进入交互菜单)第 3 条就不会进菜单。真正需要当心的是  
> **一个业务参数都不给的裸跑**——此时只剩第 4 条「stdin 是不是终端」这一道判断，  
> 而 cron、`systemd`、CI runner、`nohup`、`</dev/null` 各自把 stdin 接成什么，  
> 取决于调度器实现，不该赌。

### 10. 只出图不刷屏 / 查版本

```bash
# 安静模式：终端不打表格，但照常产出报告与抖音图
astro-mountain --peak 2026-08-13 --quiet --douyin

# 安静但留调试日志：--quiet 管终端报表，--verbose 管 stderr 日志，两者不冲突
astro-mountain --peak 2026-08-13 --quiet --verbose 2> debug.log

# 查版本（build.sh 注入）与完整参数清单
astro-mountain --version
astro-mountain --help
```

---

## 输出产物说明

### Markdown 报告

文件名规则：

| 启动方式                                  | 文件名                                     |
| ------------------------------------- | --------------------------------------- |
| `--peak 2026-08-13`                   | `astro_report_peak-2026-08-13.md`       |
| `--start 2026-08-10 --end 2026-08-14` | `astro_report_2026-08-10_2026-08-14.md` |

五章节结构：

| 章节           | 内容                                                       |
| ------------ | -------------------------------------------------------- |
| 一、元信息        | 数值模式、日期范围、时区、数据来源、免责声明；**1.1 点位列表**                      |
| 二、各观测夜汇总     | **2.1 天文条件**（月相/月高/银心高度/天文暗夜）；**2.2 核心窗口通透小时数矩阵**；综合最优机位 |
| 三、拍摄影响因素权重说明 | 9 个因素的经验权重表（合计 100%），区分「脚本自动计算」与「需人工/外部数据」               |
| 四、低云海拔评估明细   | 每夜一张表（`### YYYY-MM-DD 夜`），每点位一行，11 列                     |
| 五、导出字段说明     | CSV / JSON 全部 30 个字段的中文对照与含义                             |

> **即使一条有效数据都没取到，报告也一定会生成**，并在正文里写明原因  
> （网络不可达 / 超出预报时效 / 模式名拼错）。跑了就一定留痕。

### CSV 明细

- 编码：**UTF-8 with BOM**（`utf-8-sig`），Excel 双击打开中文不乱码
- 表头：中文
- 30 个字段，**列顺序固定不可调整**（下游脚本与 Excel 模板按列序号取值）：

```
site, alt, night, time, has_data, rating, relation,
cloud_base_agl, cloud_base_msl, cloud_top_agl, cloud_top_msl,
cloud_thickness, layer_max_cc, cloud_low, cloud_mid, cloud_high,
visibility, temp, dew, spread, wind_ms,
boundary_layer_height, freezing_level_height, lcl_agl_est,
sun_alt, moon_alt, moon_illum, gc_alt, astro_dark, note
```

对应中文表头依次为：点位、机位海拔(m)、观测夜、时间(ISO)、有数据、评级、云层状态、  
云底相对机位(m)、云底海拔(m)、云顶相对机位(m)、云顶海拔(m)、云厚(m)、最大层云量(%)、  
低云量(%)、中云量(%)、高云量(%)、能见度(m)、气温(°C)、露点(°C)、温露差(°C)、风速(m/s)、  
边界层高度(m)、冻结层高度(m)、LCL估算高度(m)、太阳高度(°)、月亮高度(°)、月相照度(%)、  
银心高度(°)、天文暗夜、判断说明。

> **缺测值写空单元格，不写 0**。这是安全红线在导出层的体现：`0` 和「没数据」  
> 在下游统计里含义天差地别。

### JSON 明细

包含四部分：`rows`（含完整气压层剖面数组）、`field_labels`（字段中文标签）、  
`config`（本次生效的全部阈值）、`meta`（元信息）。适合二次分析与可视化。

### 抖音竖版图

- 尺寸：1080 × 1920（9:16），深色星空风 PNG
- 输出目录：`douyin_dir`（默认 `reports/douyin`）
- 命名规则：`<报告文件名去扩展名>_<小节slug>[_<日期>]_<页码>.png`

小节 slug 映射：

| 小节关键词    | slug           |
| -------- | -------------- |
| 点位列表     | `sites`        |
| 天文条件     | `astro`        |
| 核心窗口     | `transparency` |
| 低云海拔评估明细 | `cloud_detail` |

示例：

```
astro_report_peak-2026-08-13_sites.png
astro_report_peak-2026-08-13_astro.png
astro_report_peak-2026-08-13_transparency.png
astro_report_peak-2026-08-13_cloud_detail_2026-08-08_1.png
astro_report_peak-2026-08-13_cloud_detail_2026-08-08_2.png
```

「低云海拔评估明细」会按 `### YYYY-MM-DD 夜` 三级子节拆成独立图组；单页放不下时  
按每页 4 行分页，每页重复表头，标题带全角「（p/total）」分页标记。

> **出图失败不影响报告**。字体缺失、渲染异常都只在结尾打印一行警告，  
> 已生成的 Markdown / CSV / JSON 完全不受影响，退出码仍为 0。

---

## 评级含义

| 评级       | 含义         | 判据                                                                                  |
| -------- | ---------- | ----------------------------------------------------------------------------------- |
| ✅**通透**  | 头顶通透，可拍摄   | 全层无云（`CLEAR`），或云顶低于机位海拔（`SEA_BELOW` 云海在脚下）且无雾、无降水                                       |
| ⚠️**风险** | 有风险，需现场判断  | 头顶云量 40~~70%（剖面反演）；或**薄云兜底**：地表中云量 ≥ `mid_cloud_veil_cc`（3–8km 中云盖顶）或高云量 ≥ `high_cloud_thin_veil_cc`（8km+ 薄卷云）；或轻雾（能见度 1000~~5000m，或近地 RH ≥ 95%）；或温露差 < 3℃ 有结露风险；或低云量交叉校验对「云海在脚下」结论存疑；或机位处于「湿层软判据」（模式云量 < 40% 且仅由近地湿度识别、地表 RH < 98%，疑为起雾/低云，需现场确认） |
| 🔴**不宜** | 不宜拍摄       | **降水 / 恶劣天气码**（`wcode` 51–99 中的雨/雪/冻雨/雷暴类，**硬否决、优先级最高**，不折衷）；或头顶云量 ≥ 70%（`OVERHEAD` 严重）；或机位在云中（`IN_CLOUD`，且非「湿层软判据」）；或有雾（能见度 < 1000m，或近地 RH ≥ 98%） |
| ❓**无数据** | 无气象数据，无从判断 | 气压层剖面（8 层云量 + 相对湿度）**全部缺测** → 廓线为空（`NODATA`）。返回前会先判定地表降水 / 雾（已修复旧盲区：廓线空 + 下雨曾被误报无数据），只有地表也无降水 / 雾信号时才报无数据 |

### 汇总表两个易混字段：主要状态 vs 主要诱因

每夜汇总表有两个长得像、但含义完全不同的列，别看串了：

| 列 | 它回答什么 | 取值示例 | 能否单独判断点位好坏 |
| --- | --- | --- | --- |
| **主要状态** | 当晚「云在头顶还是脚下」的几何关系（取出现次数最多的逐时云层关系） | `云海在脚下` / `头顶通透` / `机位在云中` / `头顶薄云` | **不能**——它只描述几何，不解释评级 |
| **主要诱因** | 把结论（🔴/⚠️）压低到这个档位的**根因**是什么 | `浓雾（能见度<1000m）` / `降水 / 雷暴` / `机位在云中` / `头顶厚云（云量≥70%）` / `中云盖顶（3–8km）` / `高云洗天（8km+）` / `头顶薄云（云量40–70%）` / `轻雾/霾` / `结露 / LCL 风险` | 能——它点名了「为什么不是 ✅」 |

> **为什么需要两列？** 因为「几何状态好」和「当晚能不能拍」是两件事。一个典型反例：
> 某点位整夜 `云海在脚下`（机位在云层之上，头顶没有云），但**地表起了浓雾**或**机位卡在贴地湿层里**——
> 这时 `主要状态=云海在脚下` 看着很美，结论却是 `🔴 建议放弃`。以前只有几何状态、没有诱因列，
> 看起来就像「明明云海在脚下却叫你放弃」，自相矛盾。
>
> 现在 `主要诱因` 会直接点名根因（例如 `浓雾` / `降水` / `机位在云中`），让「建议放弃」不再是没来由的硬标签。
> **结论列（✅/⚠️/🔴/❓）才是当晚能不能去的唯一权威判据**，`主要状态` 与 `主要诱因` 都只是帮你看懂这个结论的附注。
>
> 提取规则：只在 `🔴不宜` / `⚠️风险` 时次里采样逐时 `Note`，按「**最频次 + 严重度优先**」提炼——
> 硬否决（降水 / 浓雾 / 在云中 / 头顶厚云）优先于软降级（薄云兜底 / 轻雾 / 结露），同严重度取出现次数最多者；
> 全是 ✅ 或无有效数据时该列为空。

### 汇总表第三个易误读的列：「日出窗云海」

这一列**只看日出前后那一小段**，和看整夜的「主要状态」不是一回事。窗口长度见配置
`sunrise_window_before_min` / `sunrise_window_after_min`；窗口内无采样时次则记「无」。

| 取值 | 含义 | 该怎么读 |
| --- | --- | --- |
| `有` | 日出窗内出现 ✅ 级可见云海 | 直接守候日出云海 |
| `有（被山顶雾/降水遮蔽）` | 日出窗内几何上有云海，但被山顶雾或降水压成了不宜 | 云海在，但拍不到；等雾散或换一天 |
| `辐射雾` | 日出窗内被辐射雾遮蔽（贴地 + 静风 + 晴夜少云形成） | **别急着放弃**——辐射雾贴地，日出后大概率消散，可守候破云与云海 |
| `辐射雾（云海）` | 日出窗内既有 ✅ 级可见云海、又有辐射雾 | 脚下云海与贴地辐射雾同框、静风，**最该守候的题材** |
| `无` | 日出窗内没有任何云海时次 | 这一天的日出云海别指望了 |

「辐射雾」档括号里还会给出雾层相对机位的高度范围，用来判断无人机能不能飞出雾顶、
起降是不是在雾中：

| 括号写法 | 含义 |
| --- | --- |
| `机下Xm·机上Ym` | 雾层跨过机位：脚下 X m 到头顶 Y m 都有雾，机位正在雾里 |
| `机下a~bm` | 雾全在机位下方：浅处 a m、深处 b m |
| `机上a~bm` | 雾全在机位上方：a m 到 b m，机位在雾底下 |

> 取值优先级：**可见云海 + 辐射雾同框 > 可见云海 > 辐射雾遮蔽 > 其他遮蔽 > 无**。
> 所以 `辐射雾（云海）` 一旦出现，云海和雾两条信息是同时给出的，不会只报一个。

### ⚠️ 安全红线：❓无数据 ≠ 晴朗

这是本工具最重要的一条设计约束，请务必理解：

**当数值模式在某个时次没有返回有效数据时，工具输出 `❓无数据`，绝不会退化成 `✅通透`。**

原因很直接：把「不知道」当成「晴朗」，代价是你半夜开三小时山路上去，  
发现头顶一层厚云。反过来把「不知道」标成「不知道」，代价只是你多查一个数据源。

因此实现上有几条硬规则：

- 所有可能缺测的数值都用「缺测安全的浮点数」承载，JSON 的 `null` 解析成  
  `Valid=false`，**绝不退化成 `0`**（`0` 会被误判为「云量 0%，晴朗」）
- 气压层剖面为空时直接返回 `NODATA`，不走任何「默认晴朗」分支；返回前会先判定地表降水 / 雾（已修复旧盲区：廓线空 + 下雨曾被误报无数据），只有地表也无降水 / 雾信号时才退回 `NODATA`
- CSV / JSON 导出时缺测写空值，不写 0
- 有专门的自动化用例：构造云量 / 湿度 / 能见度全缺测的时次，断言评级**必为**  
  `❓无数据` 且 `has_data=false`

看到 `❓无数据` 时，请按「未知」对待，出发前用其它渠道（卫星云图、实时摄像头、  
当地群友）复核。

### 云-机位关系分类

| 关系          | 中文    | 含义                       |
| ----------- | ----- | ------------------------ |
| `CLEAR`     | 全层无云  | 剖面内没有任何达到云量阈值的层          |
| `SEA_BELOW` | 云海在脚下 | 机位海拔 > 云顶海拔 —— 拍云海的理想状态  |
| `IN_CLOUD`  | 机位在云中 | 云底 ≤ 机位海拔 ≤ 云顶 —— 你正泡在云里 |
| `OVERHEAD`  | 云在头顶  | 机位海拔 < 云底海拔 —— 头顶一层盖子    |
| `NODATA`    | 无数据   | 剖面为空，无法判断（**绝不等同于通透**）   |

### 算法局限

评估算法在绝大多数情况下准确，但存在以下边界，理解它们能避免误读：

- **廓线为空时的旧盲区（已修复）**：早期版本在 8 层气压剖面（云量 + 相对湿度）整体缺测时直接返回 `❓无数据` 并**跳过**地表降水 / 雾判据，「廓线空 + 实际在下雨」的时次会被标成 `❓无数据` 而非 `🔴不宜`（实测复现：廓线 8 层全缺测 + 降水 2mm + `wcode` 61 → 仍返回 `❓无数据`）。现已修复：降水 / 雾判据被抽成 helper，在廓线为空的分支里也先调用，地表信号明确就按地表判，绝不回退成无数据；仅当地表也无降水 / 雾信号时才报 `❓无数据`。该修复由 `TestEvaluateHourEmptyLevelsPrecipStillBad` / `...FogStillBad` / `...HazeWarns` 三条用例钉死。`icon_seamless` 几乎总会返回这 8 层，实际触发极少，但兜底逻辑现在是对的。
- **「云海在脚下」+ 雾的文案**：当机位下方有云海、同时地表有雾时，评级会正确降到 `🔴不宜`，但说明里仍保留「云海在脚下……头顶通透，最佳拍摄条件」的短语（来自 `SEA_BELOW` 分支），随后才追加雾的判据。请读完整说明，不要被前半句「最佳拍摄条件」误导。
- **阈值为经验值**：`cloud_cover_threshold`(40%)、`overhead_severe_cc`(70%)、`mid/high_cloud_veil_cc`(80%)、`fog_*`、`dew_spread_c` 等均为针对华东山区夏季的经验阈值，依据真实样本（如冷湖镇 2026-08-09 剖面 8 层全 0、mid 过阈）调校，并非普适物理常数；换区域 / 换季节建议重新校准。

---

## 常见问题

### Q1. 提示「未找到可用的中文字体」，抖音图出不来

工具按 `douyin.font_candidates` 顺序探测系统字体，全部不可用时会报错并**列出已尝试的  
全部路径**。解决办法：在 `configs/config.json` 里显式指定一个中文 TrueType 字体：

```json
{
  "douyin": {
    "font_path": "/System/Library/Fonts/Hiragino Sans GB.ttc"
  }
}
```

也可以走菜单：`[4] 运行参数设置 → 中文字体路径`，改完会立刻重新探测并告诉你成功与否。

各平台常见可用字体：

| 平台      | 推荐路径                                                                              |
| ------- | --------------------------------------------------------------------------------- |
| macOS   | `/System/Library/Fonts/Hiragino Sans GB.ttc`、`/System/Library/Fonts/PingFang.ttc` |
| Windows | `C:/Windows/Fonts/msyh.ttc`（微软雅黑）、`C:/Windows/Fonts/simhei.ttf`（黑体）               |
| Linux   | `/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc`、Noto CJK 系列                        |

支持 `.ttf` / `.ttc` / `.otf`。**指定了但文件不可用时会直接报错，不会静默降级**——  
静默降级只会让你拿到一张全是豆腐块的图。

> 字体只影响抖音出图。终端表格、Markdown、CSV、JSON 都不依赖字体，  
> 实在配不好就加 `--no-douyin`。

### Q2. API 超时 / 请求失败

- 工具默认重试 5 次，退避因子 0.2。可在 `api.retries` / `api.backoff_factor` 调整
- **单个点位取数失败不会中断整轮运行**，只记一条 warning，其余点位照常计算
- 全部点位都失败时报告仍会生成，正文写明失败原因
- 加 `--verbose` 可以看到每次 HTTP 请求与缓存命中情况，便于定位是网络问题还是参数问题

如果确认是网络受限，可以换个数值模式试试：

```bash
astro-mountain --peak 2026-08-13 --models best_match --verbose
```

### Q3. 数据好像是旧的 / 想强制重新拉取

本地缓存默认 30 分钟。两种绕过方式：

```bash
# 方式一：本次运行禁用缓存
astro-mountain --peak 2026-08-13 --no-cache

# 方式二：直接删缓存目录（默认 .cache_astro，见 api.cache_dir）
rm -rf .cache_astro
```

也可以在菜单里改：`[4] 运行参数设置 → HTTP 缓存`，或在报告流程的  
`步骤 4/4 高级选项 → [3] HTTP 缓存` 里临时关掉。

### Q4. Windows 终端中文乱码

PowerShell / CMD 默认代码页不是 UTF-8。执行一次：

```powershell
chcp 65001
```

然后重新运行程序。如果表格竖线仍然错位，把终端字体换成等宽中文字体  
（如「Cascadia Mono」+「微软雅黑」回退，或直接用 Windows Terminal）。

CSV 用 Excel 打开乱码的情况本工具已经处理过了——导出时会写 UTF-8 BOM，  
双击直接就是中文。若你用的是 WPS 且仍乱码，导入时手动选 UTF-8 编码即可。

### Q5. 报告里全是「❓无数据」

按以下顺序排查：

1. **日期超出预报时效**。Open-Meteo forecast 端点最远约 +15 天、最早约 −90 天；  
   `icon_seamless` 模式的有效预报时效约 180 小时（7.5 天）。流星雨极大若还很远，  
   临近再跑
2. **网络不可达** `api.open-meteo.com`，加 `--verbose` 看请求日志
3. **`--models` 拼写错误**，换成 `best_match` 试试
4. 确认不是缓存了一份空结果——加 `--no-cache` 重跑

再次强调：`❓无数据` **不代表天气好**，只代表工具拿不到数据。

### Q6. 我的机位不在内置的 20 个点位里

三种办法，都不需要改代码：

1. **走菜单**（推荐）：`[3] 点位配置管理 → [a] 新增点位`，逐项填完选 `[s]` 保存写回
2. **直接编辑** `configs/sites.json`，参考上面的字段说明表
3. **用独立文件**：`astro-mountain --sites ~/my-sites.json --peak 2026-08-13`

菜单保存时会自动把原文件备份为 `sites.json.bak`，并采用「写临时文件 → 校验可解析 →  
原子替换」，中途崩溃也不会把你的配置写坏。

改坏了想恢复？菜单里 `[3] → [x] 导出内置默认点位到文件` 一键还原。

### Q7. 判定结果和实际不符

优先检查**坐标精度**。这是最常见的原因：

- `lat/lon` 要指向主峰 / 山脊地标本身，不是山脚停车场或上山公路起点
- `alt` 填你实际站立位置的海拔（MSL），不是山体平均高度
- 数值模式的网格有平滑效应，DEM 高程通常比真实主峰低 100~200m，属正常范围；  
  但如果差 400m 以上，多半是坐标点落在谷地了

点位备注里那些「DEM 1340」就是这个用途——记录该坐标处的模式高程，方便对照。

### Q8. 退出码是什么含义

（速查表也在[命令行参数 › 退出码](#退出码)一节。）

| 退出码 | 含义                                                 |
| --- | -------------------------------------------------- |
| `0` | 成功（**含「有警告但产物已生成」的情况**）                            |
| `1` | 运行期失败：一条有效数据都没取到，或 CSV/JSON 导出失败                   |
| `2` | 参数或配置错误：日期格式错、`--peak` 与 `--start` 冲突、配置 JSON 语法错误 |

**出图失败、单点取数失败、缓存写失败都只是 warning，不会改变退出码。**  
主结论（报告 + 数据）有效就算成功，强行置非零会让 CI 与定时任务误判整轮失败。

交互菜单里某一次评估失败也不会让菜单退出——失败信息展示在界面上，你可以留在  
菜单里改参数重试。菜单正常退出（选 `[0]`、输入 `q`、stdin 读到 EOF、Ctrl-C）  
一律返回 `0`。

---

## 许可与致谢

- 气象数据来源：[Open-Meteo](https://open-meteo.com/) 免费 API
- 天文量（月相、月高、银心高度、天文暗夜）为纯计算近似算法结果，不下载星历
- 云底 / 云顶为气压层剖面反演值，非实测

**本工具的输出仅供出行参考。山区天气变化极快，实际情况请以现场判断为准，  
安全第一。**
