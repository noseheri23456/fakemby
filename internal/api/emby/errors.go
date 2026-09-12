package emby

import "net/http"

type EmbyError struct {
	StatusCode int    `json:"StatusCode"`
	Message    string `json:"Message"`
}

func NewError(statusCode int, message string) EmbyError {
	return EmbyError{
		StatusCode: statusCode,
		Message:    message,
	}
}

var (
	ErrUnauthorized       = NewError(401, "Unauthorized")
	ErrForbidden          = NewError(403, "Forbidden")
	ErrNotFound           = NewError(404, "Not found")
	ErrBadRequest         = NewError(400, "Bad request")
	ErrInternal           = NewError(500, "Internal server error")
	ErrInvalidToken       = NewError(401, "Invalid or expired token")
	ErrInvalidCredentials = NewError(401, "Invalid username or password")
)

func HTTPStatusCode(statusCode int) int {
	switch statusCode {
	case 400:
		return http.StatusBadRequest
	case 401:
		return http.StatusUnauthorized
	case 403:
		return http.StatusForbidden
	case 404:
		return http.StatusNotFound
	case 500:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
