-- 0002_add_vault_flag.sql — D-039 版本仓库机制：
-- placements.vault 标记该决策的产出物位于版本仓库（vault/）而非媒体库（library/）。
-- 0=主版本（在 library），1=vault 内版本。

ALTER TABLE placements ADD COLUMN vault INTEGER NOT NULL DEFAULT 0;
