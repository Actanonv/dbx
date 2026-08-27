-- +goose Up
CREATE INDEX IF NOT EXISTS idx_orders_item_id ON orders(item_id);
ALTER TABLE items ADD COLUMN price INTEGER DEFAULT 0;

-- +goose Down
DROP INDEX IF EXISTS idx_orders_item_id;
ALTER TABLE items DROP COLUMN price;
