package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

type SessionManager struct {
	password  string
	ttl       time.Duration
	mu        sync.RWMutex
	sessions  map[string]time.Time
	ticker    *time.Ticker
	done      chan struct{}
	wg        sync.WaitGroup
	startOnce sync.Once
	closeOnce sync.Once
	closed    atomic.Bool
}

func NewSessionManager(password string, ttl time.Duration) *SessionManager {
	return &SessionManager{
		password: password,
		ttl:      ttl,
		sessions: make(map[string]time.Time),
	}
}

func (m *SessionManager) Authenticate(password string) (token string, expiry time.Time, ok bool) {
	if m.password == "" {
		return "", time.Time{}, false
	}

	cfgHash := sha256.Sum256([]byte(m.password))
	pwHash := sha256.Sum256([]byte(password))
	pwOk := subtle.ConstantTimeCompare(pwHash[:], cfgHash[:]) == 1

	if !pwOk {
		return "", time.Time{}, false
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, false
	}

	token = hex.EncodeToString(b)
	expiry = time.Now().Add(m.ttl)

	m.mu.Lock()
	m.sessions[token] = expiry
	m.mu.Unlock()

	return token, expiry, true
}

func (m *SessionManager) Validate(token string) bool {
	if len(token) != 64 {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	target := []byte(token)
	var matchExpiry time.Time
	matched := false

	for storedToken, exp := range m.sessions {
		if subtle.ConstantTimeCompare(target, []byte(storedToken)) == 1 {
			matchExpiry = exp
			matched = true
			break
		}
	}

	if !matched {
		return false
	}
	return time.Now().Before(matchExpiry)
}

func (m *SessionManager) Invalidate(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

func (m *SessionManager) Start() {
	m.startOnce.Do(func() {
		if m.closed.Load() {
			return
		}

		interval := m.ttl / 4
		if interval < time.Minute {
			interval = time.Minute
		}
		if interval > 15*time.Minute {
			interval = 15 * time.Minute
		}

		m.done = make(chan struct{})
		m.ticker = time.NewTicker(interval)
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer m.ticker.Stop()
			for {
				select {
				case <-m.done:
					m.cleanup()
					return
				case <-m.ticker.C:
					m.cleanup()
				}
			}
		}()
	})
}

func (m *SessionManager) Close() {
	m.closed.Store(true)
	m.closeOnce.Do(func() {
		if m.done != nil {
			close(m.done)
		}
		m.wg.Wait()
	})
}

func (m *SessionManager) Active() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (m *SessionManager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for token, expiry := range m.sessions {
		if now.After(expiry) {
			delete(m.sessions, token)
		}
	}
}
