package app

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// 连接 URL 导入的通用错误；UI 只按类型给本地化提示，绝不回显原始 URL
// （URL 里可能带密码）。
var (
	ErrConnURLInvalid     = errors.New("invalid connection URL")
	ErrConnURLUnsupported = errors.New("unsupported connection URL")
)

// ParsedConnURL 是从 JDBC / URI 里解析出的连接字段；Port 为 0 表示省略，
// 由 UI 按数据源类型和 ClickHouse 协议填默认端口。
type ParsedConnURL struct {
	Kind     string // mysql / postgres / clickhouse / sqlite
	Protocol string // 仅 ClickHouse：native / http / https
	Host     string
	Port     int
	User     string
	Password string
	Database string
	Path     string
	Params   string
	Name     string // 建议的连接名：网络库用 database/host，SQLite 用文件名
}

// ParseConnURL 解析常见的 JDBC / 标准 URI / SQLAlchemy URL：
//
//	jdbc:mysql://user:pw@host:3306/db?useSSL=false
//	jdbc:postgresql://host:5432/db?sslmode=require
//	jdbc:clickhouse:https://host:8443/db
//	jdbc:sqlite:/tmp/a.db
//	postgresql+psycopg2://user:pw@host/db
//
// 只做字符串解析，不连接、不校验凭据。
func ParseConnURL(raw string) (ParsedConnURL, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ParsedConnURL{}, ErrConnURLInvalid
	}
	hadJDBC := false
	if len(s) >= 5 && strings.EqualFold(s[:5], "jdbc:") {
		hadJDBC = true
		s = strings.TrimSpace(s[5:])
	}
	lower := strings.ToLower(s)
	if hadJDBC {
		base := lower
		if i := strings.IndexByte(base, ':'); i >= 0 {
			base = base[:i]
		}
		switch base {
		case "mysql", "mariadb", "postgres", "postgresql", "clickhouse", "sqlite", "file":
		default:
			return ParsedConnURL{}, ErrConnURLUnsupported
		}
	}
	switch {
	case strings.HasPrefix(lower, "sqlite:") || strings.HasPrefix(lower, "file:"):
		return parseSQLiteURL(s)
	case strings.HasPrefix(lower, "clickhouse:http://"):
		return parseNetworkURL("http://"+s[len("clickhouse:http://"):], "clickhouse", "http")
	case strings.HasPrefix(lower, "clickhouse:https://"):
		return parseNetworkURL("https://"+s[len("clickhouse:https://"):], "clickhouse", "https")
	case strings.HasPrefix(lower, "clickhouse:native://"):
		return parseNetworkURL("clickhouse://"+s[len("clickhouse:native://"):], "clickhouse", "native")
	}
	return parseNetworkURL(s, "", "")
}

// parseSQLiteURL 保留 SQLite 的本地路径语义：去掉 scheme 后原样作为 Path，
// 包括 :memory:、Windows 盘符和 ?mode=ro 之类的查询串。
func parseSQLiteURL(s string) (ParsedConnURL, error) {
	lower := strings.ToLower(s)
	rest := ""
	switch {
	case strings.HasPrefix(lower, "sqlite:"):
		rest = s[len("sqlite:"):]
	case strings.HasPrefix(lower, "file:"):
		rest = s[len("file:"):]
	}
	if strings.HasPrefix(rest, "//") {
		rest = rest[2:]
	}
	if rest == "" {
		return ParsedConnURL{}, ErrConnURLInvalid
	}
	return ParsedConnURL{Kind: "sqlite", Path: rest, Name: sqliteName(rest)}, nil
}

func sqliteName(path string) string {
	name := path
	if i := strings.IndexByte(name, '?'); i >= 0 {
		name = name[:i]
	}
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	if strings.HasPrefix(name, ":") && strings.HasSuffix(name, ":") {
		name = strings.Trim(name, ":")
	}
	if name == "" {
		return "SQLite"
	}
	return name
}

// parseNetworkURL 解析带 host 的 URL；kind/protocol 非空时覆盖 scheme 推断。
func parseNetworkURL(raw, kind, protocol string) (ParsedConnURL, error) {
	i := strings.Index(raw, "://")
	if i <= 0 {
		return ParsedConnURL{}, ErrConnURLInvalid
	}
	scheme := strings.ToLower(raw[:i])
	base, driver, _ := strings.Cut(scheme, "+")
	if kind == "" {
		switch base {
		case "mysql", "mariadb":
			kind = "mysql"
		case "postgres", "postgresql":
			kind = "postgres"
		case "clickhouse":
			kind = "clickhouse"
			switch driver {
			case "native", "http", "https":
				protocol = driver
			}
		default:
			return ParsedConnURL{}, ErrConnURLUnsupported
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ParsedConnURL{}, ErrConnURLInvalid
	}
	out := ParsedConnURL{Kind: kind, Host: u.Hostname()}
	if ps := u.Port(); ps != "" {
		p, err := strconv.Atoi(ps)
		if err != nil || p <= 0 || p > 65535 {
			return ParsedConnURL{}, ErrConnURLInvalid
		}
		out.Port = p
	}
	if u.User != nil {
		out.User = u.User.Username()
		out.Password, _ = u.User.Password()
	}
	q := u.Query()
	if v := firstParam(q, "user", "username", "userName"); v != "" {
		out.User = v
	}
	if v := firstParam(q, "password", "pass"); v != "" {
		out.Password = v
	}
	out.Database = strings.TrimPrefix(u.Path, "/")
	if v := firstParam(q, "database", "db", "dbname", "databaseName"); v != "" {
		out.Database = v
	}
	deleteParams(q, "user", "username", "userName", "password", "pass", "database", "db", "dbname", "databaseName")
	out.Params = q.Encode()
	if kind == "clickhouse" {
		if protocol == "" {
			switch out.Port {
			case 8123:
				protocol = "http"
			case 8443:
				protocol = "https"
			default:
				protocol = "native"
			}
		}
		out.Protocol = protocol
	}
	if out.Database != "" {
		out.Name = out.Database
	} else {
		out.Name = out.Host
	}
	return out, nil
}

func firstParam(q url.Values, names ...string) string {
	for _, want := range names {
		for key, vals := range q {
			if strings.EqualFold(key, want) && len(vals) > 0 {
				return vals[0]
			}
		}
	}
	return ""
}

func deleteParams(q url.Values, names ...string) {
	for key := range q {
		for _, want := range names {
			if strings.EqualFold(key, want) {
				delete(q, key)
				break
			}
		}
	}
}
