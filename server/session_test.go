package server

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestSessionManager_Authenticate_Valid(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", time.Hour)
	token, expiry, ok := m.Authenticate("secret")

	if !ok {
		t.Fatal("expected ok=true for correct password")
	}
	if len(token) != 64 {
		t.Errorf("expected token length 64, got %d", len(token))
	}
	if !expiry.After(time.Now()) {
		t.Error("expected expiry in the future")
	}
}

func TestSessionManager_Authenticate_Invalid(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", time.Hour)
	_, _, ok := m.Authenticate("wrong")

	if ok {
		t.Error("expected ok=false for wrong password")
	}
}

func TestSessionManager_Authenticate_EmptyPassword(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("", time.Hour)
	_, _, ok := m.Authenticate("anything")

	if ok {
		t.Error("expected ok=false when configured password is empty")
	}

	_, _, ok = m.Authenticate("")
	if ok {
		t.Error("expected ok=false when both passwords are empty")
	}
}

func TestSessionManager_Validate_RoundTrip(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", time.Hour)
	token, _, ok := m.Authenticate("secret")
	if !ok {
		t.Fatal("expected authentication to succeed")
	}

	if !m.Validate(token) {
		t.Error("expected Validate to return true for valid token")
	}
}

func TestSessionManager_Validate_UnknownToken(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", time.Hour)

	if m.Validate("0000000000000000000000000000000000000000000000000000000000000000") {
		t.Error("expected Validate to return false for unknown token")
	}
}

func TestSessionManager_Validate_AfterInvalidate(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", time.Hour)
	token, _, ok := m.Authenticate("secret")
	if !ok {
		t.Fatal("expected authentication to succeed")
	}

	m.Invalidate(token)

	if m.Validate(token) {
		t.Error("expected Validate to return false after Invalidate")
	}
}

func TestSessionManager_Validate_Expired(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", 1*time.Millisecond)
	token, _, ok := m.Authenticate("secret")
	if !ok {
		t.Fatal("expected authentication to succeed")
	}

	time.Sleep(50 * time.Millisecond)

	if m.Validate(token) {
		t.Error("expected expired token to be invalid")
	}
}

func TestSessionManager_StartClose_Idempotent(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", time.Hour)

	m.Start()
	m.Start()
	m.Start()

	m.Close()
	m.Close()
	m.Close()

	// Should not panic; if we get here, it passed.
}

func TestSessionManager_ConcurrentAuthenticate(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", time.Hour)

	const n = 100
	var wg sync.WaitGroup
	tokens := make(map[string]bool)
	var mu sync.Mutex

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, _, ok := m.Authenticate("secret")
			if ok {
				mu.Lock()
				tokens[tok] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(tokens) != n {
		t.Errorf("expected %d unique tokens, got %d", n, len(tokens))
	}

	for tok := range tokens {
		if !m.Validate(tok) {
			t.Errorf("token should be valid: %s", tok)
		}
	}
}

func TestSessionManager_Active(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", time.Hour)
	if n := m.Active(); n != 0 {
		t.Errorf("expected 0 active sessions, got %d", n)
	}

	m.Authenticate("secret")
	if n := m.Active(); n != 1 {
		t.Errorf("expected 1 active session, got %d", n)
	}

	m.Authenticate("secret")
	if n := m.Active(); n != 2 {
		t.Errorf("expected 2 active sessions, got %d", n)
	}

	m.Invalidate("nonexistent")
	if n := m.Active(); n != 2 {
		t.Errorf("Invaliding unknown token should not change count, got %d", n)
	}
}

func TestSessionManager_CloseBeforeStart(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", time.Hour)
	m.Close()
	m.Close()

	// Should not panic.
}

func TestSessionManager_Start_Cleanup(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", 50*time.Millisecond)
	m.Start()

	token, _, ok := m.Authenticate("secret")
	if !ok {
		t.Fatal("expected authentication to succeed")
	}

	if !m.Validate(token) {
		t.Fatal("expected token to be valid before expiry")
	}

	time.Sleep(100 * time.Millisecond)

	// Validate catches expiry even before background cleanup fires.
	if m.Validate(token) {
		t.Error("expected expired token to be invalid")
	}

	// Close triggers synchronous cleanup.
	m.Close()

	if m.Active() != 0 {
		t.Errorf("expected 0 active sessions after Close cleanup, got %d", m.Active())
	}
}

func TestSessionManager_StartAfterClose(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", time.Hour)
	before := runtime.NumGoroutine()

	m.Close()

	goroutinesAfterClose := runtime.NumGoroutine()

	m.Start()

	time.Sleep(50 * time.Millisecond)

	goroutinesAfterStart := runtime.NumGoroutine()

	t.Logf("goroutines: before=%d, after-close=%d, after-start=%d", before, goroutinesAfterClose, goroutinesAfterStart)

	if goroutinesAfterStart > goroutinesAfterClose+2 {
		t.Errorf("Start() after Close() leaked goroutines: before-start=%d, after-start=%d", goroutinesAfterClose, goroutinesAfterStart)
	}
}

func TestSessionManager_Validate_WrongTokenLength(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", time.Hour)
	token, _, ok := m.Authenticate("secret")
	if !ok {
		t.Fatal("expected authentication to succeed")
	}
	if !m.Validate(token) {
		t.Error("expected valid token to pass validation")
	}
	if m.Validate(token[:63]) {
		t.Error("expected wrong-length token (63) to fail validation")
	}
	if m.Validate(token + "a") {
		t.Error("expected wrong-length token (65) to fail validation")
	}
	if m.Validate("") {
		t.Error("expected empty token to fail validation")
	}
}

func TestSessionManager_Authenticate_PasswordLengthIndependent(t *testing.T) {
	t.Parallel()

	short := NewSessionManager("s", time.Hour)
	long := NewSessionManager("a-very-long-password-that-should-not-leak-length-via-timing", time.Hour)

	const n = 50
	var shortDur, longDur time.Duration

	for i := 0; i < n; i++ {
		start := time.Now()
		short.Authenticate("wrong")
		shortDur += time.Since(start)

		start = time.Now()
		long.Authenticate("wrong")
		longDur += time.Since(start)
	}

	shortAvg := shortDur / n
	longAvg := longDur / n

	t.Logf("short password avg: %v, long password avg: %v", shortAvg, longAvg)

	ratio := float64(shortAvg) / float64(longAvg)
	if ratio < 0.5 || ratio > 2.0 {
		t.Errorf("timing differs too much between short and long passwords: ratio=%f", ratio)
	}
}

func TestSessionManager_ConcurrentMixedReadWrite(t *testing.T) {
	t.Parallel()

	m := NewSessionManager("secret", time.Hour)
	m.Start()
	defer m.Close()

	const n = 50
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, _, ok := m.Authenticate("secret")
			if ok {
				m.Validate(token)
			}
		}()
	}

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Authenticate("wrong")
		}()
	}

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Active()
		}()
	}

	wg.Wait()
}
