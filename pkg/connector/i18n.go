package connector

import (
	"fmt"
	"strings"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/commands"
	"maunium.net/go/mautrix/bridgev2/status"
)

func (tc *TelegramConnector) isRussian() bool {
	return tc != nil && tc.Config.Language == "ru"
}

func (tc *TelegramConnector) tr(en, ru string) string {
	if tc.isRussian() {
		return ru
	}
	return en
}

func (tc *TelegramConnector) trf(en, ru string, args ...any) string {
	return fmt.Sprintf(tc.tr(en, ru), args...)
}

func (tc *TelegramConnector) respErr(base bridgev2.RespError, ru string) bridgev2.RespError {
	if tc.isRussian() {
		base.Err = ru
	}
	return base
}

func connectorFromEvent(ce *commands.Event) *TelegramConnector {
	if ce != nil && ce.Bridge != nil {
		if tc, ok := ce.Bridge.Network.(*TelegramConnector); ok {
			return tc
		}
	}
	return nil
}

func trEvent(ce *commands.Event, en, ru string) string {
	return connectorFromEvent(ce).tr(en, ru)
}

func initBridgeStateHumanErrors(lang string) {
	if lang == "ru" {
		status.BridgeStateHumanErrors.Update(status.BridgeStateErrorMap{
			"tg-client-dead":        "Отключено от Telegram, переподключение",
			"tg-connectivity-error": "Потеряно соединение с Telegram, переподключение",
			"tg-unknown-error":      "Неожиданный сбой подключения к Telegram",
		})
	} else {
		status.BridgeStateHumanErrors.Update(status.BridgeStateErrorMap{
			"tg-client-dead":        "Disconnected from Telegram, reconnecting",
			"tg-connectivity-error": "Lost connection to Telegram, reconnecting",
			"tg-unknown-error":      "The Telegram connection failed unexpectedly",
		})
	}
}

type localizedCommandHelp struct {
	args        string
	description string
}

var ruHelpSections = map[string]string{
	"#### General":                     "#### Общие",
	"#### Authentication":              "#### Авторизация",
	"#### Starting and managing chats": "#### Чаты и управление ими",
	"#### Miscellaneous":               "#### Разное",
	"#### Administration":              "#### Администрирование",
}

var ruHelpCommands = map[string]localizedCommandHelp{
	"help": {
		description: "Показать эту справку.",
	},
	"cancel": {
		description: "Отменить текущее действие.",
	},
	"set-management-room": {
		description: "Назначить эту комнату комнатой управления",
	},
	"login": {
		args:        "[_ID способа_]",
		description: "Войти в мост",
	},
	"relogin": {
		args:        "<_ID входа_> [_ID способа_]",
		description: "Повторно авторизовать существующий вход",
	},
	"list-logins": {
		description: "Показать список ваших входов",
	},
	"logout": {
		args:        "<_ID входа_>",
		description: "Выйти из моста",
	},
	"set-preferred-login": {
		args:        "<_ID входа_>",
		description: "Выбрать основной вход для отправки сообщений в этот портал (актуально при нескольких аккаунтах)",
	},
	"set-relay": {
		args:        "[_ID входа_]",
		description: "Использовать ваш аккаунт для пересылки сообщений от неавторизованных пользователей",
	},
	"unset-relay": {
		description: "Отключить пересылку сообщений от неавторизованных пользователей",
	},
	"set-profile": {
		args:        "<_ID входа_> <name|avatar> [_значение_]",
		description: "Обновить имя или аватар аккаунта (оставьте значение пустым для удаления)",
	},
	"resolve-identifier": {
		args:        "[_ID входа_] <_идентификатор_>",
		description: "Проверить, есть ли указанный пользователь в Telegram",
	},
	"start-chat": {
		args:        "[_ID входа_] <_идентификатор_>",
		description: "Начать личный чат с указанным пользователем",
	},
	"create-group": {
		args:        "[_тип группы_]",
		description: "Создать новую групповую беседу для текущей комнаты Matrix",
	},
	"create-portal": {
		args:        "[ID входа] <ID чата>",
		description: "Создать новую комнату Matrix для существующего чата",
	},
	"search": {
		args:        "<_запрос_>",
		description: "Поиск пользователей в Telegram",
	},
	"id": {
		description: "Показать внутренний сетевой ID текущей комнаты-портала",
	},
	"bridge": {
		args:        "[ID входа] <ID чата>",
		description: "Привязать существующий чат из Telegram к этой комнате Matrix",
	},
	"unbridge": {
		description: "Отвязать текущую комнату-портал",
	},
	"sync-portal": {
		description: "Синхронизировать текущую комнату-портал",
	},
	"mute": {
		args:        "[длительность]",
		description: "Отключить или включить уведомления для чата в Telegram",
	},
	"delete-chat": {
		args:        "[--for-everyone]",
		description: "Удалить текущий чат в Telegram",
	},
	"block-user": {
		args:        "[--report-spam]",
		description: "Заблокировать собеседника текущего личного чата в Telegram",
	},
	"filter": {
		args:        "<allow/deny> <ID чата> [получатель]",
		description: "Управлять фильтром создания комнат (изменения действуют до перезапуска)",
	},
	"import-image-pack": {
		args:        "<url>",
		description: "Импортировать набор стикеров или эмодзи из Telegram",
	},
	"sync-chats": {
		args:        "[_ID входа_]",
		description: "Синхронизировать чаты",
	},
	"upgrade": {
		description: "Преобразовать обычную группу в супергруппу Telegram",
	},
	"join": {
		args:        "[ID входа] <ссылка-приглашение>",
		description: "Присоединиться к группе или каналу Telegram по ссылке-приглашению",
	},
	"emoji-pack": {
		args:        "<upload/download/list/help> [аргументы...]",
		description: "Перенос наборов эмодзи и стикеров между Matrix и Telegram",
	},
	"delete-portal": {
		description: "Удалить текущую комнату-портал",
	},
	"delete-all-portals": {
		description: "Удалить все известные мосту порталы",
	},
	"sudo": {
		args:        "[--create] <_ID пользователя_> <_команда_> [_аргументы..._]",
		description: "Выполнить команду от имени другого пользователя",
	},
	"doin": {
		args:        "<_ID комнаты_> <_команда_> [_аргументы..._]",
		description: "Выполнить команду в другой комнате",
	},
	"debug-register-push": {
		args:        "<_ID входа_> <_тип push_> <_push-токен_>",
		description: "Зарегистрировать push-уведомления",
	},
	"debug-account-data": {
		args:        "<_тип_> <_содержимое_>",
		description: "Отправить событие account data комнаты в мост",
	},
	"debug-reset-network": {
		args:        "[--reset-transport]",
		description: "Сбросить сетевые подключения к Telegram",
	},
}

func (tc *TelegramConnector) translateHelp(rawHelp, cmdPrefix string) string {
	if !tc.isRussian() {
		return rawHelp
	}
	replacer := strings.NewReplacer(
		fmt.Sprintf("This is your management room: prefixing commands with `%s` is not required.", cmdPrefix),
		fmt.Sprintf("Это ваша комната управления: добавлять префикс `%s` перед командами не обязательно.", cmdPrefix),
		fmt.Sprintf("**This is a portal room**: you must always prefix commands with `%s`. Management commands will not be bridged.", cmdPrefix),
		fmt.Sprintf("**Это комната-портал**: всегда добавляйте префикс `%s` перед командами. Команды управления не пересылаются в Telegram.", cmdPrefix),
		fmt.Sprintf("This is not your management room: prefixing commands with `%s` is required.", cmdPrefix),
		fmt.Sprintf("Это не ваша комната управления: перед командами обязательно добавлять префикс `%s`.", cmdPrefix),
		"Parameters in [square brackets] are optional, while parameters in <angle brackets> are required.",
		"Параметры в [квадратных скобках] необязательны, параметры в <угловых скобках> обязательны.",
	)

	lines := strings.Split(rawHelp, "\n")
	for i, line := range lines {
		if ruSection, ok := ruHelpSections[line]; ok {
			lines[i] = ruSection
			continue
		}
		if strings.HasPrefix(line, "**") {
			if end := strings.Index(line[2:], "**"); end > 0 {
				cmd := line[2 : 2+end]
				if ruMeta, ok := ruHelpCommands[cmd]; ok {
					if ruMeta.args == "" {
						lines[i] = fmt.Sprintf("**%s** - %s", cmd, ruMeta.description)
					} else {
						lines[i] = fmt.Sprintf("**%s** %s - %s", cmd, ruMeta.args, ruMeta.description)
					}
					continue
				}
			}
		}
		lines[i] = replacer.Replace(line)
	}
	return strings.Join(lines, "\n")
}
