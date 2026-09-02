package config

import _ "embed"

// 内置默认配置的嵌入源。
//
// go:embed 不允许引用包目录之外的路径，因此嵌入源必须物理落在
// internal/config/defaults/ 下。它不是 configs/ 的平级副本，而是派生物：
// 仓库根 configs/ 是唯一真相源，defaults/ 由 build.sh 在编译前从 configs/ 重新生成。

// 重新生成 embed 源：configs/ 是唯一真相源，defaults/ 由其派生。
// 脱离 build.sh 时可用 `go generate ./internal/config` 重建，避免裸 go build 失败。
//
// mkdir -p 不能省：defaults/*.json 已被 .gitignore 排除，全新 clone 后这个目录
// 本身不存在（go:generate 的工作目录是包目录 internal/config/，故此处用相对路径
// defaults），裸 cp 会因父目录缺失而失败，导致裸 go build 一并断裂。
//go:generate sh -c "mkdir -p defaults && cp ../../configs/config.example.json defaults/config.json && cp ../../configs/sites.json defaults/sites.json"

//go:embed defaults/sites.json
var defaultSitesJSON []byte

//go:embed defaults/config.json
var defaultConfigJSON []byte

// DefaultSitesJSON 返回内置默认 sites.json 的原始字节，供 --init-config 导出。
// 返回的是副本，调用方修改不会影响嵌入数据。
func DefaultSitesJSON() []byte {
	out := make([]byte, len(defaultSitesJSON))
	copy(out, defaultSitesJSON)
	return out
}

// DefaultConfigJSON 返回内置默认 config.json 的原始字节，供 --init-config 导出。
// 返回的是副本，调用方修改不会影响嵌入数据。
func DefaultConfigJSON() []byte {
	out := make([]byte, len(defaultConfigJSON))
	copy(out, defaultConfigJSON)
	return out
}
