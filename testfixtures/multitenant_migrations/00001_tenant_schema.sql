-- +goose Up
CREATE TABLE tenant_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS tenant_settings;
