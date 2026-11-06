package blog

import (
	"crypto/rand"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"github.com/trentnix/hyperserver/pkg/util"
)

type userPage struct {
	user.User
	Users                                             []user.User
	CurrentID, Error, ErrorFields, InvitationLifetime string
	Active, Pending                                   int
	AllowInvitations                                  bool
	LastAdmin                                         bool
}

func (m *module) renderUsers(w http.ResponseWriter, r *http.Request, view string, data userPage, status int) {
	if view == "userEditor" && data.ID != "" {
		users, err := m.administration.ListUsers(r.Context())
		if err != nil {
			m.storageError(w, r, err)
			return
		}
		var adminID string
		admins := 0
		for _, account := range users {
			if account.Role == string(roleAdmin) && !account.Invited && !account.NeedsVerification() && account.RegistrationAuthType != "" {
				admins++
				adminID = account.ID
			}
		}
		data.LastAdmin = admins == 1 && adminID == data.ID
		if data.LastAdmin {
			data.Role = string(roleAdmin)
		}
	}
	data.AllowInvitations = m.settings.MultiUser
	if account := user.GetUserFromContext(r.Context()); account != nil {
		data.CurrentID = account.ID
	}
	data.InvitationLifetime = m.settings.InviteExpiration.String()
	if m.settings.InviteExpiration%(24*time.Hour) == 0 {
		data.InvitationLifetime = fmt.Sprintf("%d days", m.settings.InviteExpiration/(24*time.Hour))
	}
	page := m.views[view].Content(r, data)
	page.Title, page.ResponseStatusCode = "Users", status
	if err := page.Render(w, r); err != nil {
		page.HandleRenderError(w, r, "Unable to display users.", err)
	}
}

func (m *module) users(w http.ResponseWriter, r *http.Request) {
	users, err := m.administration.ListUsers(r.Context())
	if err != nil {
		m.storageError(w, r, err)
		return
	}
	data := userPage{Users: users}
	for _, u := range users {
		if u.Invited {
			data.Pending++
		} else {
			data.Active++
		}
	}
	m.renderUsers(w, r, "users", data, http.StatusOK)
}

func (m *module) userEditor(w http.ResponseWriter, r *http.Request) {
	data := userPage{User: user.User{Role: string(rolePoster)}}
	if id := r.PathValue("id"); id != "" {
		u, err := m.accounts.GetByID(r.Context(), id)
		if err != nil {
			m.userError(w, r, data, err)
			return
		}
		if u.Role == "" {
			m.userError(w, r, data, user.ErrUserNotFound)
			return
		}
		data.User = *u
	}
	m.renderUsers(w, r, "userEditor", data, http.StatusOK)
}

func (m *module) saveUser(w http.ResponseWriter, r *http.Request) {
	data := userPage{User: user.User{ID: r.PathValue("id")}}
	if err := form.Parse(r); err != nil {
		data.Error = err.Message
		m.renderUsers(w, r, "userEditor", data, err.StatusCode)
		return
	}
	data.Email = strings.TrimSpace(r.PostForm.Get("email"))
	data.Role = r.PostForm.Get("role")
	data.SessionVersion, _ = strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	if data.ID != "" {
		existing, err := m.accounts.GetByID(r.Context(), data.ID)
		if err != nil {
			m.userError(w, r, data, err)
			return
		}
		data.Email, data.Invited = existing.Email, existing.Invited
	}
	switch {
	case !form.ValidEmail(data.Email):
		data.Error, data.ErrorFields = "Enter a valid email address.", "email"
	case !role(data.Role).valid():
		data.Error, data.ErrorFields = "Choose a role.", "role"
	}
	if data.Error != "" {
		m.renderUsers(w, r, "userEditor", data, http.StatusUnprocessableEntity)
		return
	}
	if data.ID == "" {
		if !m.app.Config.Auth.RegistrationEnabled {
			data.Error = "Enable registration before inviting users."
			m.renderUsers(w, r, "userEditor", data, http.StatusUnprocessableEntity)
			return
		}
		u, err := m.accounts.GetByEmail(r.Context(), data.Email)
		if err != nil && !errors.Is(err, user.ErrUserNotFound) {
			m.userError(w, r, data, err)
			return
		}
		if u != nil {
			data.ID, data.SessionVersion = u.ID, u.SessionVersion
		} else {
			data.ID = rand.Text()
		}
		m.inviteUser(w, r, data)
		return
	}
	err := m.administration.ChangeUserRole(r.Context(), user.GetUserFromContext(r.Context()).ID, string(roleAdmin), &data.User)
	if err != nil {
		m.userError(w, r, data, err)
		return
	}
	if data.ID == user.GetUserFromContext(r.Context()).ID && data.Role != string(roleAdmin) {
		util.RedirectToURL(w, r, loginURL)
		return
	}
	util.RedirectToURL(w, r, "/users")
}

func (m *module) inviteUser(w http.ResponseWriter, r *http.Request, data userPage) {
	if m.mail == nil || m.mail.ValidateConfig() != nil {
		if r.PathValue("id") == "" {
			data.ID = ""
		}
		data.Error = "Configure email delivery before inviting users."
		m.renderUsers(w, r, "userEditor", data, http.StatusServiceUnavailable)
		return
	}
	token, err := user.NewInvitationToken(&data.User, m.settings.InviteExpiration)
	if err != nil {
		m.userError(w, r, data, err)
		return
	}
	link, err := util.BuildPublicURL(r, m.app.Config.HTTP, "/invite", map[string]string{"token": token.Token})
	if err != nil {
		m.userError(w, r, data, err)
		return
	}
	if err := m.administration.InviteUser(r.Context(), user.GetUserFromContext(r.Context()).ID, string(roleAdmin), &data.User, token.Metadata()); err != nil {
		m.userError(w, r, data, err)
		return
	}
	body := "<p>You've been invited to " + html.EscapeString(m.profile.Current().Name) + ".</p><p><a href=\"" + html.EscapeString(link.String()) + "\">Accept invitation</a></p><p>Use " + html.EscapeString(data.Email) + " to register or sign in. This link expires " + token.ExpiresAt.UTC().Format("January 2, 2006 at 15:04 UTC") + ".</p>"
	if err := m.mail.Compose().To(data.Email).Subject("Invitation to " + m.profile.Current().Name).Body(body).Send(r.Context()); err != nil {
		logger.LogRequestError(r, err)
		data.Error = "The invitation was saved, but its email could not be sent. Use Resend invitation to try again."
		m.renderUsers(w, r, "userEditor", data, http.StatusBadGateway)
		return
	}
	util.RedirectToURL(w, r, "/users")
}

func (m *module) resendInvitation(w http.ResponseWriter, r *http.Request) {
	if err := form.Parse(r); err != nil {
		http.Error(w, err.Message, err.StatusCode)
		return
	}
	u, err := m.accounts.GetByID(r.Context(), r.PathValue("id"))
	if err != nil {
		m.userError(w, r, userPage{}, err)
		return
	}
	u.SessionVersion, _ = strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	m.inviteUser(w, r, userPage{User: *u})
}

func (m *module) removeUser(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == user.GetUserFromContext(r.Context()).ID {
		m.userError(w, r, userPage{}, user.ErrRemoveSelf)
		return
	}
	u, err := m.accounts.GetByID(r.Context(), r.PathValue("id"))
	if err != nil {
		m.userError(w, r, userPage{}, err)
		return
	}
	if r.Method == http.MethodGet {
		m.renderUsers(w, r, "userRemove", userPage{User: *u}, http.StatusOK)
		return
	}
	if err := form.Parse(r); err != nil {
		http.Error(w, err.Message, err.StatusCode)
		return
	}
	u.SessionVersion, _ = strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	u.Role = ""
	if err := m.administration.ChangeUserRole(r.Context(), user.GetUserFromContext(r.Context()).ID, string(roleAdmin), u); err != nil {
		m.userError(w, r, userPage{User: *u}, err)
		return
	}
	util.RedirectToURL(w, r, "/users")
}

func (m *module) userError(w http.ResponseWriter, r *http.Request, data userPage, err error) {
	if r.PathValue("id") == "" {
		data.ID = ""
	}
	switch {
	case errors.Is(err, user.ErrUserNotFound):
		m.storageError(w, r, errNotFound)
		return
	case errors.Is(err, user.ErrAccessDenied), errors.Is(err, user.ErrRemoveSelf):
		m.app.ContentManager.HandleError(w, r, err.Error(), nil, http.StatusForbidden)
		return
	case errors.Is(err, user.ErrLastAdministrator), errors.Is(err, user.ErrAlreadyActive):
		data.Error = err.Error()
	case errors.Is(err, user.ErrEmailAlreadyExists), errors.Is(err, user.ErrUserChanged):
		data.Error = "This user's access changed. Reload the page before saving."
	default:
		m.storageError(w, r, err)
		return
	}
	m.renderUsers(w, r, "userEditor", data, http.StatusConflict)
}
