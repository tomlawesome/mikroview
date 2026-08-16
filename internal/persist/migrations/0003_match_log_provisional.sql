-- #399: a provisional marker on matches recorded while their
-- expectation definition's baseline (internal/engine.Baseline, see
-- docs/decisions/evaluation-engine.md section 1) had not yet cleared
-- its history floor -- the matchlog counterpart to flags.Flag's own new
-- Provisional field. Purely additive: every row already on disk gets
-- the default (not provisional), which is correct since nothing wrote
-- a real value here before this column existed. No data migration
-- needed.
ALTER TABLE match_log ADD COLUMN IF NOT EXISTS provisional boolean NOT NULL DEFAULT false;
