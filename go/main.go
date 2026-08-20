package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type config struct {
	port         string
	productIDMin int
	productIDMax int
	decrementMin int
	decrementMax int
}

type Product struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Stock     int       `json:"stock"`
	Price     string    `json:"price"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type purchaseResult struct {
	Product   Product `json:"product"`
	Decrement int     `json:"decrement"`
	Updated   bool    `json:"updated"`
}

func main() {
	cfg := loadConfig()

	poolConfig, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 1

	db, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /products/{id}", func(w http.ResponseWriter, r *http.Request) {
		productID, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || productID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid product id"})
			return
		}

		product, err := getProduct(r.Context(), db, productID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if err != nil {
			log.Printf("get product: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]Product{"product": product})
	})
	mux.HandleFunc("POST /products/random-purchase", func(w http.ResponseWriter, r *http.Request) {
		result, err := purchaseRandomProduct(r.Context(), db, cfg)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if err != nil {
			log.Printf("purchase random product: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		writeJSON(w, http.StatusOK, result)
	})

	log.Printf("go-service listening on :%s", cfg.port)
	log.Fatal(http.ListenAndServe(":"+cfg.port, mux))
}

func loadConfig() config {
	return config{
		port:         env("PORT", "8080"),
		productIDMin: envInt("PRODUCT_ID_MIN", 1),
		productIDMax: envInt("PRODUCT_ID_MAX", 5),
		decrementMin: envInt("DECREMENT_MIN", 1),
		decrementMax: envInt("DECREMENT_MAX", 10),
	}
}

func getProduct(ctx context.Context, db *pgxpool.Pool, id int) (Product, error) {
	var p Product
	err := db.QueryRow(ctx, `
		SELECT id, name, stock, price::text, version, created_at, updated_at
		FROM products
		WHERE id = $1
		LIMIT 1
	`, id).Scan(&p.ID, &p.Name, &p.Stock, &p.Price, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func purchaseRandomProduct(ctx context.Context, db *pgxpool.Pool, cfg config) (purchaseResult, error) {
	productID := randomInt(cfg.productIDMin, cfg.productIDMax)
	decrement := randomInt(cfg.decrementMin, cfg.decrementMax)

	tx, err := db.Begin(ctx)
	if err != nil {
		return purchaseResult{}, err
	}
	defer tx.Rollback(ctx)

	p, err := lockProduct(ctx, tx, productID)
	if err != nil {
		return purchaseResult{}, err
	}

	if p.Stock < decrement {
		return purchaseResult{
			Product:   p,
			Decrement: decrement,
			Updated:   false,
		}, tx.Commit(ctx)
	}

	p, err = decrementStock(ctx, tx, p.ID, decrement)
	if err != nil {
		return purchaseResult{}, err
	}

	return purchaseResult{
		Product:   p,
		Decrement: decrement,
		Updated:   true,
	}, tx.Commit(ctx)
}

func lockProduct(ctx context.Context, tx pgx.Tx, id int) (Product, error) {
	var p Product
	err := tx.QueryRow(ctx, `
		SELECT id, name, stock, price::text, version, created_at, updated_at
		FROM products
		WHERE id = $1
		LIMIT 1
		FOR UPDATE
	`, id).Scan(&p.ID, &p.Name, &p.Stock, &p.Price, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func decrementStock(ctx context.Context, tx pgx.Tx, id int64, decrement int) (Product, error) {
	var p Product
	err := tx.QueryRow(ctx, `
		UPDATE products
		SET stock = stock - $1,
		    updated_at = now()
		WHERE id = $2
		RETURNING id, name, stock, price::text, version, created_at, updated_at
	`, decrement, id).Scan(&p.ID, &p.Name, &p.Stock, &p.Price, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func randomInt(min, max int) int {
	return rand.Intn(max-min+1) + min
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
