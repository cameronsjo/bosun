package reconcile

import "os"

// Lock file and lock directory permissions.
//
// flock(2) grants LOCK_EX to any open descriptor regardless of the open mode,
// so a world-readable lock file lets any local uid open it read-only and hold
// the exclusive lock indefinitely -- every subsequent reconcile then fails at
// acquireLock and the circuit breaker trips. Owner-only permissions keep the
// lock reachable by the daemon's own uid, the only principal that legitimately
// reconciles.
//
// lockDirMode applies to directories bosun creates itself and is required on
// the final existing lock directory too. Shared or differently owned final
// directories fail closed with an actionable owner/mode repair message.
const (
	lockFileMode os.FileMode = 0o600
	lockDirMode  os.FileMode = 0o700
)
