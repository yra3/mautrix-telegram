package connector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"

	"go.mau.fi/mautrix-telegram/pkg/gotd/tg"
)

func newTestClient(ignoreContactNames bool, loginUserID int64) *TelegramClient {
	cfg := TelegramConfig{
		IgnoreContactNames:  ignoreContactNames,
		DisplaynameTemplate: "{{ if .Deleted }}Deleted account {{ .UserID }}{{ else }}{{ .FullName }}{{ end }}",
	}
	if err := cfg.PostProcess(); err != nil {
		panic(err)
	}
	return &TelegramClient{
		telegramUserID: loginUserID,
		main: &TelegramConnector{
			Config: cfg,
		},
	}
}

func makeGhost(name string, meta *GhostMetadata) *bridgev2.Ghost {
	if meta == nil {
		meta = &GhostMetadata{}
	}
	return &bridgev2.Ghost{
		Ghost: &database.Ghost{
			Name:     name,
			Metadata: meta,
		},
	}
}

func TestResolveUserDisplayname_IgnoreContactNames(t *testing.T) {
	const natalyaID int64 = 6758798099
	const yuraID int64 = 1285904238
	const targetID int64 = 5242019925

	t.Run("contact with username falls back to @username when ghost empty", func(t *testing.T) {
		tc := newTestClient(true, natalyaID)
		ghost := makeGhost("", nil)
		user := &tg.User{
			ID:        targetID,
			Contact:   true,
			FirstName: "Внук Сандалов Лев Дмитриевич",
			Username:  "PDFmast3r",
		}
		name := tc.resolveUserDisplayname(user, ghost)
		require.NotNil(t, name)
		assert.Equal(t, "@PDFmast3r", *name)

		meta := ghost.Metadata.(*GhostMetadata)
		assert.True(t, tc.applyUserGhostMetadata(user, meta))
		assert.True(t, meta.SourceIsContact)
		assert.Equal(t, natalyaID, meta.ContactSource)
	})

	t.Run("contact without username falls back to Telegram ID when existing name was contact", func(t *testing.T) {
		tc := newTestClient(true, natalyaID)
		ghost := makeGhost("Внук Губин Ваня", &GhostMetadata{
			NotMin:          true,
			ContactSource:   natalyaID,
			SourceIsContact: true,
		})
		user := &tg.User{
			ID:        6389966537,
			Contact:   true,
			FirstName: "Внук Губин Ваня",
		}
		name := tc.resolveUserDisplayname(user, ghost)
		require.NotNil(t, name)
		assert.Equal(t, "Telegram 6389966537", *name)
		meta := ghost.Metadata.(*GhostMetadata)
		tc.applyUserGhostMetadata(user, meta)
		assert.True(t, meta.SourceIsContact)
	})

	t.Run("non-contact overrides fallback or old contact name", func(t *testing.T) {
		tc := newTestClient(true, yuraID)
		ghost := makeGhost("@PDFmast3r", &GhostMetadata{
			NotMin:          true,
			ContactSource:   natalyaID,
			SourceIsContact: true,
		})
		user := &tg.User{
			ID:        targetID,
			Contact:   false,
			FirstName: "Лев",
			LastName:  "Сандалов",
			Username:  "PDFmast3r",
		}
		name := tc.resolveUserDisplayname(user, ghost)
		require.NotNil(t, name)
		assert.Equal(t, "Лев Сандалов", *name)

		meta := ghost.Metadata.(*GhostMetadata)
		assert.True(t, tc.applyUserGhostMetadata(user, meta))
		assert.False(t, meta.SourceIsContact)
		assert.Equal(t, yuraID, meta.ContactSource)
	})

	t.Run("contact does not overwrite existing public name", func(t *testing.T) {
		tc := newTestClient(true, natalyaID)
		ghost := makeGhost("Лев Сандалов", &GhostMetadata{
			NotMin:          true,
			ContactSource:   yuraID,
			SourceIsContact: false,
		})
		user := &tg.User{
			ID:        targetID,
			Contact:   true,
			FirstName: "Внук Сандалов Лев Дмитриевич",
			Username:  "PDFmast3r",
		}
		name := tc.resolveUserDisplayname(user, ghost)
		assert.Nil(t, name)

		meta := ghost.Metadata.(*GhostMetadata)
		assert.False(t, tc.applyUserGhostMetadata(user, meta))
		assert.False(t, meta.SourceIsContact)
		assert.Equal(t, yuraID, meta.ContactSource)
	})

	t.Run("min constructor overrides old contact name with public name", func(t *testing.T) {
		tc := newTestClient(true, yuraID)
		ghost := makeGhost("Внук Сандалов Лев Дмитриевич", &GhostMetadata{
			NotMin:          true,
			ContactSource:   natalyaID,
			SourceIsContact: true,
		})
		user := &tg.User{
			ID:        targetID,
			Min:       true,
			FirstName: "Лев",
			LastName:  "Сандалов",
		}
		name := tc.resolveUserDisplayname(user, ghost)
		require.NotNil(t, name)
		assert.Equal(t, "Лев Сандалов", *name)

		meta := ghost.Metadata.(*GhostMetadata)
		assert.True(t, tc.applyUserGhostMetadata(user, meta))
		assert.False(t, meta.SourceIsContact)
		assert.Equal(t, int64(0), meta.ContactSource)
	})

	t.Run("self user uses own name even when Contact=true", func(t *testing.T) {
		tc := newTestClient(true, yuraID)
		ghost := makeGhost("", nil)
		user := &tg.User{
			ID:        yuraID,
			Self:      true,
			Contact:   true,
			FirstName: "Юрий",
			LastName:  "Соловей",
		}
		name := tc.resolveUserDisplayname(user, ghost)
		require.NotNil(t, name)
		assert.Equal(t, "Юрий Соловей", *name)

		meta := ghost.Metadata.(*GhostMetadata)
		assert.True(t, tc.applyUserGhostMetadata(user, meta))
		assert.False(t, meta.SourceIsContact)
	})

	t.Run("legacy mode when IgnoreContactNames is false", func(t *testing.T) {
		tc := newTestClient(false, natalyaID)
		ghost := makeGhost("", nil)
		user := &tg.User{
			ID:        targetID,
			Contact:   true,
			FirstName: "Внук",
		}
		name := tc.resolveUserDisplayname(user, ghost)
		require.NotNil(t, name)
		assert.Equal(t, "Внук", *name)
	})
}
