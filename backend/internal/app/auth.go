package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"golang.org/x/crypto/bcrypt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("secure random source unavailable")
	}
	return hex.EncodeToString(b)
}
func (s *Server) tokenHash(token string) string {
	h := hmac.New(sha256.New, []byte(s.Secret))
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}

type limitEntry struct {
	Count int
	Reset time.Time
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]limitEntry
}

func (l *limiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	v := l.entries[key]
	return time.Now().Before(v.Reset) && v.Count >= 5
}
func (l *limiter) failed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.entries) > 4096 {
		for k, v := range l.entries {
			if !now.Before(v.Reset) {
				delete(l.entries, k)
			}
		}
	}
	v := l.entries[key]
	if !now.Before(v.Reset) {
		v = limitEntry{Reset: now.Add(time.Minute)}
	}
	v.Count++
	l.entries[key] = v
}
func (l *limiter) clear(key string) { l.mu.Lock(); defer l.mu.Unlock(); delete(l.entries, key) }
func source(r *http.Request) string { // The API is internal; Nginx overwrites X-Real-IP.
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return h
	}
	return r.RemoteAddr
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	key := source(r)
	if s.limit.blocked(key) {
		w.Header().Set("Retry-After", "60")
		s.fail(w, r, &Problem{429, "rate_limited", "登录尝试过多，请一分钟后重试"})
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(w, r, &input); err != nil {
		s.fail(w, r, err)
		return
	}
	var u User
	var hash string
	var enabled bool
	err := s.DB.QueryRowContext(r.Context(), "SELECT id,org_id,username,display_name,role,password_hash,enabled FROM users WHERE username=?", input.Username).Scan(&u.ID, &u.OrgID, &u.Username, &u.Name, &u.Role, &hash, &enabled)
	if err != nil && err != sql.ErrNoRows {
		s.fail(w, r, err)
		return
	}
	if err == sql.ErrNoRows {
		hash = s.dummyHash
	}
	valid := bcrypt.CompareHashAndPassword([]byte(hash), []byte(input.Password)) == nil
	if err == sql.ErrNoRows || !valid || !enabled {
		s.limit.failed(key)
		_ = writeAudit(r.Context(), s.DB, requestID(r), nil, "login", nil, "denied", map[string]any{})
		s.fail(w, r, &Problem{401, "unauthorized", "账号或密码错误"})
		return
	}
	token := randomHex(32)
	expires := time.Now().UTC().Add(12 * time.Hour)
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)", s.tokenHash(token), u.ID, expires); err == nil {
		err = writeAudit(r.Context(), tx, requestID(r), &u, "login", nil, "success", map[string]any{})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.limit.clear(key)
	send(w, 200, map[string]any{"access_token": token, "token_type": "Bearer", "expires_at": expires, "user": u})
}

type userKey struct{}

func current(r *http.Request) User { return r.Context().Value(userKey{}).(User) }
func (s *Server) protect(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := strings.Fields(r.Header.Get("Authorization"))
		if len(h) != 2 || !strings.EqualFold(h[0], "Bearer") || len(h[1]) != 64 {
			s.fail(w, r, &Problem{401, "unauthorized", "请先登录"})
			return
		}
		var u User
		err := s.DB.QueryRowContext(r.Context(), "SELECT u.id,u.org_id,u.username,u.display_name,u.role FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>UTC_TIMESTAMP(6) AND u.enabled=TRUE", s.tokenHash(h[1])).Scan(&u.ID, &u.OrgID, &u.Username, &u.Name, &u.Role)
		if err == sql.ErrNoRows {
			s.fail(w, r, &Problem{401, "unauthorized", "登录已失效，请重新登录"})
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if u.Role != "requester" && u.Role != "technician" && u.Role != "admin" {
			s.fail(w, r, &Problem{401, "unauthorized", "账号权限无效，请联系管理员"})
			return
		}
		fn(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	}
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	h := strings.Fields(r.Header.Get("Authorization"))
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE token_hash=?", s.tokenHash(h[1])); err == nil {
		err = writeAudit(r.Context(), tx, requestID(r), &u, "logout", nil, "success", map[string]any{})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
