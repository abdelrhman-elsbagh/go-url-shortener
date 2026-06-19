package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/abdelrahmantarek/go-url-shortener/internal/service"
	"github.com/gin-gonic/gin"
)

type encodeRequest struct {
	URL string `json:"url"`
}

type encodeResponse struct {
	ShortURL    string `json:"short_url"`
	ShortCode   string `json:"short_code"`
	OriginalURL string `json:"original_url"`
	CreatedAt   string `json:"created_at"`
}

// EncodeHandler handles POST /api/v1/encode.
type EncodeHandler struct {
	svc     service.Service
	fullURL func(code string) string
	logger  *slog.Logger
}

func NewEncodeHandler(svc service.Service, fullURL func(string) string, logger *slog.Logger) *EncodeHandler {
	return &EncodeHandler{svc: svc, fullURL: fullURL, logger: logger}
}

// Handle encodes a long URL into a short code.
//
// @Summary      Encode URL
// @Description  Creates or returns an idempotent short URL for a valid public HTTP/HTTPS URL.
// @Tags         URL
// @Accept       json
// @Produce      json
// @Param        request  body      EncodeRequestDoc   true  "URL to encode"
// @Success      200      {object}  EncodeResponseDoc
// @Failure      400      {object}  ErrorResponseDoc
// @Failure      422      {object}  ErrorResponseDoc
// @Failure      429      {object}  ErrorResponseDoc
// @Failure      500      {object}  ErrorResponseDoc
// @Router       /api/v1/encode [post]
func (h *EncodeHandler) Handle(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20) // 1 MB

	var req encodeRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_URL", "bad request body", err.Error())
		return
	}

	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		writeError(c, http.StatusBadRequest, "INVALID_URL", "url is required", "")
		return
	}

	// block javascript: and data: before we even parse
	lower := strings.ToLower(req.URL)
	if strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "data:") {
		writeError(c, http.StatusBadRequest, "INVALID_URL", "scheme not allowed", "javascript: and data: are forbidden")
		return
	}

	record, err := h.svc.Encode(c.Request.Context(), req.URL)
	if err != nil {
		if errors.Is(err, service.ErrInvalidURL) {
			msg := err.Error()
			// SSRF-blocked URLs get 422; plain bad format gets 400
			if strings.Contains(msg, "private") || strings.Contains(msg, "resolve") {
				writeError(c, http.StatusUnprocessableEntity, "INVALID_URL", "URL failed validation", msg)
				return
			}
			writeError(c, http.StatusBadRequest, "INVALID_URL", "invalid URL", msg)
			return
		}
		h.logger.ErrorContext(c.Request.Context(), "encode error", slog.String("err", err.Error()))
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong", "")
		return
	}

	writeJSON(c, http.StatusOK, encodeResponse{
		ShortURL:    h.fullURL(record.ShortCode),
		ShortCode:   record.ShortCode,
		OriginalURL: record.OriginalURL,
		CreatedAt:   record.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}
