package app

import (
	"bufio"
	"encoding/json"
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

// HistoryFileMax 是历史文件轮转阈值。
const HistoryFileMax = 5 << 20

// HistoryLimit 是启动时载入内存的最近条数上限。
const HistoryLimit = 2000

// DefaultHistoryPath 返回历史文件路径（与连接库同级）。
func DefaultHistoryPath() string {
	return filepath.Join(filepath.Dir(DefaultStorePath()), "history.jsonl")
}

// History 是执行历史（追加写 JSONL，内存保留最近 N 条）。
type History struct {
	mu      sync.Mutex
	path    string
	entries []HistoryEntry // 旧 → 新
}

// OpenHistory 读取最近的历史（坏行跳过）；文件缺失为空历史。
func OpenHistory(path string) *History {
	h := &History{path: path}
	f, err := os.Open(path)
	if err != nil {
		return h
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e HistoryEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // 坏行跳过
		}
		h.entries = append(h.entries, e)
	}
	if len(h.entries) > HistoryLimit {
		h.entries = h.entries[len(h.entries)-HistoryLimit:]
	}
	return h
}

// Add 追加一条历史并落盘；超阈值时轮转到 .1。
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
	if dir := filepath.Dir(h.path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if fi, err := os.Stat(h.path); err == nil && fi.Size()+int64(len(data)) > HistoryFileMax {
		_ = os.Remove(h.path + ".1")
		_ = os.Rename(h.path, h.path+".1")
	}
	f, err := os.OpenFile(h.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
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
	h.entries = append(h.entries, e)
	if len(h.entries) > HistoryLimit {
		h.entries = h.entries[len(h.entries)-HistoryLimit:]
	}
	return nil
}

// List 返回最近的条目（新的在前）。
func (h *History) List(limit int) []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	if limit <= 0 || limit > len(h.entries) {
		limit = len(h.entries)
	}
	out := make([]HistoryEntry, 0, limit)
	for i := len(h.entries) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, h.entries[i])
	}
	return out
}

// Path 返回历史文件路径。
func (h *History) Path() string { return h.path }
