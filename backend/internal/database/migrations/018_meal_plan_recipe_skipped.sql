-- Adds an explicit "won't make" state for active-plan recipes.
-- Distinct from completed=false: skipped is a deliberate user decision so
-- end-of-period flow can treat (completed OR skipped) as "resolved".
-- Skipped rows are NOT counted as cooked (completed stays FALSE), so eat-count
-- queries that filter `WHERE completed = TRUE` remain correct.
ALTER TABLE meal_plan_recipes
    ADD COLUMN IF NOT EXISTS skipped BOOLEAN NOT NULL DEFAULT FALSE;
