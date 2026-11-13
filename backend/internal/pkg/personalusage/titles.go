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

	_ "modernc.org/sqlite"
)

var uuidSession = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

// TitleResolver only reads title metadata on the server's local machine.
// Each request reads current metadata so renames are visible on refresh.
type TitleResolver struct {
	CodexHome  string
	ClaudeHome string
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
	return &TitleResolver{CodexHome: codexHome, ClaudeHome: claudeHome}
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

type SessionInfo struct {
	Name           string
	Kind           string
	ParentThreadID string
}

func (r *TitleResolver) Resolve(ctx context.Context, ids []string) map[string]string {
	result := map[string]string{}
	for id, info := range r.ResolveDetails(ctx, ids) {
		result[id] = info.Name
	}
	return result
}

func (r *TitleResolver) ResolveDetails(ctx context.Context, ids []string) map[string]SessionInfo {
	result, missing := make(map[string]SessionInfo), make(map[string]bool)
	for _, id := range ids {
		// Local Codex/Claude sessions use UUIDs. Never interpolate arbitrary
		// request headers into filesystem paths or globs.
		if !uuidSession.MatchString(id) {
			continue
		}
		missing[id] = true
	}
	if len(missing) == 0 {
		return result
	}
	titles := make(map[string]string)
	r.readCodexIndex(ctx, missing, titles)
	details := r.readCodexDB(ctx, missing, titles) // SQLite names take precedence over the index.
	for id := range missing {
		if ctx.Err() != nil {
			break
		}
		if titles[id] == "" {
			titles[id] = r.readClaudeTitle(ctx, id)
		}
		title := cleanTitle(titles[id])
		info := details[id]
		info.Name = title
		result[id] = info
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

func (r *TitleResolver) readCodexDB(ctx context.Context, ids map[string]bool, titles map[string]string) map[string]SessionInfo {
	details := map[string]SessionInfo{}
	path := filepath.Join(r.CodexHome, "state_5.sqlite")
	if _, err := os.Stat(path); err != nil {
		return details
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return details
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	u.RawQuery = "mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(100)"
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return details
	}
	defer db.Close()
	// Codex schema evolves independently. Only select known metadata fields
	// present in this database; never read first_user_message or rollout text.
	columns := map[string]bool{}
	schema, err := db.QueryContext(ctx, "PRAGMA table_info(threads)")
	if err != nil {
		return details
	}
	for schema.Next() {
		var cid, notnull, pk int
		var name, kind string
		var def any
		if schema.Scan(&cid, &name, &kind, &notnull, &def, &pk) == nil {
			columns[name] = true
		}
	}
	schema.Close()
	column := func(name string) string {
		if columns[name] {
			return "COALESCE(" + name + ", '')"
		}
		return "''"
	}
	args, placeholders := make([]any, 0, len(ids)), make([]string, 0, len(ids))
	for id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, "?")
	}
	query := "SELECT id, " + column("name") + ", " + column("title") + ", " + column("source") + ", " + column("agent_nickname") + ", " + column("agent_path") + " FROM threads WHERE id IN (" + strings.Join(placeholders, ",") + ")"
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return details
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, title, source, nickname, agentPath string
		if rows.Scan(&id, &name, &title, &source, &nickname, &agentPath) != nil {
			continue
		}
		info := SessionInfo{Kind: "main"}
		var metadata struct {
			Subagent *struct {
				Other       string `json:"other"`
				ThreadSpawn *struct {
					Parent   string `json:"parent_thread_id"`
					Nickname string `json:"agent_nickname"`
					Path     string `json:"agent_path"`
				} `json:"thread_spawn"`
			} `json:"subagent"`
		}
		_ = json.Unmarshal([]byte(source), &metadata)
		if metadata.Subagent != nil {
			info.Kind = "subagent"
			if strings.EqualFold(metadata.Subagent.Other, "guardian") {
				info.Kind = "guardian"
			}
			if spawn := metadata.Subagent.ThreadSpawn; spawn != nil {
				if uuidSession.MatchString(spawn.Parent) {
					info.ParentThreadID = spawn.Parent
				}
				if nickname == "" {
					nickname = spawn.Nickname
				}
				if agentPath == "" {
					agentPath = spawn.Path
				}
			}
			// Subagent title may contain the entire delegated prompt. Prefer
			// an explicit rename, nickname or task path instead of showing it.
			title = nickname
			if task := strings.TrimSpace(agentPath); task != "" {
				task = filepath.Base(filepath.FromSlash(task))
				if task != "." && task != "/" {
					if title != "" {
						title += " · "
					}
					title += task
				}
			}
			if title == "" {
				title = info.Kind
			}
		}
		if strings.TrimSpace(name) != "" {
			title = name
		}
		if cleanTitle(title) != "" {
			titles[id] = cleanTitle(title)
		}
		details[id] = info
	}
	return details
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
