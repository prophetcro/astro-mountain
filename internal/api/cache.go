package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Cache 是按 URL 哈希落盘的响应缓存，用文件修改时间判断过期。
//
// 所有方法都对 nil 与未启用状态安全（直接视为不命中），
// 调用方不必先判空。进程内用互斥锁串行化，但不跨进程加锁。
type Cache struct {
	dir     string
	ttl     time.Duration
	enabled bool

	mu sync.Mutex
}

// NewCache 构造磁盘缓存。目录为空或 ttl 非正时得到一个不启用的缓存，
// 后续读写都是安全的空操作。
func NewCache(dir string, ttl time.Duration) *Cache {
	return &Cache{
		dir:     dir,
		ttl:     ttl,
		enabled: dir != "" && ttl > 0,
	}
}

// Disabled 返回一个永不命中、永不落盘的缓存，用于 --no-cache 场景。
func Disabled() *Cache { return &Cache{} }

// Enabled 报告缓存是否真的会读写磁盘。
func (c *Cache) Enabled() bool { return c != nil && c.enabled }

// KeyOf 用请求 URL 的 SHA-256 作为缓存键：URL 已包含站点、日期与变量列表，
// 参数变了自然就是另一份缓存。
func KeyOf(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}

func (c *Cache) path(key string) string {
	return filepath.Join(c.dir, key+".json")
}

// Get 读取未过期的缓存内容。任何异常（不存在、过期、读失败、空文件）
// 都只报不命中，不返回错误——缓存失效不该让取数流程失败。
func (c *Cache) Get(key string) ([]byte, bool) {
	if !c.Enabled() {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.path(key)
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return nil, false
	}
	if time.Since(info.ModTime()) > c.ttl {
		// 顺手清掉过期文件，避免缓存目录只涨不消。
		_ = os.Remove(p)
		return nil, false
	}
	data, err := os.ReadFile(p)
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// Has 判断某个键是否有未过期且非空的缓存文件，不读取内容。
func (c *Cache) Has(key string) bool {
	if !c.Enabled() {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	info, err := os.Stat(c.path(key))
	if err != nil || info.IsDir() || info.Size() == 0 {
		return false
	}
	return time.Since(info.ModTime()) <= c.ttl
}

// Put 写入缓存。先写临时文件再原子改名，避免进程中途退出留下半截文件
// 被后续读成有效缓存。未启用时直接返回 nil。
func (c *Cache) Put(key string, data []byte) error {
	if !c.Enabled() {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("创建缓存目录 %s 失败：%w", c.dir, err)
	}
	p := c.path(key)
	tmp, err := os.CreateTemp(c.dir, "tmp-*")
	if err != nil {
		return fmt.Errorf("创建缓存临时文件失败：%w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("写缓存临时文件失败：%w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("关闭缓存临时文件失败：%w", err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("落盘缓存 %s 失败：%w", p, err)
	}
	return nil
}

// Clear 删除缓存目录下的所有文件（子目录保留不动）。
// 只要设置过目录就能清，不受 enabled 影响，这样 --no-cache 时也能手动清理。
// 目录不存在视为已清空。
func (c *Cache) Clear() error {
	if c == nil || c.dir == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取缓存目录 %s 失败：%w", c.dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		_ = os.Remove(filepath.Join(c.dir, e.Name()))
	}
	return nil
}
