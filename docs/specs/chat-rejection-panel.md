# Спецификация: персистентные отклонённые чаты + интерактивная панель (форк mautrix-telegram)




Файл: `docs/specs/chat-rejection-panel.md` в `~/yura/projects/mautrix-telegram-fork` (minis).
Спека — основной документ. Эта страница — как её читать и с чего начать.

## Порядок работы

1. Прочитать спеку целиком (§0 — проверенные факты со ссылками на строки;
   §1 — схема; §2–§5 — функциональность; §6 — порядок коммитов; §7 — решения
   пользователя; §8 — риски).
2. Каждый коммит = один этап §6. После этапов 1–3 фича уже работает без UI
   (команды `!tg declined` и `!s`), это нормальная промежуточная точка.
3. Компиляция: в minis нет `go` в PATH — собирать через Docker или
   `go tool maubuild` (см. `build.sh`, `Dockerfile.ci`). Тесты юнитов —
   в Docker, не на хосте.
4. Деплой — только через CI (§6 этап 6). Никогда не деплоить ручным
   копированием бинаря. Простой ~10 с на lv-vps.

## Быстрые ссылки (не переоткрывать заново)

- Форк: `~/yura/projects/mautrix-telegram-fork` (minis). Мавтрикс-модуль
  локально: `/tmp/mtxchk/maunium.net/go/mautrix@...` — если `/tmp`
  перезагрузили, перескачать zip с proxy.golang.org (указание в §0).
- Точки `RejectPeer`: `pkg/connector/chatinfo.go:613-739` (12 вызовов).
- Кэш отклонений: `pkg/connector/client.go:845-880`.
- Игнор сообщений: `pkg/connector/handletelegram.go:271-282`.
- Регистрация команд: `pkg/connector/connector.go:46`, `buildCommands` —
  `pkg/connector/commands.go:36`.
- Миграции: `pkg/connector/store/upgrades/` → новый файл `11-chat-catalog.sql`,
  сам подхватится через `//go:embed *.sql` (проверить, что 09/10-`.go`
  апгрейды не конфликтуют с номерами).
- `PostInit` для хука реакций: `cmd/mautrix-telegram/main.go:46`.
- Мавтрикс-утилиты: `EventProcessor.PrependHandler`, `IntentAPI.RedactEvent`,
  `SendMessageEvent` (c `m.replace`) — см. §0.
- БД на lv-vps: read-only только, из python3 хоста (в контейнере нет
  python3).
- Деплой: `ghcr.io/yra3/mautrix-telegram:latest`, lv-vps
  `/yura/projects/matrix-bridges` — `docker compose pull && up -d`.

## Проверки перед PR

- [ ] `go build ./...` чистый (через build.sh/Docker).
- [ ] Новые юниты: нормализация search_blob (ё/регистр/кириллица),
      ранжирование, пагинация 5/страницу, stale-generation,
      restore manual-строк при рестарте, отсутствие конфликта алиасов 1–99.
- [ ] Локи: 0 `ERR`-логов по отклонённым чатам за час работы
      (INFO с rate-limit 60с на peer — проверить, что не зальёт лог).
- [ ] E2E: 20+ действий в панели (эмодзи и цифрами), в management-DM только
      одно живое панель-сообщение; цифры стираются; redact работает.

---


Репозиторий: `~/yura/projects/mautrix-telegram-fork` на minis (SSH: `ssh -n minis`).
Модуль `go.mau.fi/mautrix-telegram`, Go 1.26+. Бранч `main`. Деплой только через CI (см. §9).

## 0. Контекст и проверенные факты

Почему всё так: каждый факт ниже проверен по исходникам (форк на minis + mautrix в `/tmp/mtxchk`
на minis, версия mautrix из `go.mod:43` = `maunium.net/go/mautrix@v0.32.1-0.20261030002103-225629560b60`).

**Текущее поведение отклонений (in-memory, теряется при рестарте):**
- `pkg/connector/client.go:845-880` — `RejectPeer(portalID, reason)` кладёт в
  `tc.rejectedPeers map[networkid.PortalID]rejectionInfo` c TTL 12ч (`rejectionTTL`),
  `UnrejectPeer` удаляет, `GetPeerRejection` читает. Ключ — **base** portal id
  (`ids.PeerToBasePortalID`, без темы: `channel:1829532653:3` → `channel:1829532653`).
- Точки отклонения — `pkg/connector/chatinfo.go` (строк ~613-739):
  `tc.RejectPeer(basePortalID, err.Error())` при `bot chat bridging is disabled`,
  `not archived` / `too many participants (N) in channel/forum N` и др.
- Авто-отклонение при создании портала: `GetChatInfo` бросает
  `too many participants (%d) in chat/channel/forum %d` (`chatinfo.go:207,481,679,700`).
  `GetChatInfo` обёртка (`chatinfo.go:582`) пробрасывает ошибку; вызывается mautrix'ом
  при создании портала.
- Игнор сообщений от отклонённых: `pkg/connector/handletelegram.go:271-282`
  — `GetPeerRejection` → `log.Debug()...Msg("Ignoring message for rejected peer (12h TTL)")`.
- Конфиг: `network.max_member_count = 20` (на lv-vps строка 100 config.yaml).
  Лимит проверяется в `config.go:115` (`c.MaxMemberCount < 0 || count <= c.MaxMemberCount`).

**Команды (маутриксовый command processor):**
- Регистрация команд форка: `pkg/connector/connector.go:46` →
  `proc.AddHandlers(tc.buildCommands()...)`, `buildCommands` — `pkg/connector/commands.go:36`.
  Команды — `*commands.FullHandler{Name, Aliases, Func, Help, RequiresLogin...}`.
- `Processor.AddHandler` кладёт хендлер в мапу по `GetName()` **без валидации имени**
  (маутрикс `bridgev2/commands/processor.go:64`) — имена `"/s"` и цифры `"1".."99"` легальны.
- В management-DM **любое** текстовое сообщение = команда:
  `mautrix bridgev2/queue.go:108` — `strings.HasPrefix(msg.Body, CommandPrefix) ||
  evt.RoomID == sender.ManagementRoom`. Неизвестная команда → `Unknown command, use the
  help command` (если у юзера нет `CommandState.Next`).
- Под-команды `!tg declined ...` доставаются через `ce.RawArgs` / `ce.Args` как в
  upstream `managechat.go`.

**Реакции и панели:**
- `EventProcessor.PrependHandler(event.Type, handler)` существует
  (`appservice/eventprocessor.go:73`); `EventHandler = func(ctx context.Context, evt *event.Event)`.
  `mxmain.Init()` вызывает `br.PostInit()` в самом конце, после создания
  `m.Matrix.EventProcessor` и `m.Matrix.Bridge` (`bridgev2/matrix/mxmain/main.go:271`).
  В форке `PostInit` уже задан в `cmd/mautrix-telegram/main.go:46` (заполнение AppVersion).
  Места для подвешивания своего обработчика реакций — туда же, в существующий `PostInit`.
- Встроенный обработчик реакций (`handleRoomEvent` → queue.go) в management-DM реакции
  **отбрасывает** (queue.go:126 → `EventHandlingResultSuccess`, бездействие) — поэтому наш
  Prepend-хендлер работает «до» и не конфликтует.
- Management-комната создаётся ботом: бот PL 9001, пользователь PL 50
  (`bridgev2/user.go:216-224`) → бот может redact-ить реакции пользователя
  (`bot.RedactEvent(ctx, roomID, eventID)` — `mautrix client.go:1638` /
  `IntentAPI.RedactEvent` `appservice/intent.go:467`).
- Редактирование собственного сообщения: `SendMessageEvent` c `m.new_content` +
  `m.relates_to{rel_type: m.replace, event_id: <старое>}` — `mautrix client.go:1483`.
- Контент реакции в mautrix: `event.ReactionEventContent` (есть `RelatesTo RelatesTo`;
  body-поле уточнить в исходниках — обычно `Body string`).

**Каталог чатов (для поиска):**
- Диалоги тянутся в `pkg/connector/chatsync.go` (`syncChats`, с 45-й строки):
  `MessagesGetDialogs` + `GetChatInfo`/`wrapChatInfo` для каждого диалога (стр. ~360-395).
  Это место для записи `chat_catalog`.
- `about` (описание) есть только у каналов/супергрупп и требует `ChannelsGetFullChannel`
  — **ленивая** подгрузка (решение пользователя: без полного обхода всех чатов).

**База:** миграции форка в `pkg/connector/store/upgrades/` (00-latest.sql + 02..10,
`.sql` подключаются через `//go:embed *.sql` + `dbutil.BuildUpgradeTable().WithFS` в
`upgrades.go`). Следующий номер — `11`.
## 1. Схема БД (миграция `pkg/connector/store/upgrades/11-chat-catalog.sql`)

Все таблицы — в БД конкретного logina (у mautrix-telegram store изолирован на
login), так что колонки `login_id` внутри таблиц не нужны (в отличие от
черновика спеки). Ключи остаются строки вида `channel:2463355752` /
`chat:123` / `user:789` (те самые portal id без темы, base form).

```sql
-- Каталог всех известных чатов данного логина. Наполняется из syncChats
-- (диалоги) + UpdateChannel/UpdateChat-апдейтов. Для дешёвого
-- регистронезависимого поиска — нормализованная колонка search_blob.
CREATE TABLE IF NOT EXISTS chat_catalog (
    portal_id      TEXT NOT NULL PRIMARY KEY,  -- base portal id: 'channel:2463355752'
    kind           TEXT NOT NULL DEFAULT '',    -- 'user' | 'group' | 'channel' | 'forum'
    title          TEXT NOT NULL DEFAULT '',
    about           TEXT NOT NULL DEFAULT '',   -- описания каналов/супергрупп; '' = не загружено
    username        TEXT NOT NULL DEFAULT '',   -- '@...' или ''
    participants   INTEGER NOT NULL DEFAULT 0,
    is_forum       INTEGER NOT NULL DEFAULT 0,
    is_broadcast   INTEGER NOT NULL DEFAULT 0,  -- канал (broadcast)
    in_archive     INTEGER NOT NULL DEFAULT 0,
    meta_fetched_ts INTEGER NOT NULL DEFAULT 0, -- unix, когда title/username/participants обновлялись
    about_fetched_ts INTEGER NOT NULL DEFAULT 0,-- unix; 0 = about не запрашивался
    search_blob    TEXT NOT NULL DEFAULT '',    -- lower+ё→е+схлопанные пробелы, из title/username/about/id/participants
    -- индекс по search_blob не нужен (маленькие таблицы, поиск строковый в Go, см. §3)
);

-- Персистентное отклонение чата. Единая сущность с двумя источниками:
--  source='auto'  — вписывается на каждое авто-отклонение (upsert, last_seen растёт)
--  source='manual'— вписывается командой `!tg declined add`; живёт до `remove`/`purge`
CREATE TABLE IF NOT EXISTS chat_rejection (
    id             TEXT NOT NULL,
    source         TEXT NOT NULL,              -- 'auto' | 'manual'
    reason         TEXT NOT NULL,              -- человекочитаемая причина
    title          TEXT NOT NULL DEFAULT '',   -- снапшот на момент отклонения
    about          TEXT NOT NULL DEFAULT '',
    participants   INTEGER NOT NULL DEFAULT 0,
    first_seen_ts  INTEGER NOT NULL,           -- unix
    last_seen_ts   INTEGER NOT NULL,
    has_mxid       INTEGER NOT NULL DEFAULT 0, -- был ли созданный MX-портал (снапшот)
    PRIMARY KEY (id, source)
);

-- Состояние интерактивной панели. Одна строка на (user, room):
-- только последняя панель управляет; старые панели в истории inert.
CREATE TABLE IF NOT EXISTS declined_panel (
    user_mxid     TEXT NOT NULL PRIMARY KEY,
    room_id       TEXT NOT NULL,               -- management room
    event_id      TEXT NOT NULL,               -- текущее панель-сообщение
    page          INTEGER NOT NULL DEFAULT 0,
    filter_query  TEXT NOT NULL DEFAULT '',    -- режим поиска (пусто = обычный список)
    sort          TEXT NOT NULL DEFAULT 'recent',-- 'recent' | 'size' | 'alpha'
    generation    INTEGER NOT NULL DEFAULT 0,  -- ++ при каждой перерисовке; защита от stale-реакций
    created_ts    INTEGER NOT NULL,
    updated_ts    INTEGER NOT NULL
);
```

Store-слой (паттерн — смотреть в `pkg/connector/store/` рядом с `topic.go`):
- `pkg/connector/store/chatcatalog.go` — `ChatCatalogRow`, `UpsertChatCatalog(ctx, row)`,
  `ListChatCatalog(ctx, filter string, sort string, limit, offset) ([]ChatCatalogRow, int)`,
  `FetchAbout(ctx, portalID)` (ленивый `ChannelsGetFullChannel`, пишет `about`+`search_blob`,
  idempotent по `about_fetched_ts`), `SearchCatalog(ctx, q, limit)` (выборка всех строк,
  ранжирование в Go — таблица маленькая, десятки-сотни тысяч верхний предел,
  `strings.Contains` по `search_blob` быстрее SQL `LIKE` и без проблем с Unicode).
- `pkg/connector/store/chatrejection.go` — `UpsertRejection(ctx, row, source)`,
  `ListRejections(ctx, keyword, sort, limit, offset)` (join с `chat_catalog` для title
  fallback; legacy-строки без каталога помечаются «title не получен»),
  `GetRejection(ctx, id)`, `DeleteRejection(ctx, id, source)`, `PurgeRejections(ctx)`,
  `ListAutoRejected(ctx, limit, offset)`.
- `pkg/connector/store/panelstate.go` — `UpsertPanel`, `GetPanel(user)`,
  `IncrementGeneration(user)`.
- Миграция регистрируется в `upgrades.go` через `.WithFS(rawUpgrades)`
  автоматически (embed `*.sql`); если нужно что-то после (например backfill
  `search_blob` из существующих строк) — отдельный `.go` апгрейд как в
  `09-legacy-migrate-fix.go`.

Писатели (этап 2, все точки — уже найдены):
1. **Авто-отклонение** — в каждой точке `tc.RejectPeer(basePortalID, ...)`
   (`chatinfo.go` ~613-739) перед/после вызова добавить `tc.main.Store.UpsertRejection(...)`
   c `source='auto'`, `reason`, снапшотом title/participants из доступного
   `Channel`/`ChannelFull`/`UserFull`. Не дублировать вызовы: лучше переделать
   `RejectPeer` так, чтобы он сам брал снапшот из контекста — но тогда сигнатура
   меняется; проще: вынести в `pkg/connector/panel/reject.go` хелпер
   `recordAutoRejection(ctx, tc, basePortalID, reason, info *bridgev2.ChatInfo | tg entity)`.
2. **Рестарт** — при запуске логина: `SELECT ... FROM chat_rejection WHERE source='manual'`
   → занести в `tc.rejectedPeers` (с `rejectionInfo{reason, expiry: очень далеко}` или
   отдельным флагом `manual: true` — manual не истекает; `GetPeerRejection` уже умеет
   TTL, у `rejectionInfo` добавить поле `manual bool`).
3. **Удаление ручного** — `!tg declined remove` → `UnrejectPeer` + `DELETE FROM
   chat_rejection WHERE source='manual'`.
4. **`syncChats`** (`chatsync.go`) — после успешного получения chat info диалога:
   `UpsertChatCatalog`. На `UpdateChannel`/`UpdateChat` (там где форк уже обновляет
   порталы) — тоже `UpsertChatCatalog` (дёшево, без API-запросов).
5. **about лениво** — `FetchAbout` вызывается: (а) при `!tg declined add <id>` без title,
   (б) при рендере панели для строк, у которых `about_fetched_ts=0` и чат
   «визуально интересен» (в списке) — фоновая гораoutine c дедупликацией, не блокируя
   рендер; (в) `!s --refresh <id>`.
## 2. Интерактивная панель (новый пакет `pkg/connector/panel/`)

Одна живая панель-сообщение в management-DM. Два способа ввода (решено пользователем:
реализовать оба сразу, общий движок):
- **эмодзи-реакции**: 1️⃣–5️⃣ = выбор строки, ⬅️/➡️ = страницы, 🔍 = войти в режим
  поиска (подсказка: `!s ...`), 🔄 = обновить, 🔀 = циклически менять сортировку.
  Бот сам сидит свои реакции-«кнопки» (решение: да). После обработки действия бот
  **удаляет (redact)** пользовательскую реакцию и свои кнопки, затем
  переписывает панель (см. ниже).
- **цифрой в чате**: в management-DM пользователь пишет `7` (любая 1–99) → бот
  обрабатывает как выбор строки 7 текущей страницы и **стирает своё сообщение с
  цифрой** (redact). Голые числа в порталах командами не являются — только в
  management-DM (там и без префикса всё = команда, см. §0).

### Рендер и переписывание

- Панель = одно `m.room.message`, переписываемое через `m.replace`
  (`SendMessageEvent` c `m.new_content` + `m.relates_to{rel_type:"m.replace"}`).
  Element показывает «(изменено)», ленту не мусорит.
- Формат страницы (5 строк на страницу — решение пользователя; 1️⃣–5️⃣ это максимум
  под телефон):

```
Отклонённые чаты — @yura (5938699949)
Стр. 1/4 · фильтр: «семья» · 18 найдено · сортировка: по дате

1. «Семья» · channel:2463355752 · 23 уч.
   too many participants · 22.11 12:47 · комнаты нет
2. «Рабочий форум» · channel:1829532653:3 · 812 уч.
   в архиве · 19.11 08:12 · комната !abc:chat.ruisenior.name
...
⬅️ Стрелки/цифры — страницы · 1️⃣–5️⃣ — действие над строкой
🔍 поиск: отправь !s <запрос> · 🔄 обновить · 🔀 сортировка
```

- После каждого действия: (1) redact пользовательских реакций на панели, (2)
  пересидить свои реакции-кнопки, (3) `m.replace`-переписать контент, (4)
  `declined_panel.generation++` + сохранение новой страницы/фильтра.
- **Stale-защита**: в реакции, которую обрабатываем, сравниваем id панели
  (наш `event_id` из `declined_panel`) и, если пользователь тапнул по старому
  сообщению (неактуальное event_id) — отвечаем в панели: «эта панель устарела,
  актуальная ниже» (без переписывания).
- Если панель отсутствует / user не владелец: реакция игнорируется.

### Семантика действий над строкой (без подтверждений — решение пользователя)

Один тап по номеру строки (эмодзи или цифрой) = **toggle**:
- отклонённый чат без MX-комнаты → снять отклонение
  (`UnrejectPeer` + `chat_rejection.remove manual`; если `source='auto'` тоже
  убрать — авто-запись при следующем отклонении обновится сама) +
  `UnrejectPeer`, после чего `!tg create-portal <id>` не нужно — портал будет
  создан при следующем сообщении в чате (если лимит позволяет). Ответ: «Чат X
  больше не отклонён».
- отклонённый чат **с MX-комнатой** (has_mxid=1) → действие уровня
  **archive**: `TagRoom(mxid, m.lowpriority, true)` (отметка «неактивно» в
  Matrix, обратимо) — не `!tg unbridge` (он `Bot.DeleteRoom(..., true)` =
  удаление комнаты с историей, `managechat.go:160-170`; НЕ использовать).
- активный чат (в режиме поиска, не отклонён) → отклонить:
  `RejectPeer` + `UpsertRejection(source='manual')`.
- `!tg declined leave-tg <id>` — отдельная команда (не из панели):
  `ChannelsLeaveChannel`; необратимо, в панели не выносится (только текстовая
  команда с id — решение «без подтверждений» касалось тапов в панели;
  leave-tg оставляем осознанно-вводом-id).

### Подвешивание обработчика реакций

В `cmd/mautrix-telegram/main.go`, в существующий `PostInit` (main.go:46), после
`m.Run()`-инициализации:
```go
m.PostInit = func() {
    ... (текущее содержимое)
    if c.Matrix != nil && c.Matrix.Bridge != nil {
        c.Matrix.EventProcessor.PrependHandler(event.EventReaction, c.HandlePanelReaction)
    }
}
```
(поля `Matrix`/`EventProcessor` — `bridgev2/matrix/connector.go:61-91` и
`mxmain/main.go:149-154`; `PrependHandler` — `appservice/eventprocessor.go:73`.)

`HandlePanelReaction(ctx, evt *event.Event)` (в `pkg/connector/panel/reactions.go`):
- `evt.RoomID != user.ManagementRoom` → return;
- `evt.Sender != user.MXID` → return (только реакции владельца, не бота);
- контент = `evt.Content.AsReaction()` (полю `Body` — ключ эмодзи);
- маппинг: «1️⃣»..«5️⃣»→строка 1..5; «⬅️»←страница назад; «➡️»→вперёд;
  «🔍»→подсказка-поиск (переписать панель в режиме «открой !s...» или сразу
  пустой поиск); «🔄»→re-render; «🔀»→цикл сортировки recent→size→alpha;
  любое другое эмодзи на нашей панели → redact + «не понимаю».
- Цифры в чате: в `buildCommands` добавить FullHandler'ы для имён `"1"`–`"99"`
  (имена валидации нет, см. §0; 99 хендлеров — безболезненно; либо один хендлер
  c `Name: "1"` + алиасы — алиасы тоже идут через `proc.aliases`, `processor.go:69-71`,
  так что можно `Aliases: []string{"2","3",...,"99"}` — проверить компиляцию
  и поведение при старте). `Func`: читает `ce.RawArgs` (пусто) и `ce.Command`
  (сама цифра) → `c.panel.SelectRow(user, roomID, strconv.Atoi(ce.Command))`,
  затем `ce.Bot.RedactEvent(ctx, roomID, ce.EventID)` (стирает сообщение с цифрой)
  и `ce.MessageStatus.DisableMSS = true` (как в `unbridge`, чтобы MSS не
  стучался).

Кнопки-реакции бота: `c.Matrix.Bot` — бот `appservice.IntentAPI` (connector.go:63);
send reaction через `SendMessageEvent(event.EventReaction, ReactionEventContent{...})`
(`client.go:1483`), redact через `IntentAPI.RedactEvent` (intent.go:467).

## 3. Поиск `/s`, `!s`, `!tg s`

Регистрируются как имена хендлеров `"/s"`, `"!s"`, плюс под-команда
`"s"` (через `!tg s` и в management-DM просто `s`? — нет: `s` без префикса в
DM уже команда; ок, имя `"s"` тоже регистрируем, алиасы). Поиск по
`chat_catalog.search_blob` + `chat_rejection` (причина), регистронезависимо,
`ё→е`. Нормализация — в Go при записи в `search_blob`
(`strings.ToLower` + `strings.ReplaceAll(s,"ё","е")` + схлопывание пробелов),
поиск — `strings.Contains` по выборке.

Ищет по: названию, описанию (about), @username, числовому id, числу
участников, причине отклонения. Ранжирование: точный id → префикс username →
title-префикс → вхождение title → вхождение about → вхождение reason →
вхождения в id/participants. Результат = та же панель в режиме поиска
(`declined_panel.filter_query` = запрос). Вывод — те же 5 строк/страница.

Сортировки панели: `recent` (last_seen desc, дефолт), `size`
(participants desc), `alpha` (title asc, collate в Go `sort.Slice`).

`!s --refresh <id>` — принудительно `FetchAbout` + обновить строку каталога.

## 4. Команды (`!tg declined`, без подтверждений)

`!tg declined [list] [keyword] [-n N]` — текстовый список (Markdown-индексы —
улучшение из прошлой итерации спеки остаётся), каждая строка:
`N. title · id · M уч. · причина · дата · [MX-комната/нет]`. Голые
`channel:2463355752` и `2463355752` авто-дополняются в `channel:`.
- `!tg declined add <id|keyword>` — manual-отклонение (создаёт
  `chat_rejection source='manual'`, ставит `UnrejectPeer`-инверсию — `RejectPeer`),
  если id не в каталоге — один разовый `ChannelsGetFullChannel`/
  `MessagesGetFullChat` за снапшотом title/about (не hot-путь).
- `!tg declined remove <id|keyword>` — снять отклонение (и в БД, и в кэше).
- `!tg declined purge` — удалить все manual-строки (auto остаются).
- `!tg declined archive <id>` — TagRoom m.lowpriority у MX-комнаты (если есть).
- `!tg declined leave-tg <id>` — `ChannelsLeaveChannel` (необратимо).
- `!tg declined panel` — открыть/перерисовать интерактивную панель в DM.

Всё новое — `Section: commands.HelpSectionMisc` (или Chats, как у соседей),
i18n-ключи — в `pkg/connector/i18n.go` (паттерн существующих команд), ru/en.

## 5. Логи

- Авто-отклонение и «ignoring message for rejected peer» — `Info` (сейчас
  `Debug` на handletelegram.go:279; точки `RejectPeer` в chatinfo.go —
  контекст `Info` при записи в БД, `Debug` при кэш-попадании). Ретраит:
  rate-limit по peer — не логировать повторно для того же peer чаще 1 раза
  в 60 с (map lastInfoTS, не в БД).
- Отдельно: mautrix-уровневая «Failed to get chat info ... from chat resync
  event» (portal.go:2741) — это уже на стороне mautrix; форк не правит mautrix,
  поэтому наша Info-запись идёт в форке ДО бросания ошибки (писатель
  отклонения), и в логе будет видно причину. Ошибки создания портала, которые
  НЕ авто-отклонение (FloodWait, 403), остаются `Err`.

## 6. Этапы (порядок коммитов, каждый самодостаточен)

1. **Данные**: `11-chat-catalog.sql` + store-слой (`chatcatalog.go`,
   `chatrejection.go`, `panelstate.go`) + запись каталога в `syncChats`/
   `UpdateChannel` + нормализация `search_blob`. После этапа: БД готова.
2. **Писатели + INFO-логи**: все точки `RejectPeer` пишут `chat_rejection`
   c снапшотом; restore manual-строк при старте; логуровни §5.
3. **Текстовые команды**: `!tg declined ...` + `!s`/`/s`/`s` + i18n. После
   этапа: всё работает без эмодзи (fallback).
4. **Панель**: рендер/replace/сидинг/generation/redact, `PostInit`-хук
   реакций, цифра-хендлеры с автостиранием.
5. **Тесты**: юнит на нормализацию поиска (регистр/ё/кириллица/латиница),
   ранжирование, пагинация, сопоставление reaction→строка incl. stale
   generation, restore manual-строк; в Docker, как в skill'е. E2E руками:
   панель в DM, 20+ тапов (эмодзи и цифрами), 0 лишних сообщений в комнате
   кроме панели, redact цифр.
6. **CI-деплой**: push main → сборка `ghcr.io/yra3/mautrix-telegram:latest`
   (workflow `.github/workflows/docker-publish.yml` форка) → на lv-vps:
   `cd /yura/projects/matrix-bridges && docker compose pull && docker compose
   up -d mautrix-telegram` (простой ~10 с). БД на lv-vps читать read-only:
   `python3 -c "import sqlite3; con=sqlite3.connect('file:/yura/projects/matrix-bridges/telegram/mautrix-telegram.db?mode=ro,uri=True')..."`
   (в контейнере mautrix-telegram нет python3 — `docker exec ... python3`
   упадёт, не делать этого).

## 7. Явные исключения (решения пользователя)

- Без подтверждений на действия панели (toggle одной кнопкой).
- `about` лениво, полный обход всех чатов — не делается.
- Бот сидит реакции-кнопки.
- 5 строк на страницу.
- Оба ввода сразу (эмодзи + цифры).
- `!tg unbridge`/`DeleteRoom` для «убрать чат» НЕ использовать — только
  m.lowpriority-архив (обратимо).

## 8. Риски / оговорки

- `/s` в Element: клиентские слэш-команды, не-встроенные, Element может
  ругнуться «неизвестная команда» — рекламируем `!s`, `/s` бонус.
- Алиасы-цифры: `AddHandler` кладёт алиасы в одну мапу (processor.go:69-71) —
  конфликт с существующими именами команд маловероятен, проверить в тестах
  (нет команд-имён "1".."99").
- PrependHandler-хендлеры в Sync-режиме (у нас `ExecMode = Sync` когда
  `!AsyncTransactions`, connector.go:149-151) выполняются в транзакционном
  канале AS — не блокировать: все тяжёлые операции панели — в гораoutines
  (запуск + возврат).
- Мультилогин: записи `chat_rejection`/каталог в store логина (per-login БД
  mautrix-telegram — проверить, что store действительно per-login:
  `pkg/connector/store/scoped_store.go`). Синк между logins не нужен (решение).
- MSC4139 (m.prompts, бот-кнопки) — статус: без клиентской поддержки (на 2026
  клиенты не рендерят). Дорожная карта: когда пройдёт — заменить реакции на
  m.prompts, движок панели не меняется. Зафиксировать в README панели.
