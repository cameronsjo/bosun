package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Remote compose up must be bounded by ComposeUpTimeout the way local compose
// up is (#727): a hung `docker compose up` over SSH would otherwise hold the
// reconcile until the outer cycle deadline, or forever without one. The shim
// `exec`s sleep so the killed process is the one holding the stderr pipe.
func TestComposeUpRemote_BoundedByComposeUpTimeout(t *testing.T) {
	installComposeRetrySSHShim(t, `
case "$*" in
  *" up "*)
    exec sleep 5
    ;;
  *" ps "*)
    echo '[]'
    exit 0
    ;;
esac
exit 1`)

	d := &DeployOps{ComposeUpTimeout: 200 * time.Millisecond}
	start := time.Now()
	err := d.ComposeUpRemote(context.Background(), "user@testhost", "/srv/compose")
	elapsed := time.Since(start)

	require.Error(t, err, "a compose up that outlives ComposeUpTimeout must fail")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, 3*time.Second, "compose up must be cut off at ComposeUpTimeout")
}

// Called without a caller deadline, the remote failure classification must
// still be bounded, matching the local classifyComposeFailure.
func TestClassifyComposeFailureRemote_BoundedWithoutDeadline(t *testing.T) {
	installComposeRetrySSHShim(t, `
case "$*" in
  *" ps "*)
    exec sleep 5
    ;;
esac
exit 1`)

	d := &DeployOps{ComposeUpTimeout: 200 * time.Millisecond}
	start := time.Now()
	_, err := d.classifyComposeFailureRemote(context.Background(), "user@testhost", "/srv/compose")
	elapsed := time.Since(start)

	require.Error(t, err, "a compose ps that outlives ComposeUpTimeout must fail")
	assert.Less(t, elapsed, 3*time.Second, "compose ps must be cut off at ComposeUpTimeout")
}
