package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
)

type ctxKey string

const ctxReqID ctxKey = "req_id"

func RequestID(c *gin.Context) {
	id := c.GetHeader("X-Request-ID")
	if id == "" {
		id = randHex(8)
	}
	c.Header("X-Request-ID", id)
	c.Set(string(ctxReqID), id)
	// also store in the standard request context so ReqIDFromCtx works
	c.Request = c.Request.WithContext(
		context.WithValue(c.Request.Context(), ctxReqID, id))
	c.Next()
}

func ReqIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxReqID).(string)
	return v
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
