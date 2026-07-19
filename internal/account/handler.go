package account

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/team-everfrost/remak-go/internal/platform/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Routes() chi.Router {
	router := chi.NewRouter()
	router.Get("/", h.get)
	router.Patch("/", h.update)
	router.Get("/storage/usage", h.storageUsage)
	router.Get("/storage/size", h.storageLimit)
	return router
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Get(r.Context(), httpx.ActorFromContext(r.Context()).AccountID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var input UpdateProfileInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.Update(r.Context(), httpx.ActorFromContext(r.Context()).AccountID, input)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) storageUsage(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.StorageUsage(r.Context(), httpx.ActorFromContext(r.Context()).AccountID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) storageLimit(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.StorageLimit(r.Context(), httpx.ActorFromContext(r.Context()).AccountID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}
