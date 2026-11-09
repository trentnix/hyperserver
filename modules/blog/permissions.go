package blog

import (
	"context"
	"errors"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type role string

const (
	roleAdmin  role = "admin"
	rolePoster role = "poster"
)

func (r role) valid() bool { return r == roleAdmin || r == rolePoster }

var errAccessDenied = errors.New("You don't have permission to make this change.")

func (m *module) authorizeAdminWrite(ctx context.Context) error {
	permission, err := m.access.Role(ctx, user.GetUserFromContext(ctx))
	if err != nil {
		return err
	}
	if permission != roleAdmin {
		return errAccessDenied
	}
	return nil
}

// Role uses the account loaded and version-checked by authentication for this
// request. Access changes take effect on the next request. User-management
// mutations also recheck authority inside their write transaction.
func (a *Access) Role(ctx context.Context, account *user.User) (role, error) {
	if a == nil || account == nil || account.ID == "" || account.Invited || account.NeedsVerification() {
		return "", nil
	}
	// Registration creates credentials. Initial setup assigns the first role.
	// Storage refuses this assignment once any account has a role.
	if account.Role == "" && user.EmailKey(account.Email) == a.emailKey {
		if err := a.administration.InitializeRole(ctx, account.Email, string(roleAdmin)); err != nil {
			return "", err
		}
		current, err := a.accounts.GetByID(ctx, account.ID)
		if err != nil {
			return "", err
		}
		*account = *current
		if account.Invited || account.NeedsVerification() {
			return "", nil
		}
	}
	return role(account.Role), nil
}

func (m *module) requestRole(r *http.Request) role {
	permission, err := m.access.Role(r.Context(), user.GetUserFromContext(r.Context()))
	if err != nil {
		logger.LogRequestError(r, err)
	}
	return permission
}

func (m *module) requireRole(next http.HandlerFunc, allowed func(role) bool) http.Handler {
	return m.requireSetup(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), adminPageKey{}, true))
		w.Header().Set("Cache-Control", "no-store")
		permission, err := m.access.Role(r.Context(), user.GetUserFromContext(r.Context()))
		if err != nil {
			m.app.ContentManager.HandleError(w, r, "Unable to check your site access. Please try again.", err, http.StatusServiceUnavailable)
			return
		}
		if !allowed(permission) {
			m.app.ContentManager.HandleError(w, r, "You don't have permission to access this page.", nil, http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func (m *module) requireWriter(next http.HandlerFunc) http.Handler {
	return m.requireRole(next, func(r role) bool { return r.valid() })
}

func (m *module) canEditPost(ctx context.Context, p Post) bool {
	account := user.GetUserFromContext(ctx)
	permission, err := m.access.Role(ctx, account)
	return err == nil && (permission == roleAdmin || permission == rolePoster && account != nil && p.AuthorID == account.ID)
}

func (m *module) requirePostOwner(next http.HandlerFunc) http.Handler {
	return m.requireWriter(func(w http.ResponseWriter, r *http.Request) {
		if id := r.PathValue("id"); id != "" {
			post, err := m.posts.GetByID(r.Context(), id, true)
			if err != nil {
				m.storageError(w, r, err)
				return
			}
			if !m.canEditPost(r.Context(), post) {
				m.app.ContentManager.HandleError(w, r, "You can manage only your own posts.", nil, http.StatusForbidden)
				return
			}
		}
		next(w, r)
	})
}

func (m *module) requireHistoryOwner(next http.HandlerFunc) http.Handler {
	post, page := m.requirePostOwner(next), m.requireAdmin(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("kind") == "post" {
			post.ServeHTTP(w, r)
		} else {
			page.ServeHTTP(w, r)
		}
	})
}
