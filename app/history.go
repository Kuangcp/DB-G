package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// HistoryEntry 是一条执行历史。
type HistoryEntry struct {
	Time     int64  `json:"ts"`
	ConnID   string `json:"connID,omitempty"`
	ConnName string `json:"connName,omitempty"`
	SQL      string `json:"sql"`
	DurMS    int64  `json:"durMS,omitempty"`
	OK       bool   `json:"ok"`
	Rows     int    `json:"rows,omitempty"`
	Err      string `json:"err,omitempty"`
}

// HistoryKeep 是裁剪后保留的条数；HistoryTrimAt 是触发裁剪的条数。
// 每追加到 6000 条才重写一次文件，回到 5000 条，减少磁盘写。
const (
	HistoryKeep   = 5000
	HistoryTrimAt = 6000
)

// DefaultHistoryDir 返回按数据源分文件的历史目录。
func DefaultHistoryDir() string {
	return filepath.Join(filepath.Dir(DefaultStorePath()), "history")
}

// History 是执行历史：每个数据源一个 <connectionID>.jsonl，追加写、惰性裁剪。
type History struct {
	mu     sync.Mutex
	dir    string
	counts map[string]int // connID → 已知行数（首次追加时扫描建立）
}

// OpenHistory 打开历史目录（不存在时首次写入创建）。
func OpenHistory(dir string) *History {
	return &History{dir: dir, counts: map[string]int{}}
}

// Path 返回某数据源的历史文件路径。
func (h *History) Path(connID string) string {
	return filepath.Join(h.dir, historyFileName(connID))
}

// historyFileName 把连接 id 变成安全文件名；空 id 落到 default.jsonl。
func historyFileName(connID string) string {
	if connID == "" {
		connID = "default"
	}
	var b strings.Builder
	for _, r := range connID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if name == "" || name == "." || name == ".." {
		name = "default"
	}
	return name + ".jsonl"
}

// Add 追加一条历史并落盘；行数达到 HistoryTrimAt 时裁剪到 HistoryKeep。
// 只在该次裁剪时写一条日志，平时每次执行只有一次追加写。
func (h *History) Add(e HistoryEntry) error {
	if e.Time == 0 {
		e.Time = time.Now().UnixMilli()
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		return err
	}
	path := h.Path(e.ConnID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	count, ok := h.counts[e.ConnID]
	if ok {
		count++
	} else {
		count = countLines(path)
	}
	if count >= HistoryTrimAt {
		if err := trimHistoryFile(path, HistoryKeep); err != nil {
			// 裁剪失败不影响本次记录；下次追加再试。
			LogWarn("history", "trim failed", "conn", e.ConnID, "file", path, "err", err.Error())
			h.counts[e.ConnID] = count
			return nil
		}
		count = HistoryKeep
		LogInfo("history", "trimmed", "conn", e.ConnID, "file", path, "kept", HistoryKeep)
	}
	h.counts[e.ConnID] = count
	return nil
}

// List 返回某数据源最近 limit 条（新的在前）；limit<=0 表示全部。
func (h *History) List(connID string, limit int) []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	f, err := os.Open(h.Path(connID))
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	ring := make([]HistoryEntry, 0, max(limit, 16))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e HistoryEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // 坏行跳过
		}
		ring = append(ring, e)
		if limit > 0 && len(ring) > limit {
			ring = ring[len(ring)-limit:]
		}
	}
	out := make([]HistoryEntry, 0, len(ring))
	for i := len(ring) - 1; i >= 0; i-- {
		out = append(out, ring[i])
	}
	return out
}

// countLines 数文件里的行数；不存在返回 0。
func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	n := 0
	for sc.Scan() {
		n++
	}
	return n
}

// trimHistoryFile 原子地把文件重写为最后 keep 行（保留 0600）。
func trimHistoryFile(path string, keep int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	ring := make([]string, 0, keep)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		ring = append(ring, line)
		if len(ring) > keep {
			ring = ring[len(ring)-keep:]
		}
	}
	err = sc.Err()
	f.Close()
	if err != nil {
		return err
	}
	if len(ring) == 0 {
		return errors.New("history file is empty")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".history-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	for _, line := range ring {
		if _, err := tmp.WriteString(line + "\n"); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// MigrateLegacy 把旧的单个 history.jsonl 按 ConnID 拆进新目录，成功后改名为 .migrated。
// 失败时把原文件改名回去，下次启动再试。
func (h *History) MigrateLegacy(path string) error {
	if path == "" || !fileExists(path) {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	var entries []HistoryEntry
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e HistoryEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			entries = append(entries, e)
		}
	}
	err = sc.Err()
	f.Close()
	if err != nil {
		return err
	}
	working := path + ".migrating"
	if err := os.Rename(path, working); err != nil {
		return err
	}
	for _, e := range entries {
		if err := h.Add(e); err != nil {
			_ = os.Rename(working, path)
			return err
		}
	}
	if err := os.Rename(working, path+".migrated"); err != nil {
		return err
	}
	LogInfo("history", "migrated legacy file", "from", path, "entries", len(entries))
	return nil
}
