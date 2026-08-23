//go:build !windows && !darwin && !linux

package daemon

import "os"

// publishSocket uses link-and-unlink on Unix platforms without a dedicated
// no-replace rename primitive. Link fails when socketPath already exists, so a
// racing entry is preserved. The staged name remains private until unlink.
func publishSocket(stagedPath, socketPath string) error {
	if err := os.Link(stagedPath, socketPath); err != nil {
		return err
	}
	// Publication has succeeded once the no-replace link exists. Best-effort
	// removal of the private name must not turn that success into an error that
	// closes the listener while leaving the public socket behind; the caller's
	// deferred staging-directory cleanup gets another chance to remove it.
	_ = os.Remove(stagedPath)
	return nil
}
