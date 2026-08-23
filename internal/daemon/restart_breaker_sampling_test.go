package daemon

import (
	"bytes"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestWarnRestartBreakerSampling(t *testing.T) {
	t.Run("warns with both effective durations", func(t *testing.T) {
		var output bytes.Buffer
		logger := zerolog.New(&output)

		warnRestartBreakerSampling(logger, 15*time.Minute, 10*time.Minute)

		logs := output.String()
		assert.Contains(t, logs, `"level":"warn"`)
		assert.Contains(t, logs, `"drift_interval":900000`)
		assert.Contains(t, logs, `"restart_window":600000`)
		assert.Contains(t, logs, "BOSUN_DRIFT_INTERVAL exceeds BOSUN_RESTART_WINDOW")
	})

	t.Run("compatible cadence emits no warning", func(t *testing.T) {
		var output bytes.Buffer
		logger := zerolog.New(&output)

		warnRestartBreakerSampling(logger, 5*time.Minute, 10*time.Minute)

		assert.Empty(t, output.String())
	})
}
