-- v11 -> v12: remember which chat every panel row button points at

ALTER TABLE declined_panel ADD COLUMN layout TEXT NOT NULL DEFAULT '';
