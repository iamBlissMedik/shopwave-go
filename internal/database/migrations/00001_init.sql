-- Initial schema: users (all roles), merchant-owned products, carts (user or
-- guest), and orders with immutable price snapshots.
--
-- Conventions: uuid primary keys, snake_case, timestamptz, money as bigint
-- cents (never floats), and `updated_at` maintained by a trigger so no query
-- can forget it.

-- +goose Up

-- +goose StatementBegin
CREATE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL UNIQUE,
    password_hash text NOT NULL, -- bcrypt, never plaintext
    name          text NOT NULL,
    role          text NOT NULL DEFAULT 'USER' CHECK (role IN ('USER', 'MERCHANT', 'ADMIN')),
    store_name    text,          -- merchants only: public storefront name
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX users_role_idx ON users (role);
CREATE INDEX users_created_at_idx ON users (created_at);

CREATE TABLE products (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text NOT NULL,
    slug        text NOT NULL UNIQUE,
    description text NOT NULL,
    price       bigint NOT NULL CHECK (price >= 0), -- cents
    image_url   text NOT NULL,
    category    text NOT NULL,
    stock       integer NOT NULL DEFAULT 0 CHECK (stock >= 0),
    -- Soft delete / moderation flag: hide a product without breaking the
    -- order history that references it.
    is_active   boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
-- Back the catalogue filters/sorts and the ownership queries.
CREATE INDEX products_merchant_id_idx ON products (merchant_id);
CREATE INDEX products_category_idx ON products (category);
CREATE INDEX products_price_idx ON products (price);
CREATE INDEX products_created_at_idx ON products (created_at);
CREATE INDEX products_is_active_idx ON products (is_active);

-- A cart belongs to exactly one of: a logged-in user, or an anonymous guest
-- session (httpOnly cookie). Unique + nullable: Postgres allows many NULLs.
CREATE TABLE carts (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    session_id text UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT carts_single_owner CHECK ((user_id IS NULL) <> (session_id IS NULL))
);

CREATE TABLE cart_items (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    cart_id    uuid NOT NULL REFERENCES carts (id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    quantity   integer NOT NULL CHECK (quantity > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (cart_id, product_id) -- one line per product; quantity tracks the rest
);
CREATE INDEX cart_items_product_id_idx ON cart_items (product_id);

CREATE TABLE orders (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    total           bigint NOT NULL CHECK (total >= 0), -- cents, snapshot at checkout
    status          text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'PAID', 'CANCELLED')),
    -- Optional client-supplied key: a retried checkout returns the original
    -- order. Scoped per user so one customer's key can never collide with, or
    -- reveal, another's order.
    idempotency_key text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT orders_user_idempotency_key UNIQUE (user_id, idempotency_key)
);
CREATE INDEX orders_user_id_created_at_idx ON orders (user_id, created_at DESC);
CREATE INDEX orders_status_created_at_idx ON orders (status, created_at);

CREATE TABLE order_items (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id   uuid NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    -- RESTRICT: a product that has been sold can't be hard-deleted (soft delete instead).
    product_id uuid NOT NULL REFERENCES products (id) ON DELETE RESTRICT,
    price      bigint NOT NULL CHECK (price >= 0), -- unit price snapshot (cents)
    quantity   integer NOT NULL CHECK (quantity > 0)
);
CREATE INDEX order_items_order_id_idx ON order_items (order_id);
CREATE INDEX order_items_product_id_idx ON order_items (product_id);

CREATE TRIGGER users_updated_at BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER products_updated_at BEFORE UPDATE ON products FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER carts_updated_at BEFORE UPDATE ON carts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER cart_items_updated_at BEFORE UPDATE ON cart_items FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER orders_updated_at BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE order_items;
DROP TABLE orders;
DROP TABLE cart_items;
DROP TABLE carts;
DROP TABLE products;
DROP TABLE users;
DROP FUNCTION set_updated_at();
