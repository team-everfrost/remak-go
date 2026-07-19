package library

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/team-everfrost/remak-go/internal/platform/httpx"
	"github.com/team-everfrost/remak-go/internal/retrieval"
)

type Handler struct {
	service   *Service
	retrieval *retrieval.Service
	files     FileService
}

type FileService interface {
	Upload(w http.ResponseWriter, r *http.Request, ownerID uuid.UUID) ([]Document, error)
	DownloadURL(ctx context.Context, ownerID, documentID uuid.UUID) (string, error)
}

func NewHandler(service *Service, retrievalService *retrieval.Service, files FileService) *Handler {
	return &Handler{service: service, retrieval: retrievalService, files: files}
}

func (h *Handler) DocumentRoutes() chi.Router {
	router := chi.NewRouter()
	router.Get("/", h.listDocuments)
	router.Post("/file", h.uploadFiles)
	router.Get("/file/{documentID}", h.downloadFile)
	router.Post("/memo", h.createMemo)
	router.Patch("/memo/{documentID}", h.updateMemo)
	router.Post("/webpage", h.createWebpage)
	router.Patch("/webpage/{documentID}", h.updateWebpage)
	router.Get("/search/tag", h.searchByTag)
	router.Get("/search/collection", h.searchByCollection)
	router.Get("/search/text", h.searchText)
	router.Get("/search/hybrid", h.searchHybrid)
	router.Get("/{documentID}", h.getDocument)
	router.Delete("/{documentID}", h.deleteDocument)
	return router
}

func (h *Handler) uploadFiles(w http.ResponseWriter, r *http.Request) {
	result, err := h.files.Upload(w, r, actorID(r))
	writeResult(w, http.StatusCreated, result, err)
}

func (h *Handler) downloadFile(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parsePathUUID(w, r, "documentID")
	if !ok {
		return
	}
	result, err := h.files.DownloadURL(r.Context(), actorID(r), documentID)
	writeResult(w, http.StatusOK, result, err)
}

func (h *Handler) TagRoutes() chi.Router {
	router := chi.NewRouter()
	router.Get("/", h.listTags)
	return router
}

func (h *Handler) CollectionRoutes() chi.Router {
	router := chi.NewRouter()
	router.Get("/", h.listCollections)
	router.Post("/", h.createCollection)
	router.Post("/add/{name}", h.addDocumentsToCollection)
	router.Get("/{name}", h.getCollection)
	router.Patch("/{name}", h.updateCollection)
	router.Delete("/{name}", h.deleteCollection)
	return router
}

func (h *Handler) createMemo(w http.ResponseWriter, r *http.Request) {
	var input MemoInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.CreateMemo(r.Context(), actorID(r), input)
	writeResult(w, http.StatusCreated, result, err)
}

func (h *Handler) createWebpage(w http.ResponseWriter, r *http.Request) {
	var input WebpageInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.CreateWebpage(r.Context(), actorID(r), httpx.RequestIDFromContext(r.Context()), input)
	writeResult(w, http.StatusCreated, result, err)
}

func (h *Handler) updateMemo(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parsePathUUID(w, r, "documentID")
	if !ok {
		return
	}
	var input MemoInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.UpdateMemo(r.Context(), actorID(r), documentID, input)
	writeResult(w, http.StatusOK, result, err)
}

func (h *Handler) updateWebpage(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parsePathUUID(w, r, "documentID")
	if !ok {
		return
	}
	var input WebpageInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.UpdateWebpage(
		r.Context(),
		actorID(r),
		documentID,
		httpx.RequestIDFromContext(r.Context()),
		input,
	)
	writeResult(w, http.StatusOK, result, err)
}

func (h *Handler) getDocument(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parsePathUUID(w, r, "documentID")
	if !ok {
		return
	}
	result, err := h.service.Get(r.Context(), actorID(r), documentID)
	writeResult(w, http.StatusOK, result, err)
}

func (h *Handler) deleteDocument(w http.ResponseWriter, r *http.Request) {
	documentID, ok := parsePathUUID(w, r, "documentID")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), actorID(r), documentID); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, nil)
}

func (h *Handler) listDocuments(w http.ResponseWriter, r *http.Request) {
	cursor, err := cursorFromRequest(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.List(r.Context(), actorID(r), cursor)
	writePage(w, result, err)
}

func (h *Handler) searchByTag(w http.ResponseWriter, r *http.Request) {
	cursor, err := cursorFromRequest(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.ListByTag(r.Context(), actorID(r), r.URL.Query().Get("tagName"), cursor)
	writePage(w, result, err)
}

func (h *Handler) searchByCollection(w http.ResponseWriter, r *http.Request) {
	cursor, err := cursorFromRequest(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.ListByCollection(r.Context(), actorID(r), r.URL.Query().Get("collectionName"), cursor)
	writePage(w, result, err)
}

func (h *Handler) searchText(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.SearchText(
		r.Context(),
		actorID(r),
		r.URL.Query().Get("query"),
		queryInt32(r, "limit", 20),
		queryInt32(r, "offset", 0),
	)
	writeResult(w, http.StatusOK, result, err)
}

func (h *Handler) searchHybrid(w http.ResponseWriter, r *http.Request) {
	result, err := h.retrieval.Search(
		r.Context(),
		actorID(r),
		r.URL.Query().Get("query"),
		int(queryInt32(r, "limit", 20)),
	)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	documents, err := h.service.GetMany(r.Context(), actorID(r), result.DocumentIDs)
	writeResult(w, http.StatusOK, documents, err)
}

func (h *Handler) listTags(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListTags(
		r.Context(),
		actorID(r),
		r.URL.Query().Get("query"),
		queryInt32(r, "limit", 20),
		queryInt32(r, "offset", 0),
	)
	writeResult(w, http.StatusOK, result, err)
}

func (h *Handler) listCollections(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListCollections(
		r.Context(),
		actorID(r),
		queryInt32(r, "limit", 20),
		queryInt32(r, "offset", 0),
	)
	writeResult(w, http.StatusOK, result, err)
}

func (h *Handler) getCollection(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetCollection(r.Context(), actorID(r), chi.URLParam(r, "name"))
	writeResult(w, http.StatusOK, result, err)
}

func (h *Handler) createCollection(w http.ResponseWriter, r *http.Request) {
	var input CreateCollectionInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.CreateCollection(r.Context(), actorID(r), input)
	writeResult(w, http.StatusCreated, result, err)
}

func (h *Handler) addDocumentsToCollection(w http.ResponseWriter, r *http.Request) {
	var input AddDocumentsInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	err := h.service.AddDocumentsToCollection(
		r.Context(), actorID(r), chi.URLParam(r, "name"), input.DocIDs,
	)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, nil)
}

func (h *Handler) updateCollection(w http.ResponseWriter, r *http.Request) {
	var input UpdateCollectionInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.UpdateCollection(r.Context(), actorID(r), chi.URLParam(r, "name"), input)
	writeResult(w, http.StatusOK, result, err)
}

func (h *Handler) deleteCollection(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteCollection(r.Context(), actorID(r), chi.URLParam(r, "name")); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, nil)
}

func cursorFromRequest(r *http.Request) (Cursor, error) {
	query := r.URL.Query()
	documentID := query.Get("doc-id")
	if documentID == "" {
		documentID = query.Get("docid")
	}
	if opaque := query.Get("page-token"); opaque != "" {
		decoded, err := DecodeCursor(opaque)
		if err != nil {
			return Cursor{}, httpx.BadRequest("invalid_page_cursor", "페이지 커서가 올바르지 않습니다")
		}
		documentID = decoded
	}
	if documentID != "" {
		if _, err := uuid.Parse(documentID); err != nil {
			return Cursor{}, httpx.BadRequest("invalid_page_cursor", "페이지 커서가 올바르지 않습니다")
		}
	}
	var cursorTime *time.Time
	if raw := query.Get("cursor"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return Cursor{}, httpx.BadRequest("invalid_page_cursor", "페이지 커서가 올바르지 않습니다")
		}
		cursorTime = &parsed
	}
	return Cursor{Time: cursorTime, DocID: documentID, Limit: queryInt32(r, "limit", 20)}, nil
}

func writePage(w http.ResponseWriter, result []Document, err error) {
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if next := EncodeNextCursor(result); next != "" {
		w.Header().Set("X-Next-Cursor", next)
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func parsePathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		httpx.WriteError(w, httpx.BadRequest("invalid_document_id", "문서 ID 형식이 올바르지 않습니다"))
		return uuid.Nil, false
	}
	return parsed, true
}

func queryInt32(r *http.Request, name string, fallback int32) int32 {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return fallback
	}
	return int32(parsed)
}

func actorID(r *http.Request) uuid.UUID {
	return httpx.ActorFromContext(r.Context()).AccountID
}

func writeResult(w http.ResponseWriter, status int, result any, err error) {
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, status, result)
}
