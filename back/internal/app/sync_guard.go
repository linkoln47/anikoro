package app

import "sync"

type inMemoryUserSyncGuard struct {
	mu                sync.Mutex
	activeUserSyncIDs map[int64]struct{}
}

func newInMemoryUserSyncGuard() *inMemoryUserSyncGuard {
	return &inMemoryUserSyncGuard{
		activeUserSyncIDs: make(map[int64]struct{}),
	}
}

func (guard *inMemoryUserSyncGuard) TryBeginUserSync(userID int64) bool {
	guard.mu.Lock()
	defer guard.mu.Unlock()

	if _, exists := guard.activeUserSyncIDs[userID]; exists {
		return false
	}

	guard.activeUserSyncIDs[userID] = struct{}{}
	return true
}

func (guard *inMemoryUserSyncGuard) FinishUserSync(userID int64) {
	guard.mu.Lock()
	defer guard.mu.Unlock()
	delete(guard.activeUserSyncIDs, userID)
}
