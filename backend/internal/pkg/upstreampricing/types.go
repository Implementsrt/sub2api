// Package upstreampricing is a fork extension. It never changes local billing.
package upstreampricing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Config struct {
	Provider        string  `json:"provider"`
	BaseURL         string  `json:"base_url"`
	Token           string  `json:"token,omitempty"`
	BrowserImportID string  `json:"browser_import_id,omitempty"`
	UserID          string  `json:"user_id,omitempty"`
	Group           string  `json:"group"`
	GroupID         string  `json:"group_id,omitempty"`
	Automatic       bool    `json:"automatic"`
	IntervalMinutes int     `json:"interval_minutes"`
	Overrides       []Price `json:"overrides,omitempty"`
}

type Group struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	Multiplier string `json:"multiplier"`
}

// Prices are USD per million tokens, before the upstream group multiplier.
// Nil means unknown; explicit zero is a valid price.
type Price struct {
	Model         string  `json:"model"`
	Mode          string  `json:"mode"`
	Input         *string `json:"input"`
	Output        *string `json:"output"`
	CacheRead     *string `json:"cache_read"`
	CacheWrite    *string `json:"cache_write"`
	CacheWrite1h  *string `json:"cache_write_1h,omitempty"`
	CacheWrite30m *string `json:"cache_write_30m,omitempty"`
	CacheWrite12h *string `json:"cache_write_12h,omitempty"`
	Request       *string `json:"request"`
	Reason        string  `json:"reason,omitempty"`
	Manual        bool    `json:"manual,omitempty"`
}

type Catalog struct {
	Group         Group   `json:"group"`
	Models        []Price `json:"models"`
	RemoteVersion string  `json:"remote_version,omitempty"`
	Supported     int     `json:"supported"`
	Unsupported   int     `json:"unsupported"`
	Fingerprint   string  `json:"fingerprint"`
}

type Usage struct {
	AccountID          int64     `json:"account_id"`
	AccountFingerprint string    `json:"account_fingerprint"`
	Model              string    `json:"model"`
	PricingAt          time.Time `json:"pricing_at"`
	Input              int       `json:"input"`
	Output             int       `json:"output"`
	CacheRead          int       `json:"cache_read"`
	CacheWrite         int       `json:"cache_write"`
	CacheWrite5m       int       `json:"cache_write_5m"`
	CacheWrite1h       int       `json:"cache_write_1h"`
	CacheWrite30m      int       `json:"cache_write_30m"`
	CacheWrite12h      int       `json:"cache_write_12h"`
	ServiceTier        string    `json:"service_tier,omitempty"`
	ExtraUnits         bool      `json:"extra_units"`
}

type Estimate struct {
	Status     string    `json:"status"`
	Amount     *string   `json:"amount"`
	Reason     string    `json:"reason,omitempty"`
	VersionID  int64     `json:"version_id,omitempty"`
	Source     string    `json:"source,omitempty"`
	Group      string    `json:"group,omitempty"`
	Multiplier string    `json:"multiplier,omitempty"`
	Model      string    `json:"model,omitempty"`
	PricingAt  time.Time `json:"pricing_at,omitempty"`
	SyncedAt   time.Time `json:"synced_at,omitempty"`
	Price      *Price    `json:"price,omitempty"`
	Usage      *Usage    `json:"usage,omitempty"`
	Stale      bool      `json:"stale"`
}

var decimalPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,12})(?:\.[0-9]{1,18})?$`)
var exponentPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?[eE][+-]?[0-9]{1,2}$`)

type PublicError struct{ message string }

func (e *PublicError) Error() string { return e.message }
func safeError(message string) error { return &PublicError{message: message} }
func PublicMessage(err error) (string, int) {
	if errors.Is(err, ErrConflict) || errors.Is(err, ErrBusy) {
		return err.Error(), 409
	}
	var safe *PublicError
	if errors.As(err, &safe) {
		return safe.message, 400
	}
	return "上游定价操作暂不可用，请检查服务状态；原有计费和价格未变更", 503
}

func decimal(s string) (*big.Rat, error) {
	if !decimalPattern.MatchString(s) {
		return nil, safeError("单价或倍率必须是非负十进制数（最多 18 位小数）")
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok || r.Cmp(big.NewRat(1000000000000, 1)) > 0 {
		return nil, safeError("单价或倍率超出范围")
	}
	return r, nil
}

func number(v any) (*string, error) {
	if v == nil {
		return nil, nil
	}
	var s string
	switch n := v.(type) {
	case json.Number:
		s = string(n)
	case string:
		s = n
	default:
		return nil, safeError("价格不是明确的数值")
	}
	// JSON scientific notation is normalized with exact rational arithmetic.
	if len(s) > 64 {
		return nil, safeError("价格数值过长")
	}
	if strings.ContainsAny(s, "eE") {
		if !exponentPattern.MatchString(s) {
			return nil, safeError("价格指数超出支持范围")
		}
		r, ok := new(big.Rat).SetString(s)
		if !ok {
			return nil, safeError("价格无效")
		}
		s = r.FloatString(18)
		rounded, _ := new(big.Rat).SetString(s)
		if rounded == nil || rounded.Cmp(r) != 0 {
			return nil, safeError("价格精度超过 18 位小数")
		}
	}
	r, err := decimal(s)
	if err != nil {
		return nil, err
	}
	result := format(r, 18)
	return &result, nil
}

func format(r *big.Rat, places int) string {
	s := r.FloatString(places)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

func scaled(v any, factor int64) (*string, error) {
	p, err := number(v)
	if err != nil || p == nil {
		return p, err
	}
	r, _ := decimal(*p)
	r.Mul(r, big.NewRat(factor, 1))
	s := format(r, 18)
	if _, err = decimal(s); err != nil {
		return nil, err
	}
	return &s, nil
}

func ValidatePrice(p Price) error {
	if strings.TrimSpace(p.Model) != p.Model || p.Model == "" || len(p.Model) > 300 || p.Model == "*" {
		return safeError("模型名称无效")
	}
	if p.Mode != "token" && p.Mode != "per_request" && p.Mode != "unavailable" {
		return safeError("不支持的计价模式")
	}
	for _, v := range []*string{p.Input, p.Output, p.CacheRead, p.CacheWrite, p.CacheWrite1h, p.CacheWrite30m, p.CacheWrite12h, p.Request} {
		if v != nil {
			if _, err := decimal(*v); err != nil {
				return err
			}
		}
	}
	if p.Mode == "token" && (p.Input == nil || p.Output == nil) {
		return safeError("缺少输入或输出单价")
	}
	if p.Mode == "per_request" && p.Request == nil {
		return safeError("缺少按次价格")
	}
	return nil
}

func finalize(c *Catalog, overrides []Price) error {
	seen := map[string]bool{}
	for _, p := range c.Models {
		if seen[p.Model] {
			return safeError("站点返回重复模型")
		}
		seen[p.Model] = true
	}
	manual := map[string]Price{}
	for _, p := range overrides {
		if p.Mode == "unavailable" {
			return safeError("人工覆盖仅支持明确单价")
		}
		if err := ValidatePrice(p); err != nil {
			return err
		}
		if _, ok := manual[p.Model]; ok {
			return safeError("人工覆盖模型重复")
		}
		p.Manual = true
		manual[p.Model] = p
	}
	for i, p := range c.Models {
		if m, ok := manual[p.Model]; ok {
			c.Models[i] = m
			delete(manual, p.Model)
		}
	}
	for _, p := range manual {
		c.Models = append(c.Models, p)
	}
	sort.Slice(c.Models, func(i, j int) bool { return c.Models[i].Model < c.Models[j].Model })
	c.Supported = 0
	c.Unsupported = 0
	for _, p := range c.Models {
		if err := ValidatePrice(p); err != nil {
			return err
		}
		if p.Mode == "unavailable" {
			c.Unsupported++
		} else {
			c.Supported++
		}
	}
	c.Fingerprint = ""
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	c.Fingerprint = hex.EncodeToString(h[:])
	return nil
}

// Calculate uses disjoint input buckets and applies the upstream multiplier once.
func Calculate(c Catalog, u Usage) Estimate {
	e := Estimate{Status: "unavailable", Model: u.Model, Group: c.Group.Name, Multiplier: c.Group.Multiplier, PricingAt: u.PricingAt}
	if u.ExtraUnits {
		e.Reason = "请求包含尚未适配的图片、音频、工具或缓存存储计费维度"
		return e
	}
	if u.ServiceTier != "" && u.ServiceTier != "default" && u.ServiceTier != "standard" {
		e.Reason = "尚未适配此服务层级的上游定价"
		return e
	}
	var p *Price
	for i := range c.Models {
		if c.Models[i].Model == u.Model {
			p = &c.Models[i]
			break
		}
	}
	if p == nil {
		e.Reason = "模型不在该版本的分组价格目录中"
		return e
	}
	e.Price = p
	if p.Mode == "unavailable" {
		e.Reason = p.Reason
		return e
	}
	if err := ValidatePrice(*p); err != nil {
		e.Reason = "价格快照无效"
		return e
	}
	ratio, err := decimal(c.Group.Multiplier)
	if err != nil {
		e.Reason = "上游倍率无效"
		return e
	}
	total := new(big.Rat)
	if p.Mode == "per_request" {
		total, _ = decimal(*p.Request)
	} else {
		counts := []int{u.Input, u.Output, u.CacheRead, u.CacheWrite, u.CacheWrite5m, u.CacheWrite1h, u.CacheWrite30m, u.CacheWrite12h}
		for _, n := range counts {
			if n < 0 {
				e.Reason = "用量为负值"
				return e
			}
		}
		if u.Input+u.Output+u.CacheRead+u.CacheWrite == 0 {
			e.Reason = "缺少上游 Token 用量"
			return e
		}
		known := u.CacheWrite5m + u.CacheWrite1h + u.CacheWrite30m + u.CacheWrite12h
		if known > u.CacheWrite {
			e.Reason = "缓存写入用量明细不一致"
			return e
		}
		parts := []struct {
			n int
			p *string
		}{{u.Input, p.Input}, {u.Output, p.Output}, {u.CacheRead, p.CacheRead}, {u.CacheWrite - known + u.CacheWrite5m, p.CacheWrite}, {u.CacheWrite1h, p.CacheWrite1h}, {u.CacheWrite30m, p.CacheWrite30m}, {u.CacheWrite12h, p.CacheWrite12h}}
		for _, part := range parts {
			if part.n == 0 {
				continue
			}
			if part.p == nil {
				e.Reason = "有用量的计费项缺少单价（包括缓存生命周期单价）"
				return e
			}
			r, _ := decimal(*part.p)
			r.Mul(r, big.NewRat(int64(part.n), 1000000))
			total.Add(total, r)
		}
	}
	total.Mul(total, ratio)
	// At most 18 decimal places each for price and multiplier, plus 6 for
	// million-token units. Preserve exact tiny charges instead of rounding to free.
	s := format(total, 42)
	e.Status = "estimated"
	e.Amount = &s
	return e
}
