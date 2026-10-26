package product

import (
	"context"
	"crypto/rand"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/iamBlissMedik/shopwave-go/internal/apperr"
	"github.com/iamBlissMedik/shopwave-go/internal/auth"
	"github.com/iamBlissMedik/shopwave-go/internal/database"
	"github.com/iamBlissMedik/shopwave-go/internal/pagination"
)

// Service holds the catalogue rules: customers only see active products, and
// writes are scoped to the owning merchant (admins may touch anything).
type Service struct{ store *Store }

func NewService(store *Store) *Service { return &Service{store: store} }

type CreateInput struct {
	Name        string `json:"name" validate:"required,min=2,max=120"`
	Description string `json:"description" validate:"required,min=10,max=2000"`
	Price       *int64 `json:"price" validate:"required,min=0"` // cents
	Category    string `json:"category" validate:"required,min=2,max=60"`
	Stock       int    `json:"stock" validate:"min=0"`
	ImageURL    string `json:"imageUrl" validate:"omitempty,http_url,max=2048"`
}

func (in *CreateInput) Normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Category = strings.TrimSpace(in.Category)
	in.ImageURL = strings.TrimSpace(in.ImageURL)
}

// UpdateInput is a partial update: nil fields are left unchanged.
type UpdateInput struct {
	Name        *string `json:"name" validate:"omitempty,min=2,max=120"`
	Description *string `json:"description" validate:"omitempty,min=10,max=2000"`
	Price       *int64  `json:"price" validate:"omitempty,min=0"`
	Category    *string `json:"category" validate:"omitempty,min=2,max=60"`
	Stock       *int    `json:"stock" validate:"omitempty,min=0"`
	ImageURL    *string `json:"imageUrl" validate:"omitempty,http_url,max=2048"`
	IsActive    *bool   `json:"isActive"`
}

func (in *UpdateInput) Normalize() {
	for _, s := range []*string{in.Name, in.Description, in.Category, in.ImageURL} {
		if s != nil {
			*s = strings.TrimSpace(*s)
		}
	}
}

func (in *UpdateInput) Check() error {
	if *in == (UpdateInput{}) {
		return apperr.Invalid("Provide at least one field to update")
	}
	return nil
}

// Catalogue lists products for the public storefront (active only).
func (s *Service) Catalogue(ctx context.Context, f ListFilter, p pagination.Params) (pagination.Page[Product], error) {
	active := true
	f.Active = &active
	return s.store.List(ctx, f, p)
}

// List lists products without visibility rules; callers (merchant/admin
// handlers) set the scoping filters themselves.
func (s *Service) List(ctx context.Context, f ListFilter, p pagination.Params) (pagination.Page[Product], error) {
	return s.store.List(ctx, f, p)
}

// GetPublic returns an active product.
func (s *Service) GetPublic(ctx context.Context, id string) (Product, error) {
	p, err := s.get(ctx, id)
	if err == nil && !p.IsActive {
		return Product{}, errNotFound
	}
	return p, err
}

func (s *Service) Categories(ctx context.Context) ([]string, error) {
	return s.store.Categories(ctx)
}

// GetOwned returns a product the actor may manage (for the edit form).
func (s *Service) GetOwned(ctx context.Context, actor auth.Principal, id string) (Product, error) {
	p, err := s.get(ctx, id)
	if err != nil {
		return Product{}, err
	}
	if actor.Role != auth.RoleAdmin && p.MerchantID != actor.ID {
		return Product{}, apperr.Forbidden("You can only manage your own products")
	}
	return p, nil
}

// Create adds a product owned by the actor, generating a unique slug.
func (s *Service) Create(ctx context.Context, actor auth.Principal, in CreateInput) (Product, error) {
	base := Slugify(in.Name)
	for attempt := 0; ; attempt++ {
		slug := base
		if attempt > 0 { // name already taken: add a short random suffix
			slug += "-" + strings.ToLower(rand.Text()[:5])
		}
		imageURL := in.ImageURL
		if imageURL == "" {
			imageURL = "https://picsum.photos/seed/" + slug + "/600/600"
		}
		p, err := s.store.Create(ctx, NewProduct{
			MerchantID: actor.ID, Name: in.Name, Slug: slug, Description: in.Description,
			Price: *in.Price, ImageURL: imageURL, Category: in.Category, Stock: in.Stock,
		})
		if database.IsUniqueViolation(err, "products_slug_key") && attempt < 3 {
			continue
		}
		return p, err
	}
}

// Update applies a partial update to a product the actor may manage.
func (s *Service) Update(ctx context.Context, actor auth.Principal, id string, in UpdateInput) (Product, error) {
	if _, err := s.GetOwned(ctx, actor, id); err != nil {
		return Product{}, err
	}
	return s.store.Update(ctx, id, in)
}

// Delete soft-deletes (hides) a product, keeping order history intact.
func (s *Service) Delete(ctx context.Context, actor auth.Principal, id string) error {
	inactive := false
	_, err := s.Update(ctx, actor, id, UpdateInput{IsActive: &inactive})
	return err
}

var errNotFound = apperr.NotFound("Product not found")

func (s *Service) get(ctx context.Context, id string) (Product, error) {
	p, err := s.store.Get(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, errNotFound
	}
	return p, err
}

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify builds a URL-safe slug: "Pixel 27\" Monitor" → "pixel-27-monitor".
func Slugify(name string) string {
	slug := strings.Trim(nonAlphanumeric.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		return "product"
	}
	return slug
}
