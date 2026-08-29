-- 0001_initial.sql — roxy 全量初始 schema
-- 设计基准见 docs/ARCHITECTURE.md §12；本文件是 schema 唯一事实源（AGENTS.md）。

CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role          TEXT NOT NULL DEFAULT 'admin',
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);

CREATE TABLE sources (
  id              INTEGER PRIMARY KEY,
  name            TEXT NOT NULL,
  path            TEXT NOT NULL UNIQUE,
  kind            TEXT NOT NULL DEFAULT 'mixed'
                  CHECK (kind IN ('mixed','video','subtitle')),
  provider_type   TEXT NOT NULL DEFAULT 'dirscan',
  provider_config TEXT,
  enabled         INTEGER NOT NULL DEFAULT 1,
  created_at      TEXT NOT NULL
);

CREATE TABLE source_files (
  id           INTEGER PRIMARY KEY,
  source_id    INTEGER NOT NULL REFERENCES sources(id),
  abs_path     TEXT NOT NULL UNIQUE,
  size         INTEGER NOT NULL,
  mtime        TEXT NOT NULL,
  kind         TEXT NOT NULL DEFAULT 'unknown'
               CHECK (kind IN ('unknown','video','subtitle','nfo','image','other')),
  status       TEXT NOT NULL DEFAULT 'new'
               CHECK (status IN ('new','parsing','parsed','placed','ignored','error')),
  parse_result TEXT,
  provenance   TEXT,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);

CREATE TABLE series (
  id               INTEGER PRIMARY KEY,
  bgm_subject_id   INTEGER UNIQUE,
  anilist_id       INTEGER,
  tmdb_id          TEXT,
  imdb_id          TEXT,
  title            TEXT NOT NULL,
  title_original   TEXT,
  year             INTEGER,
  series_type      TEXT NOT NULL DEFAULT 'tv'
                   CHECK (series_type IN ('tv','ova','ona','movie','special','other')),
  parent_series_id INTEGER REFERENCES series(id),
  library_kind     TEXT NOT NULL DEFAULT 'tv' CHECK (library_kind IN ('tv','movie')),
  library_path     TEXT,
  poster_path      TEXT,
  fanart_path      TEXT,
  policy_overrides TEXT,
  status           TEXT NOT NULL DEFAULT 'active'
                   CHECK (status IN ('active','archived')),
  created_at       TEXT NOT NULL,
  updated_at       TEXT NOT NULL
);

CREATE TABLE series_aliases (
  id        INTEGER PRIMARY KEY,
  series_id INTEGER NOT NULL REFERENCES series(id),
  alias     TEXT NOT NULL,
  source    TEXT NOT NULL CHECK (source IN ('api','user','learned')),
  UNIQUE (series_id, alias, source)
);

CREATE TABLE placements (
  id                       INTEGER PRIMARY KEY,
  source_file_id           INTEGER NOT NULL REFERENCES source_files(id),
  series_id                INTEGER NOT NULL REFERENCES series(id),
  slot_type                TEXT NOT NULL
                           CHECK (slot_type IN ('episode','special','movie','op','ed',
                                                'pv','cm','extra','subtitle','ignored')),
  season                   INTEGER,
  episode                  REAL,
  episode_title            TEXT,
  version_key              TEXT,
  version_label            TEXT,
  subtitle_of_placement_id INTEGER REFERENCES placements(id),
  confidence               REAL,
  decision_source          TEXT NOT NULL CHECK (decision_source IN ('rule','llm','human')),
  evidence                 TEXT,
  review_state             TEXT NOT NULL DEFAULT 'proposed'
                           CHECK (review_state IN ('proposed','auto_approved',
                                                   'pending_review','approved',
                                                   'rejected','rework')),
  manual_lock              INTEGER NOT NULL DEFAULT 0,
  created_at               TEXT NOT NULL,
  updated_at               TEXT NOT NULL
);

CREATE TABLE ledger (
  id            INTEGER PRIMARY KEY,
  placement_id  INTEGER REFERENCES placements(id),
  artifact_type TEXT NOT NULL CHECK (artifact_type IN ('symlink','nfo','image','dir')),
  path          TEXT NOT NULL UNIQUE,
  target        TEXT,
  state         TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','removed')),
  created_at    TEXT NOT NULL
);

CREATE TABLE review_cases (
  id           INTEGER PRIMARY KEY,
  placement_id INTEGER NOT NULL REFERENCES placements(id),
  reason       TEXT,
  llm_log_id   INTEGER,
  user_note    TEXT,
  state        TEXT NOT NULL DEFAULT 'open'
               CHECK (state IN ('open','approved','rejected','reworked')),
  created_at   TEXT NOT NULL,
  resolved_at  TEXT
);

CREATE TABLE feedback_notes (
  id          INTEGER PRIMARY KEY,
  scope       TEXT NOT NULL CHECK (scope IN ('global','series','pattern')),
  series_id   INTEGER REFERENCES series(id),
  pattern     TEXT,
  note        TEXT NOT NULL,
  inject_into TEXT NOT NULL DEFAULT 'both' CHECK (inject_into IN ('prompt','rule','both')),
  created_at  TEXT NOT NULL
);

CREATE TABLE llm_logs (
  id          INTEGER PRIMARY KEY,
  task        TEXT NOT NULL,
  provider    TEXT NOT NULL,
  model       TEXT NOT NULL,
  request     TEXT NOT NULL,
  response    TEXT NOT NULL,
  tokens_in   INTEGER,
  tokens_out  INTEGER,
  duration_ms INTEGER,
  created_at  TEXT NOT NULL
);

CREATE TABLE search_cache (
  id         INTEGER PRIMARY KEY,
  query      TEXT NOT NULL,
  source     TEXT NOT NULL,
  result     TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE TABLE tasks (
  id          INTEGER PRIMARY KEY,
  kind        TEXT NOT NULL CHECK (kind IN ('scan','match','materialize','rework',
                                            'reconcile','index_refresh')),
  payload     TEXT,
  state       TEXT NOT NULL DEFAULT 'queued'
              CHECK (state IN ('queued','running','done','failed','cancelled')),
  progress    TEXT,
  error       TEXT,
  created_at  TEXT NOT NULL,
  started_at  TEXT,
  finished_at TEXT
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE bgm_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE bgm_subjects (
  id       INTEGER PRIMARY KEY,
  type     INTEGER NOT NULL,
  name     TEXT NOT NULL,
  name_cn  TEXT,
  platform TEXT,
  date     TEXT,
  score    REAL,
  rank     INTEGER,
  nsfw     INTEGER NOT NULL DEFAULT 0,
  summary  TEXT
);

CREATE TABLE bgm_episodes (
  id         INTEGER PRIMARY KEY,
  subject_id INTEGER NOT NULL,
  name       TEXT,
  name_cn    TEXT,
  sort       REAL,
  ep_type    INTEGER NOT NULL,
  airdate    TEXT
);
CREATE INDEX idx_bgm_episodes_subject ON bgm_episodes(subject_id, ep_type, sort);

CREATE TABLE bgm_relations (
  subject_id         INTEGER NOT NULL,
  related_subject_id INTEGER NOT NULL,
  relation_type      TEXT NOT NULL
);
CREATE INDEX idx_bgm_relations ON bgm_relations(subject_id);

CREATE VIRTUAL TABLE bgm_subjects_fts USING fts5(
  name, name_cn, content='bgm_subjects', content_rowid='id'
);

CREATE TRIGGER bgm_subjects_ai AFTER INSERT ON bgm_subjects BEGIN
  INSERT INTO bgm_subjects_fts(rowid, name, name_cn)
  VALUES (new.id, new.name, new.name_cn);
END;

CREATE TRIGGER bgm_subjects_ad AFTER DELETE ON bgm_subjects BEGIN
  INSERT INTO bgm_subjects_fts(bgm_subjects_fts, rowid, name, name_cn)
  VALUES ('delete', old.id, old.name, old.name_cn);
END;

CREATE TRIGGER bgm_subjects_au AFTER UPDATE ON bgm_subjects BEGIN
  INSERT INTO bgm_subjects_fts(bgm_subjects_fts, rowid, name, name_cn)
  VALUES ('delete', old.id, old.name, old.name_cn);
  INSERT INTO bgm_subjects_fts(rowid, name, name_cn)
  VALUES (new.id, new.name, new.name_cn);
END;
