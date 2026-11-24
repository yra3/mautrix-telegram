package connector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"maunium.net/go/mautrix/id"
)

func TestShouldMuteForBackfill(t *testing.T) {
	room := id.RoomID("!room:example.com")
	// The initial forward backfill of a freshly created room is the only case where
	// the portal may be muted temporarily.
	assert.True(t, shouldMuteForBackfill(true, false, room, true))
	// Catch-up backfills have an anchor message and happen in rooms that already
	// delivered notifications before, so they must not mute anything.
	assert.False(t, shouldMuteForBackfill(true, true, room, true))
	assert.False(t, shouldMuteForBackfill(false, false, room, true))
	// Without a room there is nothing to mute.
	assert.False(t, shouldMuteForBackfill(true, false, "", true))
	// And the feature must stay off unless the config enables it.
	assert.False(t, shouldMuteForBackfill(true, false, room, false))
}
