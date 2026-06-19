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

type decodeRequest struct {
	ShortCode string `json:"short_code"`
}

type decodeResponse struct {
	OriginalURL string `json:"original_url"`
	ShortCode   string `json:"short_code"`
	CreatedAt   string `json:"created_at"`
	ClickCount  int64  `json:"click_count"`
}

// DecodeHandler handles POST /api/v1/decode.
type DecodeHandler struct {
	svc    service.Service
	logger *slog.Logger
}

func NewDecodeHandler(svc service.Service, logger *slog.Logger) *DecodeHandler {
	return &DecodeHandler{svc: svc, logger: logger}
}

// Handle decodes a short code and returns URL metadata.
//
// @Summary      Decode short code
// @Description  Looks up the original URL and metadata for a short code.
// @Tags         URL
// @Accept       json
// @Produce      json
// @Param        request  body      DecodeRequestDoc   true  "Short code payload"
// @Success      200      {object}  DecodeResponseDoc
// @Failure      400      {object}  ErrorResponseDoc
// @Failure      404      {object}  ErrorResponseDoc
// @Failure      410      {object}  ErrorResponseDoc
// @Failure      429      {object}  ErrorResponseDoc
// @Failure      500      {object}  ErrorResponseDoc
// @Router       /api/v1/decode [post]
func (h *DecodeHandler) Handle(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)

	var req decodeRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_URL", "bad request body", err.Error())
		return
	}

	req.ShortCode = strings.TrimSpace(req.ShortCode)
	if req.ShortCode == "" {
		writeError(c, http.StatusBadRequest, "INVALID_URL", "short_code is required", "")
		return
	}

	h.lookup(c, req.ShortCode)
}

func (h *DecodeHandler) lookup(c *gin.Context, code string) {
	record, err := h.svc.Decode(c.Request.Context(), code)
	if err != nil {
		decodeErr(c, err)
		return
	}
	writeJSON(c, http.StatusOK, decodeResponse{
		OriginalURL: record.OriginalURL,
		ShortCode:   record.ShortCode,
		CreatedAt:   record.CreatedAt.Format("2006-01-02T15:04:05Z"),
		ClickCount:  record.ClickCount,
	})
}

func decodeErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrURLNotFound):
		writeError(c, http.StatusNotFound, "URL_NOT_FOUND", "short code not found", "")
	case errors.Is(err, service.ErrURLExpired):
		writeError(c, http.StatusGone, "URL_EXPIRED", "this link has expired", "")
	case errors.Is(err, service.ErrInvalidURL):
		writeError(c, http.StatusBadRequest, "INVALID_URL", "invalid short code", err.Error())
	default:
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong", "")
	}
}
