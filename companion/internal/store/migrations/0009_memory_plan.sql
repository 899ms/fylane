-- The plan a workspace is working through (Batch U, U-T2/D39, 2026-09-16).
--
-- A web chat has no harness: no todo list, no progress, and a turn that
-- carries tool calls is cut off after about 25 minutes. The page in
-- memory_state says where the work stands in prose; this table says it in
-- steps a model can move one at a time, so a new conversation that is told
-- "continue" can see which step was in flight and pick it up.
--
-- Steps are replaced whole when the plan is rewritten and updated one row
-- at a time as work proceeds — that asymmetry is the point: moving one
-- step must not cost the whole plan, in tokens or in a lost update from
-- another conversation.
--
-- Like the rest of the memory, this is never a file in the user's project.
CREATE TABLE memory_plan_steps (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id),
    -- position orders the plan; it is rewritten with the plan, while id
    -- stays the handle a step is addressed by.
    position     INTEGER NOT NULL,
    title        TEXT NOT NULL,
    -- state is one of todo | doing | blocked | done.
    state        TEXT NOT NULL DEFAULT 'todo',
    -- note is why a step is blocked, or what it turned out to involve.
    note         TEXT NOT NULL DEFAULT '',
    -- provider is who last touched the step, so a plan carried between
    -- platforms says where each step was moved.
    provider     TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE INDEX memory_plan_steps_workspace ON memory_plan_steps (workspace_id, position);
