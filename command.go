package tgbot

import "sync"

// CommandLock provides mutual exclusion for locked commands per user.
// Locked commands block other locked commands for the same user.
// Non-locked commands always run without blocking.
type CommandLock struct {
	mu    sync.Mutex
	locks map[int64]bool
}

// NewCommandLock creates a new CommandLock.
func NewCommandLock() *CommandLock {
	return &CommandLock{
		locks: make(map[int64]bool),
	}
}

// TryAcquire checks if command can proceed and optionally acquires lock.
// If acquire is false, always returns true.
// If acquire is true, returns true only if lock was acquired.
func (l *CommandLock) TryAcquire(userID int64, acquire bool) bool {
	if !acquire {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.locks[userID] {
		return false
	}

	l.locks[userID] = true
	return true
}

// Unlock releases the lock for user.
func (l *CommandLock) Unlock(userID int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.locks, userID)
}
