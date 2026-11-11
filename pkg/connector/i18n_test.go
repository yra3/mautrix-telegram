package connector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelegramConfigLanguageValidation(t *testing.T) {
	base := func(lang string) *TelegramConnector {
		tc := &TelegramConnector{
			Config: TelegramConfig{
				APIID:    12345,
				APIHash:  "0123456789abcdef0123456789abcdef",
				Language: lang,
			},
		}
		tc.Config.AnimatedSticker.Target = "webp"
		require.NoError(t, tc.Config.PostProcess())
		return tc
	}

	tcDefault := base("")
	assert.Equal(t, "en", tcDefault.Config.Language)
	assert.NoError(t, tcDefault.ValidateConfig())
	assert.False(t, tcDefault.isRussian())

	tcRu := base(" RU ")
	assert.Equal(t, "ru", tcRu.Config.Language)
	assert.NoError(t, tcRu.ValidateConfig())
	assert.True(t, tcRu.isRussian())

	tcBad := base("de")
	assert.Error(t, tcBad.ValidateConfig())
}

func TestTranslateHelp(t *testing.T) {
	raw := `This is your management room: prefixing commands with ` + "`!tg`" + ` is not required.
Parameters in [square brackets] are optional, while parameters in <angle brackets> are required.

#### General
**help** - Show this help message.

#### Authentication
**login** [_flow ID_] - Log into the bridge

#### Starting and managing chats
**start-chat** [_login ID_] <_identifier_> - Start a direct chat with the given user
**sync-chats** [_ID входа_] - Синхронизировать чаты
`
	tcRu := &TelegramConnector{Config: TelegramConfig{Language: "ru"}}
	gotRu := tcRu.translateHelp(raw, "!tg")
	assert.Contains(t, gotRu, "Это ваша комната управления: добавлять префикс `!tg` перед командами не обязательно.")
	assert.Contains(t, gotRu, "Параметры в [квадратных скобках] необязательны, параметры в <угловых скобках> обязательны.")
	assert.Contains(t, gotRu, "#### Общие")
	assert.Contains(t, gotRu, "**help** - Показать эту справку.")
	assert.Contains(t, gotRu, "#### Авторизация")
	assert.Contains(t, gotRu, "**login** [_ID способа_] - Войти в мост")
	assert.Contains(t, gotRu, "#### Чаты и управление ими")
	assert.Contains(t, gotRu, "**start-chat** [_ID входа_] <_идентификатор_> - Начать личный чат с указанным пользователем")
	assert.Contains(t, gotRu, "**sync-chats** [_ID входа_] - Синхронизировать чаты")

	tcEn := &TelegramConnector{Config: TelegramConfig{Language: "en"}}
	assert.Equal(t, raw, tcEn.translateHelp(raw, "!tg"))
}

func TestGetLoginFlowsLocalization(t *testing.T) {
	tcEn := &TelegramConnector{Config: TelegramConfig{Language: "en"}}
	flowsEn := tcEn.GetLoginFlows()
	require.Len(t, flowsEn, 4)
	assert.Equal(t, "Phone Number", flowsEn[0].Name)

	tcRu := &TelegramConnector{Config: TelegramConfig{Language: "ru"}}
	flowsRu := tcRu.GetLoginFlows()
	require.Len(t, flowsRu, 4)
	assert.Equal(t, "Номер телефона", flowsRu[0].Name)
	assert.Equal(t, "QR-код", flowsRu[1].Name)
	assert.Equal(t, "Токен бота", flowsRu[2].Name)
	assert.Equal(t, "Вручную", flowsRu[3].Name)
}
