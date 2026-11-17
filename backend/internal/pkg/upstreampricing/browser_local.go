package upstreampricing

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

func ValidateLocalBrowserRequest(origin, host, scheme, remote string) error {
	if err := ValidateBrowserOrigin(origin, host, scheme); err != nil {
		return err
	}
	ip, _, err := net.SplitHostPort(remote)
	if err != nil || net.ParseIP(ip) == nil || !net.ParseIP(ip).IsLoopback() {
		return safeError("本机浏览器读取只接受来自本机回环地址的管理员请求")
	}
	return nil
}

// Claims narrow candidate selection only. The selected HTTPS station must
// authenticate the token and return the same user ID before it is imported.
func browserJWTIdentity(token string, now time.Time, requireFresh bool) (string, int64, bool) {
	if len(token) > 16384 || strings.ContainsAny(token, " \r\n\t") {
		return "", 0, false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", 0, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", 0, false
	}
	var claims map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	if decoder.Decode(&claims) != nil {
		return "", 0, false
	}
	identity := ""
	for _, key := range []string{"user_id", "sub", "uid"} {
		if claims[key] == nil {
			continue
		}
		value := stringValue(claims[key])
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n <= 0 || (identity != "" && value != identity) {
			return "", 0, false
		}
		identity = value
	}
	expires, err := strconv.ParseInt(stringValue(claims["exp"]), 10, 64)
	if identity == "" || err != nil || (requireFresh && expires <= now.Unix()) {
		return "", 0, false
	}
	if requireFresh && claims["nbf"] != nil {
		nbf, err := strconv.ParseInt(stringValue(claims["nbf"]), 10, 64)
		if err != nil || nbf > now.Unix() {
			return "", 0, false
		}
	}
	return identity, expires, true
}

func (m *Manager) ImportLocalFirefox(ctx context.Context, owner, id, revision int64, origin string, c Config) (browserImport, error) {
	return m.importLocalFirefox(ctx, owner, id, revision, origin, c, ReadLocalFirefoxTokens)
}

func (m *Manager) importLocalFirefox(ctx context.Context, owner, id, revision int64, origin string, c Config,
	read func(context.Context, string) ([]string, error)) (browserImport, error) {
	if c.Provider != "sub2api" {
		return browserImport{}, safeError("本机 Firefox 读取目前支持 Sub2API 用户登录凭证，New API 请使用站点连接")
	}
	c.Token, c.BrowserImportID = "", ""
	if err := ValidateConfig(&c); err != nil {
		return browserImport{}, err
	}
	src, err := m.Store.Source(ctx, id)
	if err != nil || src.Revision != revision {
		return browserImport{}, ErrConflict
	}
	fingerprint, err := m.Store.AccountFingerprint(ctx, id)
	if err != nil {
		return browserImport{}, err
	}
	expected := c.UserID
	if src.HasCredential && src.Config.Provider == c.Provider && src.Config.BaseURL == c.BaseURL && src.AccountFingerprint == fingerprint {
		old, err := m.Cipher.Decrypt(src.Credential)
		if err != nil {
			return browserImport{}, safeError("已保存凭证无法解密，请手动重新确认站点用户")
		}
		identity, _, ok := browserJWTIdentity(old, time.Now(), false)
		if !ok || (expected != "" && expected != identity) {
			return browserImport{}, safeError("无法确认已保存凭证的站点用户，请先手动确认身份")
		}
		expected = identity
	}
	tokens, err := read(ctx, c.BaseURL)
	if err != nil {
		return browserImport{}, err
	}
	type candidate struct {
		token, user string
		expires     int64
	}
	candidates := []candidate{}
	users, seen := map[string]bool{}, map[string]bool{}
	for _, token := range tokens {
		user, expires, valid := browserJWTIdentity(token, time.Now(), true)
		if valid && !seen[token] && (expected == "" || user == expected) && len(candidates) < 32 {
			seen[token], users[user] = true, true
			candidates = append(candidates, candidate{token, user, expires})
		}
	}
	if len(users) > 1 {
		return browserImport{}, safeError("Firefox 中该站点有多个用户，请先手动保存一次目标用户的登录凭证再读取")
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].expires > candidates[j].expires })
	for _, candidate := range candidates {
		c.Token = candidate.token
		me, err := m.Remote.get(ctx, c, "/api/v1/auth/me?timezone=Asia%2FShanghai")
		if err != nil {
			continue
		}
		data := object(me["data"])
		remoteID := stringValue(data["id"])
		if remoteID == "" {
			remoteID = stringValue(data["user_id"])
		}
		if remoteID != candidate.user {
			continue
		}
		currentFP, err := m.Store.AccountFingerprint(ctx, id)
		if err != nil || currentFP != fingerprint {
			return browserImport{}, ErrConflict
		}
		start, err := m.StartBrowserImport(ctx, owner, id, revision, origin, c)
		if err != nil {
			return browserImport{}, err
		}
		if err = m.CompleteBrowserImport(ctx, start.Ticket, origin, candidate.token, candidate.user); err != nil {
			return browserImport{}, err
		}
		return m.Imports.Status(start.ID, owner, id)
	}
	if len(candidates) > 0 {
		return browserImport{}, safeError("已找到同站点登录信息，但站点用户验证失败，请检查站点网络、登录状态或配置的网络代理")
	}
	return browserImport{}, safeError("未找到目标站点的有效登录凭证，请先在本机 Firefox 普通窗口登录同一用户；隐私窗口或容器标签请使用站点连接")
}
