ALTER TABLE studio_items ADD COLUMN pinned boolean NOT NULL DEFAULT false;
ALTER TABLE studio_items ADD COLUMN archived boolean NOT NULL DEFAULT false;
ALTER TABLE studio_items ADD CONSTRAINT studio_items_session_flags
  CHECK ((NOT pinned AND NOT archived) OR kind = 'execution');
