package personalusage

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var uuidSession = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

// TitleResolver only reads title metadata on the server's local machine.
// Cache entries include misses and expire to pick up new sessions and renames.
type TitleResolver struct {
	mu         sync.Mutex
	entries    map[string]titleEntry
	CodexHome  string
	ClaudeHome string
}

type titleEntry struct {
	title   string
	expires time.Time
}

func NewTitleResolver() *TitleResolver {
	home, _ := os.UserHomeDir()
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
	if claudeHome == "" {
		claudeHome = filepath.Join(home, ".claude")
	}
	return &TitleResolver{entries: make(map[string]titleEntry), CodexHome: codexHome, ClaudeHome: claudeHome}
}

func cleanTitle(title string) string {
	title = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, title))
	runes := []rune(title)
	if len(runes) > 512 {
		title = string(runes[:512])
	}
	return title
}

func (r *TitleResolver) Resolve(ctx context.Context, ids []string) map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if r.entries == nil {
		r.entries = make(map[string]titleEntry)
	}
	if len(r.entries) > 2048 {
		clear(r.entries)
	}
	result, missing := make(map[string]string), make(map[string]bool)
	for _, id := range ids {
		// Local Codex/Claude sessions use UUIDs. Never interpolate arbitrary
		// request headers into filesystem paths or globs.
		if !uuidSession.MatchString(id) {
			continue
		}
		if cached, ok := r.entries[id]; ok && now.Before(cached.expires) {
			result[id] = cached.title
		} else {
			missing[id] = true
		}
	}
	if len(missing) == 0 {
		return result
	}
	titles := make(map[string]string)
	r.readCodexIndex(ctx, missing, titles)
	r.readCodexDB(ctx, missing, titles) // SQLite titles take precedence over the index.
	for id := range missing {
		if ctx.Err() != nil {
			break
		}
		if titles[id] == "" {
			titles[id] = r.readClaudeTitle(ctx, id)
		}
		title := cleanTitle(titles[id])
		r.entries[id] = titleEntry{title, now.Add(10 * time.Second)}
		result[id] = title
	}
	return result
}

func readJSONLines(ctx context.Context, path string, visit func([]byte)) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	// A corrupt/huge local file must not hold up the usage endpoint.
	scanner := bufio.NewScanner(io.LimitReader(file, 64<<20))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		visit(scanner.Bytes())
	}
}

func (r *TitleResolver) readCodexIndex(ctx context.Context, ids map[string]bool, titles map[string]string) {
	readJSONLines(ctx, filepath.Join(r.CodexHome, "session_index.jsonl"), func(line []byte) {
		var row struct {
			ID    string `json:"id"`
			Title string `json:"thread_name"`
		}
		if json.Unmarshal(line, &row) == nil && ids[row.ID] && row.Title != "" {
			titles[row.ID] = row.Title
		}
	})
}

func (r *TitleResolver) readCodexDB(ctx context.Context, ids map[string]bool, titles map[string]string) {
	path := filepath.Join(r.CodexHome, "state_5.sqlite")
	if _, err := os.Stat(path); err != nil {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	u.RawQuery = "mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(100)"
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return
	}
	defer db.Close()
	args, placeholders := make([]any, 0, len(ids)), make([]string, 0, len(ids))
	for id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, "?")
	}
	rows, err := db.QueryContext(ctx, "SELECT id, title FROM threads WHERE id IN ("+strings.Join(placeholders, ",")+")", args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var title sql.NullString
		if rows.Scan(&id, &title) == nil && title.Valid && title.String != "" {
			titles[id] = title.String
		}
	}
}

func (r *TitleResolver) readClaudeTitle(ctx context.Context, id string) string {
	paths, _ := filepath.Glob(filepath.Join(r.ClaudeHome, "projects", "*", id, "custom-title.json"))
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		var row struct {
			Title string `json:"customTitle"`
		}
		err = json.NewDecoder(io.LimitReader(file, 64<<10)).Decode(&row)
		file.Close()
		if err == nil && row.Title != "" {
			return row.Title
		}
	}
	logs, _ := filepath.Glob(filepath.Join(r.ClaudeHome, "projects", "*", id+".jsonl"))
	title := ""
	for _, path := range logs {
		readJSONLines(ctx, path, func(line []byte) {
			var row struct {
				Type      string `json:"type"`
				SessionID string `json:"sessionId"`
				Title     string `json:"customTitle"`
			}
			if json.Unmarshal(line, &row) == nil && row.Type == "custom-title" && row.SessionID == id && row.Title != "" {
				title = row.Title
			}
		})
	}
	return title
}
