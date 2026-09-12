package admin

import (
	api "github.com/fakemby/fakemby/internal/api/emby"
	"github.com/google/uuid"
)

var adminAuth = api.RequireAdminAPIKey
var FlushProgress = api.FlushProgress
var GetUserPolicy = api.GetUserPolicy

type UserPolicy = api.UserPolicy

var ErrBadRequest = api.ErrBadRequest
var ErrInternal = api.ErrInternal
var ErrNotFound = api.ErrNotFound
var ErrUnauthorized = api.ErrUnauthorized

func newShortID() string { return uuid.NewString() }
