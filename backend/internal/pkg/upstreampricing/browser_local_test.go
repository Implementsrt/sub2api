package upstreampricing

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func localBrowserTestJWT(user string, expiry time.Time) string {
	payload, _ := json.Marshal(map[string]any{"user_id": user, "exp": expiry.Unix()})
	return "synthetic." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func TestLocalBrowserRequestBoundary(t *testing.T) {
	require.NoError(t, ValidateLocalBrowserRequest("http://127.0.0.1:8080", "127.0.0.1:8080", "http", "127.0.0.1:1234"))
	for _, input := range [][4]string{
		{"http://127.0.0.1:8080", "127.0.0.1:8080", "http", "192.0.2.1:1234"},
		{"http://localhost:8080", "127.0.0.1:8080", "http", "127.0.0.1:1234"},
		{"https://remote.example", "remote.example", "https", "127.0.0.1:1234"},
		{"", "localhost:8080", "http", "127.0.0.1:1234"},
	} {
		require.Error(t, ValidateLocalBrowserRequest(input[0], input[1], input[2], input[3]))
	}
}

func TestLocalBrowserJWTSelection(t *testing.T) {
	now := time.Now()
	token := localBrowserTestJWT("42", now.Add(time.Hour))
	id, _, ok := browserJWTIdentity(token, now, true)
	require.True(t, ok)
	require.Equal(t, "42", id)
	expired := localBrowserTestJWT("42", now.Add(-time.Hour))
	_, _, ok = browserJWTIdentity(expired, now, true)
	require.False(t, ok)
	_, _, ok = browserJWTIdentity(expired, now, false)
	require.True(t, ok, "expired saved credentials can still establish the expected identity")
	for _, bad := range []string{"invalid", token + "\n", strings.Repeat("a", 17000)} {
		_, _, ok = browserJWTIdentity(bad, now, true)
		require.False(t, ok)
	}
}

func TestLocalFirefoxExactOriginAndProfiles(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "profiles.ini"), []byte("[Profile0]\nIsRelative=1\nPath=Profiles/main\n[Profile1]\nPath=../escape\n"), 0600))
	create := func(origin, token string) {
		path := filepath.Join(root, "Profiles", "main", "storage", "default", origin, "ls", "data.sqlite")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		db, err := sql.Open("sqlite", path)
		require.NoError(t, err)
		_, err = db.Exec(`CREATE TABLE data(key text,conversion_type integer,compression_type integer,value blob); INSERT INTO data VALUES('auth_token',1,0,?)`, []byte(token))
		require.NoError(t, err)
		require.NoError(t, db.Close())
	}
	create("https+++station.example", "synthetic-selected-token")
	create("https+++other.example", "synthetic-other-token")
	create("https+++station.example^userContextId=1", "synthetic-container-token")
	create("https+++station.example+8443", "synthetic-port-token")
	tokens, err := readFirefoxTokens(context.Background(), "https://station.example/console", root)
	require.NoError(t, err)
	require.Equal(t, []string{"synthetic-selected-token"}, tokens)
	tokens, err = readFirefoxTokens(context.Background(), "https://station.example:8443", root)
	require.NoError(t, err)
	require.Equal(t, []string{"synthetic-port-token"}, tokens)
	_, err = readFirefoxTokens(context.Background(), "http://station.example", root)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "synthetic")
}

func TestLocalFirefoxLiveRead(t *testing.T) {
	origin := os.Getenv("FORK_FIREFOX_AUDIT_ORIGIN")
	if origin == "" {
		t.Skip("opt-in local profile verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	tokens, err := ReadLocalFirefoxTokens(ctx, origin)
	if err != nil {
		t.Fatal(err)
	}
	valid := 0
	for _, token := range tokens {
		if _, _, ok := browserJWTIdentity(token, time.Now(), true); ok {
			valid++
		}
	}
	t.Logf("local sessions found=%d, unexpired JWT candidates=%d; credentials not printed", len(tokens), valid)
	if valid == 0 {
		t.Fatal("no unexpired local Firefox session for the selected origin")
	}
	if os.Getenv("FORK_FIREFOX_VERIFY_REMOTE") == "1" {
		remote := NewRemote(os.Getenv("HTTPS_PROXY"))
		for _, token := range tokens {
			identity, _, ok := browserJWTIdentity(token, time.Now(), true)
			if !ok {
				continue
			}
			c := Config{Provider: "sub2api", BaseURL: origin, Token: token}
			me, err := remote.get(ctx, c, "/api/v1/auth/me?timezone=Asia%2FShanghai")
			if err != nil {
				t.Fatal("selected station rejected the local Firefox session or was unreachable")
			}
			data := object(me["data"])
			remoteID := stringValue(data["id"])
			if remoteID == "" {
				remoteID = stringValue(data["user_id"])
			}
			if remoteID != identity {
				t.Fatal("selected station user did not match the local session")
			}
			groups, err := remote.Groups(ctx, c)
			if err != nil {
				t.Fatal("session verified, but station group/ratio lookup failed")
			}
			t.Logf("selected HTTPS station verified the same user; available groups=%d; token remains in memory", len(groups))
			break
		}
	}
}
