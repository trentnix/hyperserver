package auth

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/components/content"
	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// admitAccount renders admission failures through the same forms as credential
// failures. Policy errors are operational failures, not user-facing explanations.
func (a *EmailAuthService) admitAccount(w http.ResponseWriter, r *http.Request, email string, page *content.Content, f form.ErrorForm) bool {
	if a.allowAccount == nil {
		return true
	}
	allowed := false
	err := r.Context().Err()
	if err == nil {
		allowed, err = a.allowAccount(r.Context(), email)
		err = errors.Join(err, r.Context().Err())
	}
	if err != nil {
		logger.Audit(r.Context(), "account_admission", "failed")
		logger.LogRequestError(r, fmt.Errorf("account admission: %w", err))
		page.ResponseStatusCode = http.StatusServiceUnavailable
		form.HandleFormError(w, r, page, f, "Unable to check account access. Please try again.", nil)
		return false
	}
	if !allowed {
		logger.Audit(r.Context(), "account_admission", "rejected")
		form.HandleFormError(w, r, page, f, "Account access is not available for this email address.", nil)
		return false
	}
	return true
}
