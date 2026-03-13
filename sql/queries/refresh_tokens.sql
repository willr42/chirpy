-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens(token, created_at, updated_at, user_id, expires_at)
    VALUES ($1, $2, $3, $4, $5)
RETURNING
    *;

-- name: GetRefreshTokenByToken :one
SELECT
    token,
    created_at,
    updated_at,
    user_id,
    expires_at,
    revoked_at
FROM
    refresh_tokens
WHERE
    refresh_tokens.token = $1;

-- name: GetUserFromRefreshToken :one
SELECT
    users.id
FROM
    refresh_tokens
    INNER JOIN users ON users.id = refresh_tokens.user_id
WHERE
    token = $1;

-- name: RevokeRefreshToken :one
UPDATE
    refresh_tokens
SET
    revoked_at = $1,
    updated_at = $2
WHERE
    token = $3
RETURNING
    *;

