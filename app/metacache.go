package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"db-g/engine"
)

// MetaCacheTTL 是元数据磁盘缓存的有效期；期内直接命中，不向目标库查询。
const MetaCacheTTL = 24 * time.Hour

// MetaCachePayload 是一个数据源的目录元数据快照。
// schema 用空串表示默认 schema；objects 只保存已经分页加载过的对象。
type MetaCachePayload struct {
	Fingerprint string                                             `json:"fingerprint"`
	SavedAt     int64                                              `json:"savedAt"`
	Schemas     []string                                           `json:"schemas,omitempty"`
	Counts      map[string]map[engine.ObjectKind]int               `json:"counts,omitempty"`
	Objects     map[string]map[engine.ObjectKind][]engine.DBObject `json:"objects,omitempty"`
}

// MetaFingerprint 是连接档案的缓存身份：指向同一个库的档案才共享缓存。
// 不含密码、名称与展示顺序。
func MetaFingerprint(p ConnProfile) string {
	return strings.Join([]string{
		p.Kind, p.Path, p.Host, strconv.Itoa(p.Port), p.User,
		p.Database, p.Protocol, p.Params,
	}, "\x00")
}

// MetaCache 是数据源目录元数据的磁盘缓存，一个连接一个 JSON 文件。
// 只做尽力而为的缓存：读写失败记日志，不打断连接流程。
type MetaCache struct {
	mu  sync.Mutex
	dir string
	ttl time.Duration
	now func() time.Time
	seq map[string]uint64 // 进程内写入序号，防止旧快照后写覆盖新快照
}

func NewMetaCache(dir string) *MetaCache {
	return NewMetaCacheWithTTL(dir, MetaCacheTTL)
}

func NewMetaCacheWithTTL(dir string, ttl time.Duration) *MetaCache {
	return &MetaCache{dir: dir, ttl: ttl, now: time.Now, seq: map[string]uint64{}}
}

func (c *MetaCache) path(profileID string) string {
	return filepath.Join(c.dir, profileID+".json")
}

// Load 读取缓存。hit 为 false 表示文件缺失/损坏/指纹不匹配；
// stale 为 true 表示仍可使用，但应在后台静默重取一次。
func (c *MetaCache) Load(p ConnProfile) (payload MetaCachePayload, hit, stale bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := os.ReadFile(c.path(p.ID))
	if err != nil {
		if !os.IsNotExist(err) {
			LogWarn("catalog", "meta cache read failed", "conn", p.ID, "err", err.Error())
		}
		return MetaCachePayload{}, false, false
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		LogWarn("catalog", "meta cache corrupt", "conn", p.ID, "err", err.Error())
		return MetaCachePayload{}, false, false
	}
	if payload.Fingerprint != MetaFingerprint(p) {
		LogInfo("catalog", "meta cache fingerprint mismatch", "conn", p.ID)
		return MetaCachePayload{}, false, false
	}
	stale = c.now().UnixMilli()-payload.SavedAt >= int64(c.ttl/time.Millisecond)
	return payload, true, stale
}

// Save 整体回写缓存；调用方传入的 payload 应是快照，写入失败只记日志。
func (c *MetaCache) Save(p ConnProfile, payload MetaCachePayload) error {
	return c.save(p, payload, 0)
}

// SaveOrdered 是带进程内序号的 Save：并发后台写时旧快照不会覆盖新快照。
func (c *MetaCache) SaveOrdered(p ConnProfile, payload MetaCachePayload, seq uint64) error {
	return c.save(p, payload, seq)
}

func (c *MetaCache) save(p ConnProfile, payload MetaCachePayload, seq uint64) error {
	payload.Fingerprint = MetaFingerprint(p)
	payload.SavedAt = c.now().UnixMilli()
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seq == nil {
		c.seq = map[string]uint64{}
	}
	if seq > 0 {
		if seq < c.seq[p.ID] {
			return nil
		}
		c.seq[p.ID] = seq
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		LogWarn("catalog", "meta cache mkdir failed", "dir", c.dir, "err", err.Error())
		return err
	}
	tmp, err := os.CreateTemp(c.dir, ".meta-*.tmp")
	if err != nil {
		LogWarn("catalog", "meta cache temp failed", "conn", p.ID, "err", err.Error())
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, c.path(p.ID)); err != nil {
		LogWarn("catalog", "meta cache write failed", "conn", p.ID, "err", err.Error())
		return err
	}
	return nil
}

// Delete 删除一个连接的缓存（删除连接、改连接地址时）。
func (c *MetaCache) Delete(profileID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seq == nil {
		c.seq = map[string]uint64{}
	}
	// 让删除前已经发出、还没落盘的后台写失效。
	c.seq[profileID]++
	if err := os.Remove(c.path(profileID)); err != nil && !os.IsNotExist(err) {
		LogWarn("catalog", "meta cache delete failed", "conn", profileID, "err", err.Error())
	}
}

// DeleteAll 清空整个缓存目录的文件（手动重载元数据时）。
func (c *MetaCache) DeleteAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seq == nil {
		c.seq = map[string]uint64{}
	}
	for id := range c.seq {
		c.seq[id]++
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if !os.IsNotExist(err) {
			LogWarn("catalog", "meta cache list failed", "dir", c.dir, "err", err.Error())
		}
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if err := os.Remove(filepath.Join(c.dir, e.Name())); err != nil {
			LogWarn("catalog", "meta cache delete failed", "file", e.Name(), "err", err.Error())
		}
	}
}

// MetaCachePath 便于日志与诊断。
func (c *MetaCache) MetaCachePath(p ConnProfile) string {
	return c.path(p.ID)
}
