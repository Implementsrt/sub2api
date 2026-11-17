package upstreampricing

import (
	"bufio"
	"context"
	"database/sql"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	_ "modernc.org/sqlite"
)

// Read only auth_token for the selected exact origin in registered Firefox
// profiles. Never enumerate site storage, read cookies, copy databases or unlock
// browser files. This is an explicit local-admin action, not a background job.
func ReadLocalFirefoxTokens(ctx context.Context, baseURL string) ([]string, error) {
	if runtime.GOOS != "windows" || os.Getenv("APPDATA") == "" {
		return nil, safeError("本机读取仅支持 Windows 上运行的 Sub2API；其他部署请使用站点连接或手动输入")
	}
	return readFirefoxTokens(ctx, baseURL, filepath.Join(os.Getenv("APPDATA"), "Mozilla", "Firefox"))
}

func firefoxOrigin(baseURL string) (string, error) {
	c := Config{Provider: "sub2api", BaseURL: baseURL}
	if err := ValidateConfig(&c); err != nil {
		return "", err
	}
	u, _ := url.Parse(c.BaseURL)
	// This reader intentionally supports the HTTPS site layout used by Firefox.
	// Container/partitioned origins have suffixes and are never searched.
	if u.Scheme != "https" || strings.Contains(u.Hostname(), ":") {
		return "", safeError("Firefox 本机读取需要 HTTPS 站点地址")
	}
	host := strings.ToLower(u.Hostname())
	if u.Port() != "" && u.Port() != "443" {
		host += "+" + u.Port()
	}
	return "https+++" + host, nil
}

func firefoxProfiles(root string) ([]string, error) {
	f, err := os.Open(filepath.Join(root, "profiles.ini"))
	if err != nil {
		return nil, safeError("无法读取当前 Windows 用户的 Firefox 配置，请确认本机已安装并登录站点")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() > 1<<20 {
		return nil, safeError("Firefox 配置无法读取")
	}
	profiles := []string{}
	seen := map[string]bool{}
	section, path, relative := "", "", true
	flush := func() {
		if !strings.HasPrefix(section, "Profile") || path == "" || len(profiles) >= 32 {
			return
		}
		p := filepath.Clean(path)
		if relative {
			if filepath.IsAbs(p) || p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
				return
			}
			p = filepath.Join(root, p)
		} else if !filepath.IsAbs(p) {
			return
		}
		if !seen[p] {
			seen[p] = true
			profiles = append(profiles, p)
		}
	}
	scanner := bufio.NewScanner(io.LimitReader(f, 1<<20))
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			section, path, relative = strings.Trim(line, "[]"), "", true
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Path":
			path = strings.TrimSpace(value)
		case "IsRelative":
			relative = strings.TrimSpace(value) != "0"
		}
	}
	flush()
	if scanner.Err() != nil {
		return nil, safeError("Firefox 配置无法读取")
	}
	return profiles, nil
}

func readFirefoxTokens(ctx context.Context, baseURL, root string) ([]string, error) {
	origin, err := firefoxOrigin(baseURL)
	if err != nil {
		return nil, err
	}
	profiles, err := firefoxProfiles(root)
	if err != nil {
		return nil, err
	}
	tokens := []string{}
	seen, failed := map[string]bool{}, false
	for _, profile := range profiles {
		if ctx.Err() != nil {
			return nil, safeError("Firefox 读取超时，请重试")
		}
		path := filepath.Join(profile, "storage", "default", origin, "ls", "data.sqlite")
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		u := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
		if !strings.HasPrefix(u.Path, "/") {
			u.Path = "/" + u.Path
		}
		u.RawQuery = "mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(500)"
		db, err := sql.Open("sqlite", u.String())
		if err != nil {
			failed = true
			continue
		}
		var conversion, compression int
		var value []byte
		err = db.QueryRowContext(ctx, `SELECT conversion_type,compression_type,value FROM data WHERE key='auth_token' AND length(value)<=16384 LIMIT 1`).Scan(&conversion, &compression, &value)
		db.Close()
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil || conversion != 1 || compression != 0 {
			failed = true
			continue
		}
		token := string(value)
		if token != "" && !seen[token] {
			seen[token] = true
			tokens = append(tokens, token)
		}
	}
	if len(tokens) == 0 && failed {
		return nil, safeError("Firefox 站点存储暂不可只读访问或编码不支持，请关闭占用后重试，或使用站点连接")
	}
	return tokens, nil
}
