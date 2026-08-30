-- 0003_add_episode_end.sql — 多集合一文件的槽位区间支撑（D-036 冻结的 S01E01E02 形态）：
-- placements.episode_end 记录多集文件的结束集；NULL=单集。
-- matcher 在 M2 即创建 placement，事后补列需 backfill，故随 M2 落地。

ALTER TABLE placements ADD COLUMN episode_end REAL;
