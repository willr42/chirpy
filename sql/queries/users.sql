-- name: CreateUser :one
INSERT INTO users(id, created_at, updated_at, email, hashed_password)
    VALUES ($1, $2, $3, $4, $5)
RETURNING
    *;

-- name: ClearUsers :exec
DELETE FROM users;

-- name: GetUserByEmail :one
SELECT
    id,
    created_at,
    updated_at,
    email,
    hashed_password
FROM
    users
WHERE
    email = $1;

-- name: UpdateUser :one
UPDATE
    users
SET
    email = $1,
    hashed_password = $2,
    updated_at = $3
WHERE
    id = $4
RETURNING
    *;

