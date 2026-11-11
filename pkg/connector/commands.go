// mautrix-telegram - A Matrix-Telegram puppeting bridge.
// Copyright (C) 2025 Sumner Evans
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package connector

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/commands"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/format"

	"go.mau.fi/mautrix-telegram/pkg/connector/ids"
	"go.mau.fi/mautrix-telegram/pkg/gotd/tg"
	"go.mau.fi/mautrix-telegram/pkg/gotd/tgerr"
)

func (tc *TelegramConnector) buildCommands() []commands.CommandHandler {
	return []commands.CommandHandler{
		&commands.FullHandler{
			Func: fnSyncChats,
			Name: "sync-chats",
			Help: commands.HelpMeta{
				Section:     commands.HelpSectionChats,
				Description: tc.tr("Synchronize your chats", "Синхронизировать чаты"),
				Args:        tc.tr("[_login ID_]", "[_ID входа_]"),
			},
			RequiresLogin: true,
		},
		&commands.FullHandler{
			Func: fnUpgrade,
			Name: "upgrade",
			Help: commands.HelpMeta{
				Section:     commands.HelpSectionChats,
				Description: tc.tr("Upgrade a minigroup to a supergroup on Telegram", "Преобразовать обычную группу в супергруппу Telegram"),
			},
			RequiresPortal: true,
		},
		&commands.FullHandler{
			Func: fnJoin,
			Name: "join",
			Help: commands.HelpMeta{
				Section:     commands.HelpSectionChats,
				Description: tc.tr("Join a Telegram group using an invite link.", "Присоединиться к группе или каналу Telegram по ссылке-приглашению"),
				Args:        tc.tr("[login ID] <invite link>", "[ID входа] <ссылка-приглашение>"),
			},
			RequiresLogin: true,
		},
		&commands.FullHandler{
			Func:    fnEmojiPack,
			Name:    "emoji-pack",
			Aliases: []string{"pack", "sticker-pack", "emojipack", "stickerpack"},
			Help: commands.HelpMeta{
				Section:     commands.HelpSectionMisc,
				Description: tc.tr("Bridge emoji packs between Matrix and Telegram.", "Перенос наборов эмодзи и стикеров между Matrix и Telegram"),
				Args:        tc.tr("<upload/download/list/help> [args...]", "<upload/download/list/help> [аргументы...]"),
			},
			RequiresLogin: true,
		},
	}
}

func fnSyncChats(ce *commands.Event) {
	logins := ce.User.GetUserLogins()
	if len(ce.Args) > 0 {
		logins = slices.DeleteFunc(logins, func(login *bridgev2.UserLogin) bool {
			return !slices.Contains(ce.Args, string(login.ID))
		})
		if len(logins) == 0 {
			ce.Reply(trEvent(ce, "No matching logins found with provided ID(s)", "Не найдено ни одного входа с указанным(и) ID"))
			return
		}
	}
	for _, login := range logins {
		client := login.Client.(*TelegramClient)
		if err := client.syncChats(ce.Ctx, 0, false, true); err != nil {
			ce.Reply(trEvent(ce, "Failed to synchronize chats for %s: %v", "Не удалось синхронизировать чаты для %s: %v"), format.SafeMarkdownCode(login.ID), err)
		} else {
			ce.Reply(trEvent(ce, "Successfully synchronized chats for %s", "Чаты для %s успешно синхронизированы"), format.SafeMarkdownCode(login.ID))
		}
	}
}

func fnUpgrade(ce *commands.Event) {
	login, _, err := ce.Portal.FindPreferredLogin(ce.Ctx, ce.User, false)
	if errors.Is(err, bridgev2.ErrNotLoggedIn) {
		ce.Reply(trEvent(ce, "No logins found to upgrade the chat", "Не найдено активных входов для обновления чата"))
	} else if err != nil {
		ce.Log.Err(err).Msg("Failed to find preferred login for upgrade command")
		ce.Reply(trEvent(ce, "Failed to find a login to upgrade the chat.", "Не удалось найти вход для обновления чата."))
	} else if peerType, chatID, _, err := ids.ParsePortalID(ce.Portal.ID); err != nil {
		ce.Log.Err(err).Str("portal_id", string(ce.Portal.ID)).Msg("Failed to parse portal ID for upgrade command")
		ce.Reply(trEvent(ce, "Failed to parse portal ID", "Не удалось разобрать ID портала"))
	} else if peerType == ids.PeerTypeChannel {
		ce.Reply(trEvent(ce, "Only minigroups can be upgraded (this is already a channel/supergroup)", "Обновить можно только обычную группу (это уже канал или супергруппа)"))
	} else if peerType == ids.PeerTypeUser {
		ce.Reply(trEvent(ce, "Only minigroups can be upgraded (this is direct chat)", "Обновить можно только обычную группу (это личный чат)"))
	} else if resp, err := login.Client.(*TelegramClient).client.API().MessagesMigrateChat(ce.Ctx, chatID); err != nil {
		ce.Log.Err(err).Int64("chat_id", chatID).Msg("Failed to upgrade chat")
		ce.Reply(trEvent(ce, "Failed to upgrade chat: %v", "Не удалось обновить чат: %v"), err)
	} else {
		ce.Log.Trace().Any("response", resp).Msg("Updates from chat upgrade")
		ce.Log.Info().Int64("old_chat_id", chatID).Msg("Successfully upgraded chat")
		ce.React("\u2705\ufe0f")
		err = login.Client.(*TelegramClient).dispatcher.Handle(ce.Ctx, resp)
		if err != nil {
			ce.Log.Err(err).Msg("Failed to handle updates from chat upgrade")
		} else {
			ce.Log.Debug().Msg("Finished handling updates from chat upgrade")
		}
	}
}

var usernameLinkRe = regexp.MustCompile(`^(?:(?:https?://)?t(?:elegram)?\.(?:me|dog)/|tg:/{0,2}resolve\?domain=)([a-zA-Z]\w{3,30}[a-zA-Z\d])(?:\?.+)?$`)
var inviteLinkRe = regexp.MustCompile(`^(?:(?:https?://)?t(?:elegram)?\.(?:me|dog)/(?:joinchat/|\+)|tg:/{0,2}join\?invite=)([a-zA-Z0-9_-]{8,64})(?:\?.+)?$`)

func fnJoin(ce *commands.Event) {
	if len(ce.Args) == 0 || len(ce.Args) > 2 {
		ce.Reply(trEvent(ce, "Usage: `$cmdprefix join [login ID] <invite link>`", "Использование: `$cmdprefix join [ID входа] <ссылка-приглашение>`"))
		return
	}
	var login *bridgev2.UserLogin
	if len(ce.Args) == 2 {
		targetLogin := ce.Bridge.GetCachedUserLoginByID(networkid.UserLoginID(ce.Args[0]))
		if targetLogin == nil || targetLogin.UserMXID != ce.User.MXID {
			ce.Reply(trEvent(ce, "No login found with the provided ID", "Вход с указанным ID не найден"))
			return
		}
		login = targetLogin
		ce.Args = ce.Args[1:]
	} else {
		login = ce.User.GetDefaultLogin()
		if login == nil {
			ce.Reply(trEvent(ce, "You're not logged in", "Вы не авторизованы"))
			return
		}
	}
	t := login.Client.(*TelegramClient)
	var resp tg.MessagesChatInviteJoinResultClass
	var chatName string
	if usernameMatch := usernameLinkRe.FindStringSubmatch(ce.Args[0]); usernameMatch != nil {
		resolve, err := t.client.API().ContactsResolveUsername(ce.Ctx, &tg.ContactsResolveUsernameRequest{Username: usernameMatch[1]})
		if err != nil {
			ce.Log.Err(err).Msg("Failed to resolve username from invite link")
			ce.Reply(trEvent(ce, "Failed to resolve username from invite link: %v", "Не удалось определить чат по ссылке: %v"), err)
			return
		}
		peer, isChannel := resolve.Peer.(*tg.PeerChannel)
		if !isChannel {
			ce.Reply(trEvent(ce, "That username does not belong to a channel or supergroup", "Этот юзернейм не принадлежит каналу или супергруппе"))
			return
		}
		var inputChannel *tg.InputChannel
		for _, chat := range resolve.Chats {
			if chat.GetID() == peer.ChannelID {
				switch typedChat := chat.(type) {
				case *tg.Channel:
					inputChannel = typedChat.AsInput()
					chatName = typedChat.Title
				case *tg.Community:
					chatName = typedChat.Title
					inputChannel = &tg.InputChannel{
						ChannelID:  typedChat.ID,
						AccessHash: typedChat.AccessHash,
					}
				}
			}
		}
		if inputChannel == nil {
			ce.Reply(trEvent(ce, "Channel information not found in resolve response", "Информация о канале не найдена в ответе Telegram"))
			return
		}
		resp, err = t.client.API().ChannelsJoinChannel(ce.Ctx, inputChannel)
		if err != nil {
			ce.Log.Err(err).Msg("Failed to join chat with invite link")
			ce.Reply(trEvent(ce, "Failed to join chat: %v", "Не удалось присоединиться к чату: %v"), err)
			return
		}
	} else if inviteLinkMatch := inviteLinkRe.FindStringSubmatch(ce.Args[0]); inviteLinkMatch != nil {
		resolve, err := t.client.API().MessagesCheckChatInvite(ce.Ctx, inviteLinkMatch[1])
		if tgerr.Is(err, tg.ErrInviteHashInvalid) {
			ce.Reply(trEvent(ce, "Invalid invite link", "Недействительная ссылка-приглашение"))
			return
		} else if tgerr.Is(err, tg.ErrInviteHashExpired) {
			ce.Reply(trEvent(ce, "Invite link expired", "Срок действия ссылки-приглашения истёк"))
			return
		}
		switch typed := resolve.(type) {
		case *tg.ChatInviteAlready:
			titler, ok := typed.Chat.(interface {
				GetTitle() string
			})
			if ok {
				chatName = titler.GetTitle()
			} else {
				chatName = trEvent(ce, "that chat", "этот чат")
			}
			ce.Reply(trEvent(ce, "You're already a member of %s", "Вы уже состоите в %s"), html.EscapeString(chatName))
			return
		case *tg.ChatInvite:
			chatName = typed.Title
		default:
			ce.Log.Warn().Type("resolved_type", typed).Msg("Unexpected response type from MessagesCheckChatInvite")
		}
		resp, err = t.client.API().MessagesImportChatInvite(ce.Ctx, inviteLinkMatch[1])
		if err != nil {
			ce.Log.Err(err).Msg("Failed to join chat with invite link")
			ce.Reply(trEvent(ce, "Failed to join chat: %v", "Не удалось присоединиться к чату: %v"), err)
			return
		}
	} else {
		ce.Reply(trEvent(ce, "Invalid invite link format", "Неверный формат ссылки-приглашения"))
		return
	}
	switch realResp := resp.(type) {
	case *tg.MessagesChatInviteJoinResultOk:
		err := t.dispatcher.Handle(ce.Ctx, realResp.Updates)
		if err != nil {
			ce.Log.Err(err).Msg("Failed to handle updates from joining chat with invite link")
		} else {
			ce.Log.Debug().Msg("Finished handling updates from joining chat with invite link")
		}
		ce.Reply(trEvent(ce, "Successfully joined %s", "Вы успешно присоединились к %s"), html.EscapeString(chatName))
	case *tg.MessagesChatInviteJoinResultWebView:
		ce.Reply(trEvent(ce, "The chat wants you to open a webview to join. Please use the native Telegram app.", "Для вступления в этот чат требуется открыть веб-страницу (WebView). Пожалуйста, воспользуйтесь официальным приложением Telegram."))
	default:
		ce.Reply(trEvent(ce, "Unexpected response for join request", "Неожиданный ответ на запрос вступления"))
	}
}

const emojiPackHelpRu = `Эта команда позволяет переносить наборы эмодзи и стикеров между Matrix и Telegram.

* $cmdprefix emoji-pack upload <telegram shortcode> <ID комнаты> <state key> — перенести набор из Matrix в Telegram.
* $cmdprefix emoji-pack download <короткое имя или ссылка> — перенести набор из Telegram в Matrix.
* $cmdprefix emoji-pack list — показать ваши текущие наборы в Telegram.
* $cmdprefix emoji-pack help — показать эту справку.`

const emojiPackHelp = `This command can be used to transfer emoji packs between Matrix and Telegram.

* $cmdprefix emoji-pack upload <telegram shortcode> <room ID> <state key> - Transfer a pack from Matrix to Telegram.
* $cmdprefix emoji-pack download <pack shortcode or link> - Transfer a pack from Telegram to Matrix.
* $cmdprefix emoji-pack list - List your current emoji packs on Telegram.
* $cmdprefix emoji-pack help - Show this help message.`

func fnEmojiPack(ce *commands.Event) {
	var login *bridgev2.UserLogin
	if len(ce.Args) > 0 {
		targetLogin := ce.Bridge.GetCachedUserLoginByID(networkid.UserLoginID(ce.Args[0]))
		if targetLogin != nil && targetLogin.UserMXID == ce.User.MXID {
			ce.Args = ce.Args[1:]
			login = targetLogin
		}
	}
	var command string
	if len(ce.Args) > 0 {
		command = strings.ToLower(ce.Args[0])
		ce.Args = ce.Args[1:]
	}

	if login == nil {
		login = ce.User.GetDefaultLogin()
		if login == nil {
			ce.Reply(trEvent(ce, "You're not logged in", "Вы не авторизованы"))
			return
		}
	}
	client := login.Client.(*TelegramClient)

	switch command {
	case "help", "":
		ce.Reply(trEvent(ce, emojiPackHelp, emojiPackHelpRu))
	case "list":
		client.fnListEmojiPacks(ce)
	case "upload":
		client.fnUploadEmojiPack(ce)
	case "download":
		client.fnDownloadEmojiPack(ce)
	default:
		ce.Reply(trEvent(ce, "Usage: `$cmdprefix emoji-pack <upload/download/list/help> [args...]`", "Использование: `$cmdprefix emoji-pack <upload/download/list/help> [аргументы...]`"))
	}
}
