package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/neko233/ssh233-agent-server/internal/config"
	"github.com/neko233/ssh233-agent-server/internal/models"
	"github.com/neko233/ssh233-agent-server/internal/store"
)

type Claims struct {
	UserID     string `json:"user_id"`
	TenantID   string `json:"tenant_id"`
	TenantSlug string `json:"tenant_slug,omitempty"`
	Username   string `json:"username"`
	Role       string `json:"role"`
	jwt.RegisteredClaims
}

func (c *Claims) IsRoot() bool {
	return c.Role == models.RoleRoot
}

func (c *Claims) Scope() store.Scope {
	return store.Scope{TenantID: c.TenantID, Root: c.IsRoot()}
}

type Service struct {
	secret string
	ttl    time.Duration
	store  *store.Store
}

func NewService(cfg *config.AuthConfig, st *store.Store) *Service {
	return &Service{
		secret: cfg.JWTSecret,
		ttl:    cfg.TokenTTL.Duration(),
		store:  st,
	}
}

func (s *Service) Login(username, password, tenantSlug string) (string, *Claims, error) {
	var user *models.User
	var tenantSlugOut string
	var err error

	root, err := s.store.GetRootUser(username)
	if err != nil {
		return "", nil, err
	}
	if root != nil {
		user = root
	} else {
		if tenantSlug == "" {
			tenantSlug = "default"
		}
		tenant, err := s.store.GetTenantBySlug(tenantSlug)
		if err != nil {
			return "", nil, err
		}
		if tenant == nil || !tenant.Enabled {
			return "", nil, errors.New("invalid credentials")
		}
		user, err = s.store.GetUserByUsername(tenant.ID, username)
		if err != nil {
			return "", nil, err
		}
		tenantSlugOut = tenant.Slug
		if user != nil {
			user.TenantID = tenant.ID
		}
	}

	if user == nil || !user.Enabled {
		return "", nil, errors.New("invalid credentials")
	}
	if !s.store.VerifyPassword(user, password) {
		return "", nil, errors.New("invalid credentials")
	}

	claims := &Claims{
		UserID:     user.ID,
		TenantID:   user.TenantID,
		TenantSlug: tenantSlugOut,
		Username:   user.Username,
		Role:       user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Second)),
			Subject:   user.ID,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.secret))
	return signed, claims, err
}

func (s *Service) ParseToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		return []byte(s.secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		claims, err := s.ParseToken(token)
		if err != nil {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		ctx := WithClaims(r.Context(), claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) AdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := ClaimsFromContext(r.Context())
		if claims == nil || (claims.Role != "admin" && !claims.IsRoot()) {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) RootMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := ClaimsFromContext(r.Context())
		if claims == nil || !claims.IsRoot() {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if parts := strings.SplitN(h, " ", 2); len(parts) == 2 && parts[0] == "Bearer" {
			return parts[1]
		}
	}
	return r.URL.Query().Get("token")
}
