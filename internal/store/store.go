package store

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"fastlink-tool/internal/model"
)

type Store struct {
	db   *sql.DB
	root string
}

const schema = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS accounts (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL DEFAULT 'password',
  label TEXT NOT NULL DEFAULT '',
  api_origin TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  password_enc TEXT NOT NULL DEFAULT '',
  subscription_url TEXT NOT NULL DEFAULT '',
  last_login INTEGER NOT NULL DEFAULT 0,
  last_fetch INTEGER NOT NULL DEFAULT 0,
  user_info_json TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sub_keys (
  source TEXT PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS cache (
  source TEXT PRIMARY KEY,
  url TEXT NOT NULL DEFAULT '',
  label TEXT NOT NULL DEFAULT '',
  fetched_at INTEGER NOT NULL DEFAULT 0,
  yaml BLOB NOT NULL,
  raw_yaml BLOB NOT NULL DEFAULT '',
  user_info_json TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS accounts_email_origin ON accounts(email, api_origin);
`

func Open(root string) (*Store, error) {
	runDir := filepath.Join(root, ".run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(runDir, "fastlink.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
		db.Close()
		return nil, err
	}
	for _, stmt := range strings.Split(schema, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("schema: %w", err)
		}
	}
	s := &Store{db: db, root: root}
	if err := s.addMissingColumns(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrateLegacy(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// addMissingColumns upgrades databases created before a column existed.
// CREATE TABLE IF NOT EXISTS does not add columns to an existing table, so each
// new column needs an explicit ALTER; "duplicate column" is the expected
// outcome once it has been applied.
func (s *Store) addMissingColumns() error {
	columns := []struct{ table, column, decl string }{
		{"cache", "raw_yaml", "BLOB NOT NULL DEFAULT ''"},
		{"accounts", "kind", "TEXT NOT NULL DEFAULT 'password'"},
	}
	for _, c := range columns {
		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.table, c.column, c.decl)
		if _, err := s.db.Exec(stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return fmt.Errorf("migrate %s.%s: %w", c.table, c.column, err)
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() int64 { return time.Now().Unix() }

// ---------------------------------------------------------------- migration

type legacyAccount struct {
	ID              string          `json:"id"`
	Label           string          `json:"label"`
	APIOrigin       string          `json:"apiOrigin"`
	Email           string          `json:"email"`
	Password        string          `json:"password"`
	SubscriptionURL string          `json:"subscriptionUrl"`
	LastLogin       int64           `json:"lastLogin"`
	LastFetch       int64           `json:"lastFetch"`
	UserInfo        *model.UserInfo `json:"userInfo"`
	Error           string          `json:"error"`
}

type legacyFile struct {
	Active    string            `json:"active"`
	ManualURL string            `json:"manualUrl"`
	Accounts  []legacyAccount   `json:"accounts"`
	SubKeys   map[string]string `json:"subKeys"`
}

type legacyCache struct {
	Source    string          `json:"source"`
	URL       string          `json:"url"`
	Label     string          `json:"label"`
	FetchedAt int64           `json:"fetchedAt"`
	YAML      string          `json:"yaml"`
	UserInfo  *model.UserInfo `json:"userInfo"`
}

func (s *Store) migrateLegacy() error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	accountsPath := filepath.Join(s.root, ".run", "accounts.json")
	raw, err := os.ReadFile(accountsPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	var legacy legacyFile
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return fmt.Errorf("accounts.json: %w", err)
	}

	caches := make([]legacyCache, 0)
	cacheDir := filepath.Join(s.root, ".run", "cache")
	if entries, err := os.ReadDir(cacheDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			block, err := os.ReadFile(filepath.Join(cacheDir, entry.Name()))
			if err != nil {
				continue
			}
			var c legacyCache
			if json.Unmarshal(block, &c) == nil && c.Source != "" {
				caches = append(caches, c)
			}
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stamp := now()
	for _, a := range legacy.Accounts {
		userInfo, _ := json.Marshal(a.UserInfo)
		if _, err := tx.Exec(`INSERT INTO accounts
			(id,label,api_origin,email,password_enc,subscription_url,last_login,last_fetch,user_info_json,error,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			a.ID, a.Label, a.APIOrigin, a.Email, a.Password, a.SubscriptionURL, a.LastLogin, a.LastFetch, string(userInfo), a.Error, stamp, stamp); err != nil {
			return err
		}
	}
	if legacy.Active != "" {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO settings(key,value) VALUES('active',?)`, legacy.Active); err != nil {
			return err
		}
	}
	if legacy.ManualURL != "" {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO settings(key,value) VALUES('manualUrl',?)`, legacy.ManualURL); err != nil {
			return err
		}
	}
	for source, key := range legacy.SubKeys {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO sub_keys(source,key,created_at) VALUES(?,?,?)`, source, key, stamp); err != nil {
			return err
		}
	}
	for _, c := range caches {
		userInfo, _ := json.Marshal(c.UserInfo)
		if _, err := tx.Exec(`INSERT OR REPLACE INTO cache(source,url,label,fetched_at,yaml,user_info_json) VALUES(?,?,?,?,?,?)`,
			c.Source, c.URL, c.Label, c.FetchedAt, []byte(c.YAML), string(userInfo)); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Keep the old files for rollback; the presence of the DB prevents a second import.
	_ = os.Rename(accountsPath, fmt.Sprintf("%s.migrated-%d", accountsPath, stamp))
	if entries, err := os.ReadDir(cacheDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			_ = os.Rename(filepath.Join(cacheDir, entry.Name()), filepath.Join(cacheDir, entry.Name()+fmt.Sprintf(".migrated-%d", stamp)))
		}
	}
	return nil
}

// ------------------------------------------------------------------ settings

func (s *Store) setting(key, fallback string) string {
	var value string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value); err != nil {
		return fallback
	}
	return value
}

func (s *Store) setSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) ActiveSource() string                { return s.setting("active", "local") }
func (s *Store) SetActiveSource(source string) error { return s.setSetting("active", source) }
func (s *Store) ManualURL() string                   { return s.setting("manualUrl", "") }
func (s *Store) SetManualURL(url string) error       { return s.setSetting("manualUrl", url) }

// ----------------------------------------------------------------- accounts

func scanAccount(row interface{ Scan(...any) error }) (model.Account, error) {
	var a model.Account
	var userInfo string
	if err := row.Scan(&a.ID, &a.Kind, &a.Label, &a.APIOrigin, &a.Email, &a.PasswordEnc, &a.SubscriptionURL, &a.LastLogin, &a.LastFetch, &userInfo, &a.Error); err != nil {
		return a, err
	}
	if a.Kind == "" {
		a.Kind = model.KindPassword
	}
	a.HasPassword = a.PasswordEnc != ""
	if userInfo != "" {
		_ = json.Unmarshal([]byte(userInfo), &a.UserInfo)
	}
	return a, nil
}

const accountColumns = `id,kind,label,api_origin,email,password_enc,subscription_url,last_login,last_fetch,user_info_json,error`

func (s *Store) Accounts() ([]model.Account, error) {
	rows, err := s.db.Query(`SELECT ` + accountColumns + ` FROM accounts ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.Account, 0)
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Account(id string) (*model.Account, error) {
	row := s.db.QueryRow(`SELECT `+accountColumns+` FROM accounts WHERE id = ?`, id)
	a, err := scanAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) UpsertAccount(a model.Account) error {
	userInfo, _ := json.Marshal(a.UserInfo)
	stamp := now()
	kind := a.Kind
	if kind == "" {
		kind = model.KindPassword
	}
	_, err := s.db.Exec(`INSERT INTO accounts
		(id,kind,label,api_origin,email,password_enc,subscription_url,last_login,last_fetch,user_info_json,error,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			kind=excluded.kind,
			label=excluded.label,
			api_origin=excluded.api_origin,
			email=excluded.email,
			password_enc=excluded.password_enc,
			subscription_url=excluded.subscription_url,
			last_login=excluded.last_login,
			last_fetch=excluded.last_fetch,
			user_info_json=excluded.user_info_json,
			error=excluded.error,
			updated_at=excluded.updated_at`,
		a.ID, kind, a.Label, a.APIOrigin, a.Email, a.PasswordEnc, a.SubscriptionURL, a.LastLogin, a.LastFetch, string(userInfo), a.Error, stamp, stamp)
	return err
}

// ClearAccountRuntime drops the cached subscription and credentials-derived state
// for one account. Called after a password change so the next fetch re-logs in
// instead of reusing a token minted with the old password.
func (s *Store) ClearAccountRuntime(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE accounts SET subscription_url='', last_login=0, last_fetch=0, user_info_json='', error='' WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM cache WHERE source = ?`, "account:"+id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteAccount(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM accounts WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sub_keys WHERE source = ?`, "account:"+id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM cache WHERE source = ?`, "account:"+id); err != nil {
		return err
	}
	var active string
	_ = tx.QueryRow(`SELECT value FROM settings WHERE key = 'active'`).Scan(&active)
	if active == "account:"+id {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO settings(key,value) VALUES('active','local')`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpdateAccountRuntime(id, subscriptionURL string, lastLogin, lastFetch int64, userInfo *model.UserInfo, errText string) error {
	userInfoJSON, _ := json.Marshal(userInfo)
	_, err := s.db.Exec(`UPDATE accounts SET
		subscription_url=CASE WHEN ? <> '' THEN ? ELSE subscription_url END,
		last_login=CASE WHEN ? > 0 THEN ? ELSE last_login END,
		last_fetch=CASE WHEN ? > 0 THEN ? ELSE last_fetch END,
		user_info_json=CASE WHEN ? <> '' THEN ? ELSE user_info_json END,
		error=?,
		updated_at=?
		WHERE id=?`,
		subscriptionURL, subscriptionURL, lastLogin, lastLogin, lastFetch, lastFetch, string(userInfoJSON), string(userInfoJSON), errText, now(), id)
	return err
}

// ------------------------------------------------------------------- sub keys

func randomSubKey() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func (s *Store) SubKey(source string) (string, error) {
	var key string
	err := s.db.QueryRow(`SELECT key FROM sub_keys WHERE source = ?`, source).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return key, err
}

func (s *Store) EnsureSubKey(source string) (string, error) {
	if key, err := s.SubKey(source); err != nil || key != "" {
		return key, err
	}
	key := randomSubKey()
	_, err := s.db.Exec(`INSERT INTO sub_keys(source,key,created_at) VALUES(?,?,?) ON CONFLICT(source) DO NOTHING`, source, key, now())
	if err != nil {
		return "", err
	}
	return s.SubKey(source)
}

func (s *Store) EnsureAllSubKeys() error {
	accounts, err := s.Accounts()
	if err != nil {
		return err
	}
	names := map[string]bool{"local": true, "manual": true}
	for _, a := range accounts {
		names["account:"+a.ID] = true
	}
	for name := range names {
		if _, err := s.EnsureSubKey(name); err != nil {
			return err
		}
	}
	rows, err := s.db.Query(`SELECT source FROM sub_keys`)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var source string
		if rows.Scan(&source) == nil && !names[source] {
			stale = append(stale, source)
		}
	}
	rows.Close()
	for _, source := range stale {
		if _, err := s.db.Exec(`DELETE FROM sub_keys WHERE source = ?`, source); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RotateSubKey(source string) (string, error) {
	key := randomSubKey()
	_, err := s.db.Exec(`INSERT INTO sub_keys(source,key,created_at) VALUES(?,?,?) ON CONFLICT(source) DO UPDATE SET key=excluded.key, created_at=excluded.created_at`, source, key, now())
	return key, err
}

func (s *Store) SubKeys() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT source,key FROM sub_keys`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var source, key string
		if err := rows.Scan(&source, &key); err != nil {
			return nil, err
		}
		out[source] = key
	}
	return out, rows.Err()
}

// --------------------------------------------------------------------- cache

func (s *Store) Cache(source string) (*model.Snapshot, error) {
	var snap model.Snapshot
	var yaml, rawYAML []byte
	var userInfo string
	err := s.db.QueryRow(`SELECT source,url,label,fetched_at,yaml,raw_yaml,user_info_json FROM cache WHERE source = ?`, source).
		Scan(&snap.Source, &snap.URL, &snap.Label, &snap.FetchedAt, &yaml, &rawYAML, &userInfo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snap.YAML = string(yaml)
	snap.RawYAML = string(rawYAML)
	if userInfo != "" {
		_ = json.Unmarshal([]byte(userInfo), &snap.UserInfo)
	}
	return &snap, nil
}

func (s *Store) PutCache(snap model.Snapshot) error {
	userInfo, _ := json.Marshal(snap.UserInfo)
	_, err := s.db.Exec(`INSERT INTO cache(source,url,label,fetched_at,yaml,raw_yaml,user_info_json) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(source) DO UPDATE SET url=excluded.url,label=excluded.label,fetched_at=excluded.fetched_at,yaml=excluded.yaml,raw_yaml=excluded.raw_yaml,user_info_json=excluded.user_info_json`,
		snap.Source, snap.URL, snap.Label, snap.FetchedAt, []byte(snap.YAML), []byte(snap.RawYAML), string(userInfo))
	return err
}
