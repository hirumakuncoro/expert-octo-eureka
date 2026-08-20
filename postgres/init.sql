CREATE TABLE IF NOT EXISTS products (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    stock INTEGER NOT NULL DEFAULT 0 CHECK (stock >= 0),
    price BIGINT NOT NULL DEFAULT 0 CHECK (price >= 0),
    version INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO products (name, stock, price)
VALUES
    ('iPhone 16 Pro', 100000, 18999000),
    ('Samsung Galaxy S25', 100000, 14999000),
    ('MacBook Air M4', 100000, 17999000),
    ('Sony WH-1000XM5', 100000, 5499000),
    ('Nintendo Switch OLED', 100000, 5299000)