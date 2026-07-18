package identity

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/team-everfrost/remak-go/internal/platform/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Routes(authenticate func(http.Handler) http.Handler) chi.Router {
	router := chi.NewRouter()
	router.Post("/signup-code", h.requestSignupCode)
	router.Post("/verify-code", h.verifySignupCode)
	router.Post("/local/signup", h.signup)
	router.Post("/local/login", h.login)
	router.Post("/refresh", h.refresh)
	router.Post("/local/logout", h.logout)
	router.Post("/check-email", h.checkEmail)
	router.Post("/reset-code", h.requestResetCode)
	router.Post("/verify-reset-code", h.verifyResetCode)
	router.Post("/reset-password", h.resetPassword)
	router.Group(func(protected chi.Router) {
		protected.Use(authenticate)
		protected.Post("/withdraw-code", h.requestWithdrawCode)
		protected.Post("/verify-withdraw-code", h.verifyWithdrawCode)
		protected.Post("/withdraw", h.withdraw)
	})
	return router
}

func (h *Handler) requestSignupCode(w http.ResponseWriter, r *http.Request) {
	var input RequestCodeInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.RequestSignupCode(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) verifySignupCode(w http.ResponseWriter, r *http.Request) {
	var input VerifyCodeInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.VerifySignupCode(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	var input SignupInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.Signup(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, result)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var input LoginInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.Login(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.Refresh(r.Context(), input.RefreshToken)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RefreshToken string `json:"refreshToken"`
	}
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(w, r, &input); err != nil {
			httpx.WriteError(w, err)
			return
		}
	}
	if err := h.service.Logout(r.Context(), input.RefreshToken); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct{}{})
}

func (h *Handler) checkEmail(w http.ResponseWriter, r *http.Request) {
	var input RequestCodeInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	// Deliberately return a neutral response to prevent account enumeration.
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"available": true})
}

func (h *Handler) requestResetCode(w http.ResponseWriter, r *http.Request) {
	var input RequestCodeInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.RequestResetCode(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) verifyResetCode(w http.ResponseWriter, r *http.Request) {
	var input VerifyCodeInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.VerifyResetCode(r.Context(), input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var input CompleteChallengeInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := h.service.ResetPassword(r.Context(), input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, nil)
}

func (h *Handler) requestWithdrawCode(w http.ResponseWriter, r *http.Request) {
	actor := httpx.ActorFromContext(r.Context())
	result, err := h.service.RequestWithdrawCode(r.Context(), actor.AccountID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) verifyWithdrawCode(w http.ResponseWriter, r *http.Request) {
	var input VerifyCodeInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	actor := httpx.ActorFromContext(r.Context())
	result, err := h.service.VerifyWithdrawCode(r.Context(), actor.AccountID, input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) withdraw(w http.ResponseWriter, r *http.Request) {
	var input CompleteChallengeInput
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(w, r, &input); err != nil {
			httpx.WriteError(w, err)
			return
		}
	}
	actor := httpx.ActorFromContext(r.Context())
	if err := h.service.Withdraw(r.Context(), actor.AccountID, input.VerificationToken); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, nil)
}
