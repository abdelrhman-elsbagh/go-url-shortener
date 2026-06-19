package handler

import "github.com/gin-gonic/gin"

type errResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

func writeJSON(c *gin.Context, status int, v any) {
	c.JSON(status, v)
}

func writeError(c *gin.Context, status int, code, message, details string) {
	c.JSON(status, errResponse{Error: apiError{
		Code:    code,
		Message: message,
		Details: details,
	}})
}
