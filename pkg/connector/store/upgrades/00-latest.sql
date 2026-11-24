-- v0 -> v11 (compatible with v2+): Latest revision

CREATE TABLE telegram_user_state (
    user_id BIGINT NOT NULL PRIMARY KEY,
    pts     BIGINT NOT NULL,
    qts     BIGINT NOT NULL,
    date    BIGINT NOT NULL,
    seq     BIGINT NOT NULL
);

CREATE TABLE telegram_channel_state (
    user_id    BIGINT NOT NULL,
    channel_id BIGINT NOT NULL,
    pts        BIGINT NOT NULL,

    PRIMARY KEY (user_id, channel_id)
);

CREATE TABLE telegram_access_hash (
    user_id     BIGINT NOT NULL,
    entity_type TEXT   NOT NULL,
    entity_id   BIGINT NOT NULL,
    access_hash BIGINT NOT NULL,

    PRIMARY KEY (user_id, entity_type, entity_id)
);

CREATE TABLE telegram_username (
    username    TEXT   NOT NULL,
    entity_type TEXT   NOT NULL,
    entity_id   BIGINT NOT NULL,

    PRIMARY KEY (username)
);

CREATE INDEX telegram_username_entity_idx ON telegram_username (entity_id);
CREATE INDEX telegram_username_username_idx ON telegram_username (LOWER(username));

CREATE TABLE telegram_phone_number (
    phone_number  TEXT   NOT NULL,
    entity_id     BIGINT NOT NULL,

    PRIMARY KEY (phone_number)
);

CREATE INDEX telegram_phone_number_entity_idx ON telegram_phone_number (entity_id);

CREATE TABLE telegram_file (
    id        TEXT PRIMARY KEY,
    mxc       TEXT NOT NULL,
    mime_type TEXT,
    size      BIGINT,
    width     INTEGER,
    height    INTEGER,
    timestamp BIGINT
);

CREATE INDEX telegram_file_mxc_idx ON telegram_file (mxc);

CREATE TABLE telegram_topic (
    channel_id BIGINT NOT NULL,
    topic_id   BIGINT NOT NULL,

    PRIMARY KEY (channel_id, topic_id)
);

CREATE TABLE chat_catalog (
    user_id          BIGINT  NOT NULL,
    portal_id        TEXT    NOT NULL,
    kind             TEXT    NOT NULL DEFAULT '',
    title            TEXT    NOT NULL DEFAULT '',
    about            TEXT    NOT NULL DEFAULT '',
    username         TEXT    NOT NULL DEFAULT '',
    participants     INTEGER NOT NULL DEFAULT 0,
    is_forum         INTEGER NOT NULL DEFAULT 0,
    is_broadcast     INTEGER NOT NULL DEFAULT 0,
    in_archive       INTEGER NOT NULL DEFAULT 0,
    meta_fetched_ts  INTEGER NOT NULL DEFAULT 0,
    about_fetched_ts INTEGER NOT NULL DEFAULT 0,
    search_blob      TEXT    NOT NULL DEFAULT '',

    PRIMARY KEY (user_id, portal_id)
);

CREATE INDEX chat_catalog_user_idx ON chat_catalog (user_id);

CREATE TABLE chat_rejection (
    user_id        BIGINT  NOT NULL,
    id             TEXT    NOT NULL,
    source         TEXT    NOT NULL,
    reason         TEXT    NOT NULL,
    title          TEXT    NOT NULL DEFAULT '',
    about          TEXT    NOT NULL DEFAULT '',
    participants   INTEGER NOT NULL DEFAULT 0,
    first_seen_ts  BIGINT  NOT NULL,
    last_seen_ts   BIGINT  NOT NULL,
    has_mxid       INTEGER NOT NULL DEFAULT 0,

    PRIMARY KEY (user_id, id, source)
);

CREATE INDEX chat_rejection_user_idx ON chat_rejection (user_id, last_seen_ts);

CREATE TABLE declined_panel (
    user_mxid     TEXT    NOT NULL PRIMARY KEY,
    room_id       TEXT    NOT NULL,
    event_id      TEXT    NOT NULL,
    page          INTEGER NOT NULL DEFAULT 0,
    filter_query  TEXT    NOT NULL DEFAULT '',
    sort          TEXT    NOT NULL DEFAULT 'recent',
    generation    INTEGER NOT NULL DEFAULT 0,
    created_ts    BIGINT  NOT NULL,
    updated_ts    BIGINT  NOT NULL,
    layout        TEXT    NOT NULL DEFAULT ''
);
