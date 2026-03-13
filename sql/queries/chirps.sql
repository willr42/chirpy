-- name: CreateChirp :one
INSERT INTO chirps(id, created_at, updated_at, body, user_id)
    VALUES ($1, $2, $3, $4, $5)
RETURNING
    *;

-- name: GetChirp :one
SELECT
    id,
    created_at,
    updated_at,
    body,
    user_id
FROM
    chirps
WHERE
    id = $1;

-- name: GetAllChirps :many
SELECT
    chirps.id,
    chirps.created_at,
    chirps.updated_at,
    chirps.body,
    chirps.user_id
FROM
    chirps
ORDER BY
    chirps.created_at ASC;

-- name: GetChirpsByUserId :many
SELECT
    chirps.body,
    chirps.created_at,
    users.email
FROM
    chirps
    INNER JOIN users ON users.id = chirps.user_id
WHERE
    chirps.user_id = $1;

