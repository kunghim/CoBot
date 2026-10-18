-- +goose Up
-- The frontend and backend roles became one full-stack engineer. Members of
-- either role take the new role and its shipped instructions (an empty
-- role_version reads the embedded default, not an old profile copy). A member
-- still carrying its role's default name is renamed like a new one would be:
-- the first in a team is 全栈工程师, the next 全栈工程师 2, and so on.
UPDATE bots SET name = renamed.name
FROM (
    SELECT id,
        CASE WHEN rn = 1 THEN '全栈工程师' ELSE '全栈工程师 ' || rn END AS name
    FROM (
        SELECT id, ROW_NUMBER() OVER (PARTITION BY team_id ORDER BY created_at, id) AS rn
        FROM bots
        WHERE role_id IN ('frontend-dev', 'backend-dev')
          AND (name LIKE '前端开发%' OR name LIKE '后端开发%')
    )
) AS renamed
WHERE bots.id = renamed.id;

UPDATE bots SET role_id = 'fullstack-engineer', role_version = ''
WHERE role_id IN ('frontend-dev', 'backend-dev');

-- The leader is now called 项目经理; a leader still named after the old
-- default takes the new name. Names the user chose are kept.
UPDATE bots SET name = '项目经理' WHERE is_leader = 1 AND name = '队长';

-- +goose Down
-- Role and name changes are not reversed: the frontend and backend roles no
-- longer ship, so a downgraded build could not run those members anyway.
SELECT 1;
