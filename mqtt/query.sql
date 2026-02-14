-- name: UpsertDevice :one
INSERT INTO devices (id, lat, lon, alt, speed, course, sats, hdop, battery_mv, rssi, snr, online, last_seen)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(id) DO UPDATE SET
    lat        = excluded.lat,
    lon        = excluded.lon,
    alt        = excluded.alt,
    speed      = excluded.speed,
    course     = excluded.course,
    sats       = excluded.sats,
    hdop       = excluded.hdop,
    battery_mv = excluded.battery_mv,
    rssi       = excluded.rssi,
    snr        = excluded.snr,
    online     = excluded.online,
    last_seen  = CURRENT_TIMESTAMP
RETURNING *;

-- name: ListDevices :many
SELECT * FROM devices ORDER BY last_seen DESC;

-- name: MarkDeviceOffline :exec
UPDATE devices SET online = 0 WHERE id = ?;

-- name: GetDevice :one
SELECT * FROM devices WHERE id = ? LIMIT 1;

-- name: DeleteStaleDevices :exec
DELETE FROM devices WHERE last_seen < datetime('now', '-48 hours');
