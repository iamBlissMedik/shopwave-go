// Command seed resets the database to a small demo marketplace: one admin,
// one customer, and two merchants with a catalogue split between them, so
// every role is usable immediately. It WIPES all data and refuses to run
// when APP_ENV=production.
//
//	go run ./cmd/seed
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"

	"github.com/iamBlissMedik/shopwave-go/internal/auth"
	"github.com/iamBlissMedik/shopwave-go/internal/database"
	"github.com/iamBlissMedik/shopwave-go/internal/product"
)

type seedUser struct {
	key, name, email, password string
	role                       auth.Role
	storeName                  string
}

var users = []seedUser{
	{"admin", "Platform Admin", "admin@example.com", "Admin123!", auth.RoleAdmin, ""},
	{"user", "Demo Customer", "user@example.com", "User1234!", auth.RoleUser, ""},
	{"sound", "Sasha (SoundHub)", "sound@example.com", "Merchant1!", auth.RoleMerchant, "SoundHub Audio"},
	{"gear", "Gabe (GearStop)", "gear@example.com", "Merchant1!", auth.RoleMerchant, "GearStop Supplies"},
}

// Prices are in cents; store is the owning merchant's seedUser key.
var products = []struct {
	store, name, category, description string
	price                              int64
	stock                              int
}{
	{"sound", "Aurora Wireless Headphones", "Audio", "Over-ear headphones with active noise cancellation and 30-hour battery life.", 12999, 40},
	{"sound", "Pulse Bluetooth Speaker", "Audio", "Compact, water-resistant speaker with deep bass and 12-hour playback.", 5999, 60},
	{"sound", "Drift Noise-Cancelling Earbuds", "Audio", "True wireless earbuds with ANC, wireless charging case, and IPX5 rating.", 8999, 55},
	{"sound", "Quartz Smartwatch", "Wearables", "Fitness smartwatch with heart-rate, GPS, and a bright AMOLED display.", 19999, 30},
	{"sound", "Halo Fitness Band", "Wearables", "Lightweight activity tracker with sleep monitoring and 7-day battery.", 6999, 50},
	{"sound", "Cobalt Power Bank 20K", "Power", "20,000mAh power bank with fast charging and dual USB-C ports.", 3999, 90},
	{"gear", "Nimbus Mechanical Keyboard", "Accessories", "Hot-swappable mechanical keyboard with RGB backlighting and tactile switches.", 8999, 35},
	{"gear", "Vector Ergonomic Mouse", "Accessories", "Wireless ergonomic mouse with adjustable DPI and silent clicks.", 4499, 80},
	{"gear", "Lumen 4K Webcam", "Accessories", "4K UHD webcam with auto-focus and built-in noise-cancelling mic.", 7499, 25},
	{"gear", "Orbit USB-C Hub", "Accessories", "7-in-1 USB-C hub with HDMI, card reader, and 100W power delivery.", 5499, 70},
	{"gear", "Zephyr Laptop Stand", "Office", "Aluminium adjustable laptop stand for better posture and cooling.", 3499, 100},
	{"gear", "Beacon Desk Lamp", "Office", "LED desk lamp with adjustable color temperature and wireless charging base.", 4299, 45},
	{"gear", "Forge Gaming Chair", "Furniture", "Ergonomic gaming chair with lumbar support and reclining backrest.", 24999, 15},
	{"gear", "Slate Standing Desk", "Furniture", "Electric height-adjustable standing desk with memory presets.", 39999, 12},
	{"gear", "Pixel 27\" Monitor", "Displays", "27-inch QHD IPS monitor with 165Hz refresh rate and thin bezels.", 29999, 20},
	{"gear", "Cinema 34\" Ultrawide", "Displays", "34-inch curved ultrawide monitor, perfect for multitasking and gaming.", 54999, 10},
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "seed failed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	_ = godotenv.Load()
	if os.Getenv("APP_ENV") == "production" {
		return fmt.Errorf("refusing to wipe and seed a production database")
	}

	pool, err := database.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()

	fmt.Println("Seeding database…")
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `TRUNCATE order_items, orders, cart_items, carts, products, users CASCADE`); err != nil {
			return err
		}

		accounts := auth.NewStore(tx)
		ids := map[string]string{}
		for _, u := range users {
			hash, err := auth.HashPassword(u.password)
			if err != nil {
				return err
			}
			var storeName *string
			if u.storeName != "" {
				storeName = &u.storeName
			}
			created, err := accounts.Create(ctx, auth.NewAccount{
				Name: u.name, Email: u.email, PasswordHash: hash, Role: u.role, StoreName: storeName,
			})
			if err != nil {
				return fmt.Errorf("create user %s: %w", u.email, err)
			}
			ids[u.key] = created.ID
		}

		catalogue := product.NewStore(tx)
		for i, p := range products {
			_, err := catalogue.Create(ctx, product.NewProduct{
				MerchantID:  ids[p.store],
				Name:        p.name,
				Slug:        product.Slugify(p.name),
				Description: p.description,
				Price:       p.price,
				ImageURL:    fmt.Sprintf("https://picsum.photos/seed/shopwave-%d/600/600", i+1),
				Category:    p.category,
				Stock:       p.stock,
			})
			if err != nil {
				return fmt.Errorf("create product %q: %w", p.name, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Printf("Seeded %d products across 2 stores and %d users\n", len(products), len(users))
	for _, u := range users {
		fmt.Printf("  %-8s → %s / %s\n", u.role, u.email, u.password)
	}
	return nil
}
