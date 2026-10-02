// Package emby preserves the legacy API entry points.
// Deprecated: use internal/api/emby and internal/api/admin.
package emby

import (
	admin "github.com/fakemby/fakemby/internal/api/admin"
	api "github.com/fakemby/fakemby/internal/api/emby"
	"github.com/fakemby/fakemby/internal/types"
)

// DTO 已迁到 internal/types（2026-10-03 审查 P3-8）；这里只做转发，
// 保持旧包路径继续可用。
var DefaultUserConfig = types.DefaultUserConfig
var GetUserPolicy = api.GetUserPolicy
var RegisterAuthRoutes = api.RegisterAuthRoutes
var AuthTokenMiddleware = api.AuthTokenMiddleware

type AuthenticateRequest = types.AuthenticateRequest
type AuthenticateResponse = types.AuthenticateResponse
type UserDTO = types.UserDTO
type UserPolicy = types.UserPolicy
type UserConfig = types.UserConfig
type SessionInfo = types.SessionInfo
type PlayState = types.PlayState
type Capabilities = types.Capabilities
type PublicUserDTO = types.PublicUserDTO

var RequireUserMatch = api.RequireUserMatch
var RegisterCompatRoutes = api.RegisterCompatRoutes
var NewError = api.NewError
var HTTPStatusCode = api.HTTPStatusCode

type EmbyError = api.EmbyError

var RegisterImageRoutes = api.RegisterImageRoutes
var GetImageTag = api.GetImageTag
var GetImageTagsForItem = api.GetImageTagsForItem
var RegisterItemRoutes = api.RegisterItemRoutes
var RequestLogMiddleware = api.RequestLogMiddleware
var ErrorHandlerMiddleware = api.ErrorHandlerMiddleware
var CORSMiddleware = api.CORSMiddleware
var ShutdownWebSockets = api.ShutdownWebSockets
var RegisterWebSocketRoutes = api.RegisterWebSocketRoutes
var OperationsMiddleware = api.OperationsMiddleware
var RegisterOperations = api.RegisterOperations
var RegisterPlaybackRoutes = api.RegisterPlaybackRoutes

type PlaybackInfoRequest = api.PlaybackInfoRequest
type PlaybackInfoResponse = api.PlaybackInfoResponse

var RegisterSearchRoutes = api.RegisterSearchRoutes

type SearchHintsRequest = api.SearchHintsRequest
type SearchHintsResponse = api.SearchHintsResponse

var InitProgressBuffer = api.InitProgressBuffer
var FlushProgressNow = api.FlushProgressNow
var FlushProgress = api.FlushProgress
var BufferProgress = api.BufferProgress
var ShutdownProgressBuffer = api.ShutdownProgressBuffer
var RegisterSessionRoutes = api.RegisterSessionRoutes

type NowPlayingItem = types.NowPlayingItem
type PlayingRequest = api.PlayingRequest
type StoppedRequest = api.StoppedRequest
type ProgressRequest = api.ProgressRequest
type ProgressBuffer = api.ProgressBuffer

var RegisterShowRoutes = api.RegisterShowRoutes
var RegisterStatsRoutes = api.RegisterStatsRoutes

type CustomQueryRequest = api.CustomQueryRequest

var RegisterSystemRoutes = api.RegisterSystemRoutes

type SystemInfoPublic = api.SystemInfoPublic
type SystemInfo = api.SystemInfo
type SystemConfiguration = api.SystemConfiguration
type SystemEndpointInfo = api.SystemEndpointInfo

var RegisterUserDataRoutes = api.RegisterUserDataRoutes
var RegisterUserRoutes = api.RegisterUserRoutes
var RequireAdmin = api.RequireAdmin
var RegisterAdminExtraRoutes = admin.RegisterAdminExtraRoutes
var RegisterAdminUserRoutes = admin.RegisterAdminUserRoutes

type CreateUserRequest = admin.CreateUserRequest
type UserResponse = admin.UserResponse
type StatsResponse = admin.StatsResponse
type ChangePasswordRequest = admin.ChangePasswordRequest

var RegisterImportRoutes = admin.RegisterImportRoutes

type ImportRequest = admin.ImportRequest
type ImportItem = admin.ImportItem
type ImportPerson = admin.ImportPerson
type ImportExternalUrl = admin.ImportExternalUrl
type ImportSeason = admin.ImportSeason
type ImportEpisode = admin.ImportEpisode
type ImportSource = admin.ImportSource
type ImportSubtitle = admin.ImportSubtitle
type ImportResponse = admin.ImportResponse

var RegisterAdminItemRoutes = admin.RegisterAdminItemRoutes

type CreateItemRequest = admin.CreateItemRequest
type AddSourceRequest = admin.AddSourceRequest

var ErrUnauthorized = api.ErrUnauthorized
var ErrForbidden = api.ErrForbidden
var ErrNotFound = api.ErrNotFound
var ErrBadRequest = api.ErrBadRequest
var ErrInternal = api.ErrInternal
var ErrInvalidToken = api.ErrInvalidToken
var ErrInvalidCredentials = api.ErrInvalidCredentials

var RegisterMSGOCompatRoutes = api.RegisterMSGOCompatRoutes
