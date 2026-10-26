package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/iamBlissMedik/shopwave-go/internal/auth"
	"github.com/iamBlissMedik/shopwave-go/internal/config"
	"github.com/iamBlissMedik/shopwave-go/internal/database"
	"github.com/iamBlissMedik/shopwave-go/internal/server"
)

// Integration tests run the real HTTP stack against a real Postgres database.
// They need TEST_DATABASE_URL (a disposable database — it is truncated
// between tests) and are skipped without it:
//
//	TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/shopwave_go_test?sslmode=disable go test ./...

var (
	baseURL string
	pool    *pgxpool.Pool
)

func TestMain(m *testing.M) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		fmt.Println("skipping integration tests: TEST_DATABASE_URL is not set")
		os.Exit(0)
	}
	os.Exit(runWithServer(m, url))
}

func runWithServer(m *testing.M, url string) int {
	ctx := context.Background()
	if err := database.Migrate(ctx, url, "up"); err != nil {
		fmt.Println("migrate:", err)
		return 1
	}
	var err error
	if pool, err = database.Connect(ctx, url); err != nil {
		fmt.Println("connect:", err)
		return 1
	}
	defer pool.Close()

	cfg := config.Config{
		Env:        "test",
		JWTSecret:  "test-secret-not-for-production",
		JWTTTL:     time.Hour,
		AuthCookie: "token",
		CartCookie: "cart_sid",
	}
	srv := httptest.NewServer(server.New(cfg, pool, slog.New(slog.DiscardHandler)))
	defer srv.Close()
	baseURL = srv.URL

	return m.Run()
}

// resetDB wipes every table so each test starts from a clean slate.
func resetDB(t *testing.T) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`TRUNCATE order_items, orders, cart_items, carts, products, users CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
}

// client is a cookie-keeping HTTP client: one client = one browser.
type client struct {
	t    *testing.T
	http *http.Client
}

func newClient(t *testing.T) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, http: &http.Client{Jar: jar}}
}

type response struct {
	t      *testing.T
	status int
	body   map[string]any
}

func (c *client) do(method, path string, body any, headers ...string) response {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, baseURL+path, reader)
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	out := response{t: c.t, status: res.StatusCode}
	_ = json.NewDecoder(res.Body).Decode(&out.body)
	return out
}

func (c *client) get(path string) response            { return c.do(http.MethodGet, path, nil) }
func (c *client) post(path string, body any) response { return c.do(http.MethodPost, path, body) }
func (c *client) patch(path string, body any) response {
	return c.do(http.MethodPatch, path, body)
}
func (c *client) delete(path string) response { return c.do(http.MethodDelete, path, nil) }

// field walks the JSON body by a dotted path, e.g. "cart.items.0.quantity".
func (r response) field(path string) any {
	r.t.Helper()
	var cur any = r.body
	for _, key := range strings.Split(path, ".") {
		switch v := cur.(type) {
		case map[string]any:
			cur = v[key]
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i >= len(v) {
				r.t.Fatalf("path %q: bad index %q in %v", path, key, r.body)
			}
			cur = v[i]
		default:
			r.t.Fatalf("path %q not found in %v", path, r.body)
		}
	}
	return cur
}

func (r response) num(path string) int    { return int(r.field(path).(float64)) }
func (r response) str(path string) string { return r.field(path).(string) }
func (r response) len(path string) int    { return len(r.field(path).([]any)) }

// expect fails the test if the status isn't want, printing the body.
func (r response) expect(want int) response {
	r.t.Helper()
	if r.status != want {
		r.t.Fatalf("status = %d, want %d; body = %v", r.status, want, r.body)
	}
	return r
}

var emailSeq atomic.Int64

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s%d@example.com", prefix, emailSeq.Add(1))
}

const testPassword = "Password1!"

// register signs up a USER or MERCHANT and returns their logged-in client.
func register(t *testing.T, role auth.Role) (*client, string, string) {
	t.Helper()
	c := newClient(t)
	email := uniqueEmail(strings.ToLower(string(role)))
	body := map[string]any{"name": "Tester", "email": email, "password": testPassword, "role": role}
	if role == auth.RoleMerchant {
		body["storeName"] = "Test Store"
	}
	res := c.post("/api/auth/register", body).expect(http.StatusCreated)
	return c, res.str("user.id"), email
}

// login returns a fresh client logged in as email.
func login(t *testing.T, email string) *client {
	t.Helper()
	c := newClient(t)
	c.post("/api/auth/login", map[string]any{"email": email, "password": testPassword}).expect(http.StatusOK)
	return c
}

// adminClient inserts an ADMIN directly (self-signup can't create admins) and logs in.
func adminClient(t *testing.T) (*client, string) {
	t.Helper()
	hash, _ := auth.HashPassword(testPassword)
	email := uniqueEmail("admin")
	u, err := auth.NewStore(pool).Create(context.Background(), auth.NewAccount{
		Name: "Admin", Email: email, PasswordHash: hash, Role: auth.RoleAdmin,
	})
	if err != nil {
		t.Fatal(err)
	}
	return login(t, email), u.ID
}

// createProduct creates a product as merchant and returns its id.
func createProduct(t *testing.T, merchant *client, overrides map[string]any) string {
	t.Helper()
	body := map[string]any{
		"name": "Test Widget", "description": "A solid, dependable test widget.",
		"price": 1999, "category": "Tools", "stock": 10,
	}
	for k, v := range overrides {
		body[k] = v
	}
	return merchant.post("/api/merchant/products", body).expect(http.StatusCreated).str("product.id")
}

func addToCart(t *testing.T, c *client, productID string, qty int) response {
	t.Helper()
	return c.post("/api/cart/items", map[string]any{"productId": productID, "quantity": qty})
}
