package api

import (
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

// verifyStepUp re-checks the signed-in administrator's password before a
// sensitive agent change (creating tokens, widening grants, changing the agent
// deploy level). It uses the same password verification and limiter as
// password changes, keyed by account and peer IP, so step-up guesses never
// consume the login budget. Failures answer 403, never 401, so the editor
// does not mistake a typo for an expired session. It writes the response and
// returns false when the request must stop.
func (h *AuthHandler) verifyStepUp(w http.ResponseWriter, r *http.Request, password string) bool {
	session, ok := GetUserFromContext(r.Context())
	if !ok || session == nil {
		writeError(w, http.StatusUnauthorized, "Sign in to continue")
		return false
	}
	if password == "" {
		writeError(w, http.StatusForbidden, "Confirm your password to continue")
		return false
	}
	ip := peerIP(r)
	if !reserve(w, h.password, ip, session.Username) {
		return false
	}
	user, err := h.store.GetUser(session.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Cannot read account")
		return false
	}
	hash := dummyBcryptHash
	if user != nil {
		hash = []byte(user.PasswordHash)
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || user == nil {
		h.password.failure(ip, session.Username)
		writeError(w, http.StatusForbidden, "The password is incorrect")
		return false
	}
	h.password.success(ip, session.Username)
	return true
}
