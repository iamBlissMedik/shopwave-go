package server_test

import (
	"net/http"
	"sync"
	"testing"

	"github.com/iamBlissMedik/shopwave-go/internal/auth"
)

func TestAuth(t *testing.T) {
	resetDB(t)

	c, _, email := register(t, auth.RoleUser)
	if got := c.get("/api/auth/me").expect(http.StatusOK).str("user.email"); got != email {
		t.Errorf("me email = %q, want %q", got, email)
	}

	t.Run("duplicate email is a conflict", func(t *testing.T) {
		newClient(t).post("/api/auth/register", map[string]any{
			"name": "Dup", "email": email, "password": testPassword,
		}).expect(http.StatusConflict)
	})
	t.Run("self-signup cannot create admins", func(t *testing.T) {
		newClient(t).post("/api/auth/register", map[string]any{
			"name": "Sneaky", "email": uniqueEmail("x"), "password": testPassword, "role": "ADMIN",
		}).expect(http.StatusBadRequest)
	})
	t.Run("wrong password and unknown email look identical", func(t *testing.T) {
		a := newClient(t).post("/api/auth/login", map[string]any{"email": email, "password": "wrong-pass"})
		b := newClient(t).post("/api/auth/login", map[string]any{"email": "ghost@example.com", "password": "wrong-pass"})
		a.expect(http.StatusUnauthorized)
		b.expect(http.StatusUnauthorized)
		if a.str("error.message") != b.str("error.message") {
			t.Error("login errors differ — leaks which emails exist")
		}
	})
	t.Run("logout ends the session", func(t *testing.T) {
		c.post("/api/auth/logout", nil).expect(http.StatusOK)
		c.get("/api/auth/me").expect(http.StatusUnauthorized)
	})
}

func TestAccessControl(t *testing.T) {
	resetDB(t)
	anon := newClient(t)
	user, _, _ := register(t, auth.RoleUser)
	merchant, _, _ := register(t, auth.RoleMerchant)

	anon.get("/api/orders").expect(http.StatusUnauthorized)
	anon.get("/api/merchant/stats").expect(http.StatusUnauthorized)
	user.get("/api/merchant/stats").expect(http.StatusForbidden)
	user.get("/api/admin/stats").expect(http.StatusForbidden)
	merchant.get("/api/admin/users").expect(http.StatusForbidden)
}

func TestCatalogue(t *testing.T) {
	resetDB(t)
	merchant, _, _ := register(t, auth.RoleMerchant)
	cheap := createProduct(t, merchant, map[string]any{"name": "Cheap Mug", "price": 500, "category": "Kitchen"})
	createProduct(t, merchant, map[string]any{"name": "Fancy Lamp", "price": 9000, "category": "Office"})
	hidden := createProduct(t, merchant, map[string]any{"name": "Hidden Thing"})
	merchant.delete("/api/merchant/products/" + hidden).expect(http.StatusOK)

	anon := newClient(t)
	if n := anon.get("/api/products").expect(http.StatusOK).num("meta.total"); n != 2 {
		t.Errorf("public catalogue total = %d, want 2 (inactive hidden)", n)
	}
	res := anon.get("/api/products?category=Kitchen&maxPrice=1000").expect(http.StatusOK)
	if res.len("data") != 1 || res.str("data.0.id") != cheap {
		t.Errorf("filter returned %v", res.body)
	}
	if got := anon.get("/api/products?sort=price_desc").str("data.0.name"); got != "Fancy Lamp" {
		t.Errorf("price_desc first = %q", got)
	}
	anon.get("/api/products/" + hidden).expect(http.StatusNotFound)
	anon.get("/api/products?limit=1000").expect(http.StatusBadRequest)
	if n := anon.get("/api/products/categories").len("categories"); n != 2 {
		t.Errorf("categories = %d, want 2", n)
	}
}

func TestGuestCart(t *testing.T) {
	resetDB(t)
	merchant, _, _ := register(t, auth.RoleMerchant)
	product := createProduct(t, merchant, map[string]any{"price": 1000, "stock": 4})
	_, _, email := register(t, auth.RoleUser)

	t.Run("guests get isolated carts", func(t *testing.T) {
		a, b := newClient(t), newClient(t)
		addToCart(t, a, product, 1).expect(http.StatusOK)
		if n := a.get("/api/cart").num("cart.itemCount"); n != 1 {
			t.Errorf("guest a itemCount = %d, want 1", n)
		}
		if n := b.get("/api/cart").num("cart.itemCount"); n != 0 {
			t.Errorf("guest b itemCount = %d, want 0", n)
		}
	})

	t.Run("login merges the guest cart, summing and clamping to stock", func(t *testing.T) {
		account := login(t, email)
		addToCart(t, account, product, 1).expect(http.StatusOK)

		browser := newClient(t)
		addToCart(t, browser, product, 4).expect(http.StatusOK) // 1 + 4 > stock of 4
		browser.post("/api/auth/login", map[string]any{"email": email, "password": testPassword}).expect(http.StatusOK)

		cart := browser.get("/api/cart").expect(http.StatusOK)
		if cart.len("cart.items") != 1 || cart.num("cart.itemCount") != 4 || cart.num("cart.subtotal") != 4000 {
			t.Errorf("merged cart = %v", cart.body)
		}
	})

	t.Run("stock is checked when adding", func(t *testing.T) {
		addToCart(t, newClient(t), product, 5).expect(http.StatusBadRequest)
	})
}

func TestCheckout(t *testing.T) {
	resetDB(t)
	merchant, _, _ := register(t, auth.RoleMerchant)
	product := createProduct(t, merchant, map[string]any{"price": 2000, "stock": 5})
	user, _, _ := register(t, auth.RoleUser)

	t.Run("empty cart is rejected", func(t *testing.T) {
		user.post("/api/orders/checkout", nil).expect(http.StatusBadRequest)
	})

	addToCart(t, user, product, 2).expect(http.StatusOK)
	order := user.do(http.MethodPost, "/api/orders/checkout", nil, "Idempotency-Key", "k1").expect(http.StatusCreated)
	orderID := order.str("order.id")

	t.Run("snapshots price, decrements stock, clears cart", func(t *testing.T) {
		if order.num("order.total") != 4000 || order.num("order.items.0.price") != 2000 || order.str("order.status") != "PAID" {
			t.Errorf("order = %v", order.body)
		}
		if n := merchant.get("/api/merchant/products/" + product).num("product.stock"); n != 3 {
			t.Errorf("stock = %d, want 3", n)
		}
		if n := user.get("/api/cart").num("cart.itemCount"); n != 0 {
			t.Errorf("cart not cleared: itemCount = %d", n)
		}
	})

	t.Run("retry with the same idempotency key returns the same order", func(t *testing.T) {
		again := user.do(http.MethodPost, "/api/orders/checkout", nil, "Idempotency-Key", "k1").expect(http.StatusCreated)
		if again.str("order.id") != orderID {
			t.Error("idempotent retry created a new order")
		}
	})

	t.Run("price changes never alter history", func(t *testing.T) {
		merchant.patch("/api/merchant/products/"+product, map[string]any{"price": 9999}).expect(http.StatusOK)
		got := user.get("/api/orders/" + orderID).expect(http.StatusOK)
		if got.num("order.total") != 4000 || got.num("order.items.0.price") != 2000 {
			t.Errorf("historical order changed: %v", got.body)
		}
	})

	t.Run("orders are private to their owner", func(t *testing.T) {
		other, _, _ := register(t, auth.RoleUser)
		other.get("/api/orders/" + orderID).expect(http.StatusNotFound)
		if n := user.get("/api/orders").num("meta.total"); n != 1 {
			t.Errorf("order list total = %d, want 1", n)
		}
	})
}

// Two customers race for the last unit: exactly one checkout may succeed, and
// the loser's transaction must roll back completely.
func TestCheckoutNeverOversells(t *testing.T) {
	resetDB(t)
	merchant, _, _ := register(t, auth.RoleMerchant)
	product := createProduct(t, merchant, map[string]any{"stock": 1})

	buyers := make([]*client, 5)
	for i := range buyers {
		buyers[i], _, _ = register(t, auth.RoleUser)
		addToCart(t, buyers[i], product, 1).expect(http.StatusOK)
	}

	statuses := make([]int, len(buyers))
	var wg sync.WaitGroup
	for i, b := range buyers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i] = b.post("/api/orders/checkout", nil).status
		}()
	}
	wg.Wait()

	wins := 0
	for _, s := range statuses {
		if s == http.StatusCreated {
			wins++
		} else if s != http.StatusBadRequest {
			t.Errorf("unexpected status %d", s)
		}
	}
	if wins != 1 {
		t.Errorf("%d checkouts succeeded for 1 unit of stock", wins)
	}
	if n := merchant.get("/api/merchant/products/" + product).num("product.stock"); n != 0 {
		t.Errorf("stock = %d, want 0", n)
	}
}

func TestMerchantDashboard(t *testing.T) {
	resetDB(t)
	alice, _, _ := register(t, auth.RoleMerchant)
	product := createProduct(t, alice, map[string]any{"name": "Alice Widget", "price": 2500, "stock": 10})
	bob, _, _ := register(t, auth.RoleMerchant)
	createProduct(t, bob, map[string]any{"name": "Bob Widget"})

	user, _, _ := register(t, auth.RoleUser)
	addToCart(t, user, product, 2).expect(http.StatusOK)
	user.post("/api/orders/checkout", nil).expect(http.StatusCreated)

	t.Run("merchants only see and touch their own products", func(t *testing.T) {
		list := alice.get("/api/merchant/products").expect(http.StatusOK)
		if list.num("meta.total") != 1 || list.str("data.0.name") != "Alice Widget" {
			t.Errorf("alice's list = %v", list.body)
		}
		bob.get("/api/merchant/products/" + product).expect(http.StatusForbidden)
		bob.patch("/api/merchant/products/"+product, map[string]any{"price": 1}).expect(http.StatusForbidden)
		bob.delete("/api/merchant/products/" + product).expect(http.StatusForbidden)
	})

	t.Run("stats reflect real sales", func(t *testing.T) {
		stats := alice.get("/api/merchant/stats").expect(http.StatusOK)
		if stats.num("totals.unitsSold") != 2 || stats.num("totals.totalRevenue") != 5000 ||
			stats.num("topProducts.0.revenue") != 5000 || stats.len("revenueByDay") != 14 {
			t.Errorf("stats = %v", stats.body)
		}
		last := stats.field("revenueByDay").([]any)[13].(map[string]any)
		if last["value"].(float64) != 5000 {
			t.Errorf("today's revenue = %v, want 5000", last["value"])
		}
	})

	t.Run("sales feed shows only the merchant's lines", func(t *testing.T) {
		sales := alice.get("/api/merchant/sales").expect(http.StatusOK)
		if sales.len("data") != 1 || sales.num("data.0.revenue") != 5000 || sales.num("data.0.quantity") != 2 {
			t.Errorf("sales = %v", sales.body)
		}
		if n := bob.get("/api/merchant/sales").len("data"); n != 0 {
			t.Errorf("bob sees %d sales, want 0", n)
		}
	})

	t.Run("an empty update is rejected", func(t *testing.T) {
		alice.patch("/api/merchant/products/"+product, map[string]any{}).expect(http.StatusBadRequest)
	})
}

func TestAdminConsole(t *testing.T) {
	resetDB(t)
	merchant, merchantID, _ := register(t, auth.RoleMerchant)
	product := createProduct(t, merchant, map[string]any{"price": 4000})
	user, userID, userEmail := register(t, auth.RoleUser)
	addToCart(t, user, product, 1).expect(http.StatusOK)
	user.post("/api/orders/checkout", nil).expect(http.StatusCreated)
	admin, adminID := adminClient(t)

	t.Run("platform stats", func(t *testing.T) {
		stats := admin.get("/api/admin/stats").expect(http.StatusOK)
		if stats.num("totals.users") != 3 || stats.num("totals.merchants") != 1 ||
			stats.num("totals.orders") != 1 || stats.num("totals.revenue") != 4000 || stats.len("usersByRole") != 3 {
			t.Errorf("stats = %v", stats.body)
		}
	})

	t.Run("user directory filters by role and counts relations", func(t *testing.T) {
		res := admin.get("/api/admin/users?role=MERCHANT").expect(http.StatusOK)
		if res.num("meta.total") != 1 || res.num("data.0._count.products") != 1 {
			t.Errorf("merchants = %v", res.body)
		}
	})

	t.Run("moderation hides a product from the catalogue", func(t *testing.T) {
		admin.patch("/api/admin/products/"+product, map[string]any{"isActive": false}).expect(http.StatusOK)
		newClient(t).get("/api/products/" + product).expect(http.StatusNotFound)
		if n := admin.get("/api/admin/products?status=inactive").num("meta.total"); n != 1 {
			t.Errorf("inactive products = %d, want 1", n)
		}
	})

	t.Run("role change applies at next login", func(t *testing.T) {
		admin.patch("/api/admin/users/"+userID+"/role", map[string]any{"role": "MERCHANT"}).expect(http.StatusOK)
		user.get("/api/merchant/stats").expect(http.StatusForbidden) // old token still says USER
		login(t, userEmail).get("/api/merchant/stats").expect(http.StatusOK)
	})

	t.Run("admins cannot change or delete themselves", func(t *testing.T) {
		admin.patch("/api/admin/users/"+adminID+"/role", map[string]any{"role": "USER"}).expect(http.StatusBadRequest)
		admin.delete("/api/admin/users/" + adminID).expect(http.StatusBadRequest)
	})

	t.Run("a merchant with sold products cannot be hard-deleted", func(t *testing.T) {
		admin.delete("/api/admin/users/" + merchantID).expect(http.StatusConflict)
	})

	t.Run("deleting a user", func(t *testing.T) {
		_, id, _ := register(t, auth.RoleUser)
		admin.delete("/api/admin/users/" + id).expect(http.StatusOK)
		admin.delete("/api/admin/users/" + id).expect(http.StatusNotFound)
	})
}
