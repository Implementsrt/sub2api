package upstreampricing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrConflict = safeError("配置或账号已变化，请重新预览；原价格仍然保留")
var ErrBusy = safeError("该账号正在同步，请稍后重试")

type Store struct{ DB *sql.DB }
type Cipher interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}

type Source struct {
	AccountID          int64      `json:"account_id"`
	Revision           int64      `json:"revision"`
	Config             Config     `json:"config"`
	HasCredential      bool       `json:"has_credential"`
	ActiveVersion      *int64     `json:"active_version"`
	LastSuccess        *time.Time `json:"last_success"`
	LastAttempt        *time.Time `json:"last_attempt"`
	LastError          string     `json:"last_error"`
	NextSync           *time.Time `json:"next_sync"`
	Credential         string     `json:"-"`
	AccountFingerprint string     `json:"-"`
}

type Preview struct {
	ID      string  `json:"id"`
	Catalog Catalog `json:"catalog"`
	Added   int     `json:"added"`
	Changed int     `json:"changed"`
	Removed int     `json:"removed"`
}
type Version struct {
	AccountFingerprint string     `json:"-"`
	ID                 int64      `json:"id"`
	EffectiveFrom      time.Time  `json:"effective_from"`
	EffectiveUntil     *time.Time `json:"effective_until"`
	SyncedAt           time.Time  `json:"synced_at"`
	Catalog            Catalog    `json:"catalog"`
	Config             Config     `json:"config"`
}
type Run struct {
	ID        int64     `json:"id"`
	Trigger   string    `json:"trigger"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	VersionID *int64    `json:"version_id"`
	CreatedAt time.Time `json:"created_at"`
}

func randomToken() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure random unavailable")
	}
	return hex.EncodeToString(b[:])
}
func encoded(v any) []byte         { b, _ := json.Marshal(v); return b }
func publicConfig(c Config) Config { c.Token = ""; return c }

// Use the same selected-account snapshot at dispatch and when validating a source.
// Never persist or return the actual inference credentials in pricing metadata.
func AccountFingerprint(kind, platform string, credentials map[string]any) string {
	v := map[string]any{"type": kind, "platform": platform}
	for _, key := range []string{"api_key", "base_url", "api_base_urls", "api_protocol", "account_mode"} {
		v[key] = credentials[key]
	}
	h := sha256.Sum256(encoded(v))
	return hex.EncodeToString(h[:])
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func accountFingerprint(ctx context.Context, q queryRower, id int64, lock bool) (string, error) {
	query := `SELECT type, platform, credentials FROM accounts WHERE id=$1 AND deleted_at IS NULL`
	if lock {
		query += " FOR SHARE"
	}
	var kind, platform string
	var b []byte
	if err := q.QueryRowContext(ctx, query, id).Scan(&kind, &platform, &b); err != nil {
		return "", safeError("上游账号不存在或已删除")
	}
	if kind != "apikey" {
		return "", safeError("上游定价同步当前仅支持 API Key 账号")
	}
	var creds map[string]any
	if json.Unmarshal(b, &creds) != nil {
		return "", safeError("无法验证上游账号")
	}
	return AccountFingerprint(kind, platform, creds), nil
}
func (s *Store) AccountFingerprint(ctx context.Context, id int64) (string, error) {
	return accountFingerprint(ctx, s.DB, id, false)
}

func (s *Store) Source(ctx context.Context, id int64) (*Source, error) {
	v := &Source{AccountID: id}
	var b []byte
	err := s.DB.QueryRowContext(ctx, `SELECT revision,config,credential,account_fingerprint,active_version,last_success,last_attempt,last_error,next_sync FROM fork_upstream_pricing_sources WHERE account_id=$1`, id).Scan(&v.Revision, &b, &v.Credential, &v.AccountFingerprint, &v.ActiveVersion, &v.LastSuccess, &v.LastAttempt, &v.LastError, &v.NextSync)
	if errors.Is(err, sql.ErrNoRows) {
		v.Config = Config{Provider: "newapi", IntervalMinutes: 60}
		return v, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, &v.Config) != nil {
		return nil, safeError("定价配置损坏")
	}
	v.HasCredential = v.Credential != ""
	v.Config.Token = ""
	return v, nil
}

func (s *Store) SavePreview(ctx context.Context, id, revision int64, c Config, credential, fp string, cat Catalog) (Preview, error) {
	p := Preview{ID: randomToken(), Catalog: cat}
	old := map[string]Price{}
	multiplierChanged := false
	var b []byte
	err := s.DB.QueryRowContext(ctx, `SELECT v.catalog FROM fork_upstream_pricing_versions v JOIN fork_upstream_pricing_sources s ON s.active_version=v.id WHERE s.account_id=$1`, id).Scan(&b)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	if len(b) > 0 {
		var v Catalog
		if json.Unmarshal(b, &v) != nil {
			return p, safeError("旧价格快照损坏")
		}
		multiplierChanged = v.Group.Multiplier != cat.Group.Multiplier
		for _, m := range v.Models {
			old[m.Model] = m
		}
	}
	for _, m := range cat.Models {
		if prior, ok := old[m.Model]; !ok {
			p.Added++
		} else {
			if string(encoded(prior)) != string(encoded(m)) || (multiplierChanged && m.Mode != "unavailable") {
				p.Changed++
			}
			delete(old, m.Model)
		}
	}
	p.Removed = len(old)
	_, err = s.DB.ExecContext(ctx, `INSERT INTO fork_upstream_pricing_previews(account_id,token,revision,config,credential,account_fingerprint,catalog,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,clock_timestamp()+interval '10 minutes') ON CONFLICT(account_id) DO UPDATE SET token=EXCLUDED.token,revision=EXCLUDED.revision,config=EXCLUDED.config,credential=EXCLUDED.credential,account_fingerprint=EXCLUDED.account_fingerprint,catalog=EXCLUDED.catalog,expires_at=EXCLUDED.expires_at`, id, p.ID, revision, string(encoded(publicConfig(c))), credential, fp, string(encoded(cat)))
	return p, err
}

// Claim is persisted, so manual and scheduled syncs on different server instances
// cannot race. A configuration revision change also invalidates this lease.
func (s *Store) Claim(ctx context.Context, id int64) (*Source, string, error) {
	token := randomToken()
	res, err := s.DB.ExecContext(ctx, `UPDATE fork_upstream_pricing_sources SET lease_token=$2,lease_until=clock_timestamp()+interval '2 minutes',last_attempt=clock_timestamp() WHERE account_id=$1 AND (lease_until IS NULL OR lease_until<clock_timestamp())`, id, token)
	if err != nil {
		return nil, "", err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, "", ErrBusy
	}
	src, err := s.Source(ctx, id)
	return src, token, err
}

// ApplyPreview rechecks the account identity and source revision in one transaction.
func (s *Store) ApplyPreview(ctx context.Context, id int64, token string) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Serialize even the first configuration for this account, before a source row exists.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, -id); err != nil {
		return 0, err
	}
	var revision int64
	var cfgb, catb []byte
	var credential, fp string
	err = tx.QueryRowContext(ctx, `SELECT revision,config,credential,account_fingerprint,catalog FROM fork_upstream_pricing_previews WHERE account_id=$1 AND token=$2 AND expires_at>clock_timestamp() FOR UPDATE`, id, token).Scan(&revision, &cfgb, &credential, &fp, &catb)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrConflict
	}
	if err != nil {
		return 0, err
	}
	actual, err := accountFingerprint(ctx, tx, id, true)
	if err != nil {
		return 0, err
	}
	if actual != fp {
		return 0, ErrConflict
	}
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM fork_upstream_pricing_sources WHERE account_id=$1 FOR UPDATE`, id).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if current != revision {
		return 0, ErrConflict
	}
	var c Config
	var cat Catalog
	if json.Unmarshal(cfgb, &c) != nil || json.Unmarshal(catb, &cat) != nil {
		return 0, safeError("预览数据损坏")
	}
	version, err := publish(ctx, tx, id, c, credential, fp, cat, current, "manual_apply")
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM fork_upstream_pricing_previews WHERE account_id=$1`, id); err != nil {
		return 0, err
	}
	return version, tx.Commit()
}

func publish(ctx context.Context, tx *sql.Tx, id int64, c Config, credential, fp string, cat Catalog, revision int64, trigger string) (int64, error) {
	// One database timestamp is used for both sides of the version boundary.
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return 0, err
	}
	var version int64
	var oldHash, oldFP string
	var oldConfig []byte
	err := tx.QueryRowContext(ctx, `SELECT v.id,v.fingerprint,v.account_fingerprint,v.config FROM fork_upstream_pricing_versions v JOIN fork_upstream_pricing_sources s ON s.active_version=v.id WHERE s.account_id=$1`, id).Scan(&version, &oldHash, &oldFP, &oldConfig)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	// Scheduling changes do not change price identity, but connection/group changes do.
	var previous Config
	_ = json.Unmarshal(oldConfig, &previous)
	identitySame := previous.Provider == c.Provider && previous.BaseURL == c.BaseURL && previous.UserID == c.UserID && previous.Group == c.Group && previous.GroupID == c.GroupID
	if version == 0 || oldHash != cat.Fingerprint || oldFP != fp || !identitySame {
		if _, err = tx.ExecContext(ctx, `UPDATE fork_upstream_pricing_versions SET effective_until=$2 WHERE account_id=$1 AND effective_until IS NULL`, id, now); err != nil {
			return 0, err
		}
		err = tx.QueryRowContext(ctx, `INSERT INTO fork_upstream_pricing_versions(account_id,config,account_fingerprint,catalog,fingerprint,effective_from,synced_at) VALUES($1,$2,$3,$4,$5,$6,$6) RETURNING id`, id, string(encoded(publicConfig(c))), fp, string(encoded(cat)), cat.Fingerprint, now).Scan(&version)
		if err != nil {
			return 0, err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE fork_upstream_pricing_versions SET synced_at=$2 WHERE id=$1`, version, now); err != nil {
			return 0, err
		}
	}
	var next *time.Time
	if c.Automatic {
		t := now.Add(time.Duration(c.IntervalMinutes) * time.Minute)
		next = &t
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO fork_upstream_pricing_sources(account_id,revision,config,credential,account_fingerprint,active_version,last_success,last_attempt,next_sync) VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8) ON CONFLICT(account_id) DO UPDATE SET revision=EXCLUDED.revision,config=EXCLUDED.config,credential=EXCLUDED.credential,account_fingerprint=EXCLUDED.account_fingerprint,active_version=EXCLUDED.active_version,last_success=EXCLUDED.last_success,last_attempt=EXCLUDED.last_attempt,next_sync=EXCLUDED.next_sync,last_error='',lease_token=NULL,lease_until=NULL,updated_at=EXCLUDED.last_success`, id, revision+1, string(encoded(publicConfig(c))), credential, fp, version, now, next)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO fork_upstream_pricing_runs(account_id,trigger,status,version_id) VALUES($1,$2,'success',$3)`, id, trigger, version)
	return version, err
}

func (s *Store) PublishSync(ctx context.Context, src *Source, token string, cat Catalog, trigger string) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, -src.AccountID); err != nil {
		return 0, err
	}
	var rev int64
	var lease string
	err = tx.QueryRowContext(ctx, `SELECT revision,COALESCE(lease_token,'') FROM fork_upstream_pricing_sources WHERE account_id=$1 FOR UPDATE`, src.AccountID).Scan(&rev, &lease)
	if err != nil {
		return 0, err
	}
	if rev != src.Revision || lease != token {
		return 0, ErrConflict
	}
	fp, err := accountFingerprint(ctx, tx, src.AccountID, true)
	if err != nil {
		return 0, err
	}
	if fp != src.AccountFingerprint {
		return 0, ErrConflict
	}
	version, err := publish(ctx, tx, src.AccountID, src.Config, src.Credential, fp, cat, rev, trigger)
	if err != nil {
		return 0, err
	}
	return version, tx.Commit()
}

func (s *Store) Fail(ctx context.Context, src *Source, token, trigger, message string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE fork_upstream_pricing_sources SET lease_token=NULL,lease_until=NULL,last_error=$4,next_sync=CASE WHEN (config->>'automatic')::boolean THEN clock_timestamp()+make_interval(mins => (config->>'interval_minutes')::integer) ELSE NULL END WHERE account_id=$1 AND revision=$2 AND lease_token=$3`, src.AccountID, src.Revision, token, message)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO fork_upstream_pricing_runs(account_id,trigger,status,message) VALUES($1,$2,'failed',$3)`, src.AccountID, trigger, message); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Schedule(ctx context.Context, id int64, automatic bool, minutes int) error {
	c := Config{Provider: "newapi", BaseURL: "https://example.com", IntervalMinutes: minutes}
	if err := ValidateConfig(&c); err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE fork_upstream_pricing_sources SET config=jsonb_set(jsonb_set(config,'{automatic}',to_jsonb($2::boolean)),'{interval_minutes}',to_jsonb($3::integer)),revision=revision+1,lease_token=NULL,lease_until=NULL,next_sync=CASE WHEN $2 THEN clock_timestamp()+make_interval(mins => $3) ELSE NULL END WHERE account_id=$1`, id, automatic, minutes)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return safeError("请先预览并应用价格")
	}
	return nil
}

func (s *Store) Due(ctx context.Context) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT s.account_id FROM fork_upstream_pricing_sources s JOIN accounts a ON a.id=s.account_id AND a.deleted_at IS NULL WHERE (s.config->>'automatic')::boolean AND s.next_sync<=clock_timestamp() AND (s.lease_until IS NULL OR s.lease_until<clock_timestamp()) ORDER BY s.next_sync LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) History(ctx context.Context, id int64) ([]Run, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,trigger,status,message,version_id,created_at FROM fork_upstream_pricing_runs WHERE account_id=$1 ORDER BY id DESC LIMIT 20`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var v Run
		if err = rows.Scan(&v.ID, &v.Trigger, &v.Status, &v.Message, &v.VersionID, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) Versions(ctx context.Context, id int64) ([]Version, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,effective_from,effective_until,synced_at,catalog,config,account_fingerprint FROM fork_upstream_pricing_versions WHERE account_id=$1 ORDER BY id DESC LIMIT 20`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		var cat, cfg []byte
		if err = rows.Scan(&v.ID, &v.EffectiveFrom, &v.EffectiveUntil, &v.SyncedAt, &cat, &cfg, &v.AccountFingerprint); err != nil {
			return nil, err
		}
		if json.Unmarshal(cat, &v.Catalog) != nil || json.Unmarshal(cfg, &v.Config) != nil {
			return nil, safeError("价格版本损坏")
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type UsageJob struct {
	ID        int64
	RequestID string
	APIKeyID  int64
	UserID    int64
	Usage     Usage
}

func (s *Store) Enqueue(ctx context.Context, j UsageJob) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO fork_upstream_pricing_usage(usage_id,payload,version_id)
	SELECT u.id,$6::jsonb,(SELECT v.id FROM fork_upstream_pricing_versions v WHERE v.account_id=$5 AND v.account_fingerprint=$7 AND v.effective_from<=$8 AND (v.effective_until IS NULL OR v.effective_until>$8) ORDER BY v.effective_from DESC LIMIT 1)
	FROM usage_logs u WHERE u.user_id=$4 AND u.account_id=$5 AND (($1::bigint>0 AND u.id=$1) OR ($1=0 AND $2<>'' AND u.request_id=$2 AND u.api_key_id=$3))
	ON CONFLICT(usage_id) DO NOTHING`, j.ID, j.RequestID, j.APIKeyID, j.UserID, j.Usage.AccountID, string(encoded(j.Usage)), j.Usage.AccountFingerprint, j.Usage.PricingAt)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists bool
		err = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fork_upstream_pricing_usage e JOIN usage_logs u ON u.id=e.usage_id WHERE u.user_id=$4 AND u.account_id=$5 AND (($1::bigint>0 AND u.id=$1) OR ($1=0 AND $2<>'' AND u.request_id=$2 AND u.api_key_id=$3)))`, j.ID, j.RequestID, j.APIKeyID, j.UserID, j.Usage.AccountID).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return safeError("用量尚未落库")
		}
	}
	return nil
}

func (s *Store) Process(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT e.usage_id,e.payload,v.id,v.catalog,v.config,v.synced_at FROM fork_upstream_pricing_usage e LEFT JOIN fork_upstream_pricing_versions v ON v.id=e.version_id WHERE e.estimate IS NULL ORDER BY e.usage_id LIMIT 100`)
	if err != nil {
		return err
	}
	type pending struct {
		id      int64
		u       Usage
		version sql.NullInt64
		cat     Catalog
		cfg     Config
		synced  sql.NullTime
	}
	jobs := []pending{}
	for rows.Next() {
		var j pending
		var payload, cat, cfg []byte
		if err = rows.Scan(&j.id, &payload, &j.version, &cat, &cfg, &j.synced); err != nil {
			rows.Close()
			return err
		}
		if json.Unmarshal(payload, &j.u) != nil {
			rows.Close()
			return safeError("用量估算任务损坏")
		}
		if j.version.Valid && (json.Unmarshal(cat, &j.cat) != nil || json.Unmarshal(cfg, &j.cfg) != nil) {
			rows.Close()
			return safeError("价格快照损坏")
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		e := Estimate{Status: "not_configured", Reason: "请求开始时没有匹配账号身份的有效价格版本", Model: j.u.Model, PricingAt: j.u.PricingAt}
		if j.u.PricingAt.IsZero() {
			e.Status = "unavailable"
			e.Reason = "此请求路径未记录可靠的开始计价时间"
		} else if j.version.Valid {
			e = Calculate(j.cat, j.u)
			e.VersionID = j.version.Int64
			e.Source = j.cfg.BaseURL
			e.SyncedAt = j.synced.Time
			e.Stale = j.u.PricingAt.Sub(j.synced.Time) > time.Duration(j.cfg.IntervalMinutes*2)*time.Minute
			copy := j.u
			copy.AccountFingerprint = ""
			e.Usage = &copy
		}
		if _, err = s.DB.ExecContext(ctx, `UPDATE fork_upstream_pricing_usage SET estimate=$2::jsonb WHERE usage_id=$1 AND estimate IS NULL`, j.id, string(encoded(e))); err != nil {
			return err
		}
	}
	return nil
}

// UserID zero is reserved for the full administrator route. Personal calls always
// pass the authenticated user's ID; selected usage IDs alone are not authorization.
func (s *Store) Estimates(ctx context.Context, userID int64, ids []int64) (map[int64]*Estimate, error) {
	out := map[int64]*Estimate{}
	if len(ids) == 0 {
		return out, nil
	}
	if len(ids) > 1000 {
		return nil, safeError("用量数量超出限制")
	}
	args := []any{userID}
	slots := []string{}
	for _, id := range ids {
		args = append(args, id)
		slots = append(slots, fmt.Sprintf("$%d", len(args)))
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id,e.usage_id,e.estimate FROM usage_logs u LEFT JOIN fork_upstream_pricing_usage e ON e.usage_id=u.id WHERE ($1::bigint=0 OR u.user_id=$1) AND u.id IN (`+strings.Join(slots, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var evidenceID sql.NullInt64
		var b []byte
		if err = rows.Scan(&id, &evidenceID, &b); err != nil {
			return nil, err
		}
		e := &Estimate{Status: "unavailable", Reason: "此记录尚无上游估算；历史记录不会按当前价格自动回算"}
		if evidenceID.Valid {
			e.Status = "pending"
			e.Reason = "用量已记录，等待后台估算"
		}
		if len(b) > 0 {
			if json.Unmarshal(b, e) != nil {
				return nil, safeError("估算记录损坏")
			}
		}
		out[id] = e
	}
	return out, rows.Err()
}
