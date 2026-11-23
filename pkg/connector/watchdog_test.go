package connector

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRestartBackoff(t *testing.T) {
	assert.Equal(t, 10*time.Second, restartBackoff(1))
	assert.Equal(t, 30*time.Second, restartBackoff(3))
	// The delay is capped so a permanently broken session does not restart in a
	// tight loop.
	assert.Equal(t, 2*time.Minute, restartBackoff(60))
	assert.Equal(t, 2*time.Minute, restartBackoff(1000))
	// And it never drops below the minimum, even for bogus input.
	assert.Equal(t, 5*time.Second, restartBackoff(0))
	assert.Equal(t, 5*time.Second, restartBackoff(-5))
}

func TestWatchdogConstantsAreSane(t *testing.T) {
	// The watchdog must tolerate a backlog of a few minutes (bridgev2 event
	// handling can legitimately lag during backfill) before it restarts a
	// client, and it must not restart more often than once per restart gap.
	lag := time.Duration(watchdogMaxStalls) * watchdogInterval
	assert.GreaterOrEqual(t, lag, 10*time.Minute, "watchdog is too trigger-happy")
	assert.LessOrEqual(t, lag, 30*time.Minute, "watchdog would take too long to recover")
	assert.GreaterOrEqual(t, watchdogMinRestartGap, lag, "restart gap must be at least the detection window")
	assert.Greater(t, watchdogRPCTimeout, time.Duration(0))
}
