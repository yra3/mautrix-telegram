-- v10 -> v11 (compatible with v2+): Persist chat catalog, chat rejections and the declined panel state

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
    updated_ts    BIGINT  NOT NULL
);
