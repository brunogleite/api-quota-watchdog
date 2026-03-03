# API Quota Watchdog — Agent Memory

## Package Layout (confirmed, compiles clean — post multi-tenant refactor)
```
internal/
  apperror/   apperror.go   — AppError{Code,Message,Err}, New(), Write()
  store/      store.go      — Store, New(), all DB methods (fully implemented)
  quota/      quota.go      — Enforcer, Check(), Record(), ThresholdExceeded() (pure fn)
  proxy/      proxy.go      — Proxy, Forward(), stripHopByHop() — no store field
  alert/      alert.go      — Dispatcher, Dispatch() (fire-and-forget goroutine)
  sse/        hub.go        — Hub, Register(), Deregister(), Broadcast()
  mock/       mock.go       — Responder, NewResponder(), Respond()
  middleware/ auth.go       — Auth(), ClaimsFromContext(), UserIDFromContext()
  handler/    auth.go       — AuthHandler, ServeRegister(), ServeLogin()
              provider.go   — ProviderHandler, ServeCreate(), ServeList(), ServeDelete()
              proxy.go      — ProxyHandler, ServeProxy(), capturingResponseWriter
  server/     server.go     — Server, NewServer(*sql.DB, []byte jwtSecret), ServeHTTP()
              routes.go     — routes() wires all endpoints
main.go                     — slog init, JWT guard, DB open, graceful shutdown
```
NOTE: config/ package was deleted in multi-tenant refactor. No config.yaml.

## Approved Dependencies (go.mod — post refactor)
- `github.com/golang-jwt/jwt/v5 v5.2.1`
- `github.com/lib/pq v1.10.9`
- `golang.org/x/crypto v0.48.0` — bcrypt for password hashing (handler/auth.go)
- NOTE: `gopkg.in/yaml.v3` was removed when config package was deleted

## Key Architectural Decisions
- **AppError boundary**: store/proxy/quota return plain `error`; only handler/ constructs AppError
- **Config package deleted**: no YAML config; all provider config is in the DB (multi-tenant)
- **Proxy**: hand-rolled with `http.Client`; never `httputil.ReverseProxy`; no store field
- **API key injection removed**: clients supply credentials in request headers; proxy forwards as-is
- **Mock responder**: provider.MockEnabled=true routes to mock.Responder instead of real upstream
- **Quota**: synchronous DB write on every proxied request; no in-memory cache
- **Alerts**: edge-triggered; crossing state tracked in DB (`quotas.threshold_crossed`)
- **SSE Broadcast**: copies client map under lock, then sends with `select/default` (never blocks)
- **JWT**: HS256 only; secret from `WATCHDOG_JWT_SECRET` env var; passed to NewServer() not read internally
- **Logging**: `log/slog` with `JSONHandler`, initialized once in `main.go`
- **Multi-tenancy**: providers scoped to user_id; GetProviderByName takes (ctx, userID, name)

## Environment Variables
- `WATCHDOG_JWT_SECRET` — required; server fatal-exits if absent; read only in main.go
- `DATABASE_URL` — PostgreSQL DSN; required
- `WATCHDOG_WEBHOOK_URL` — optional; read in server.NewServer() internally

## NewServer Signature
`NewServer(db *sql.DB, jwtSecret []byte)` — jwtSecret injected from main.go

## Store Methods (confirmed, post migration 002)
- `GetProviderByName(ctx, userID int64, name string) (Provider, error)`
- `RecordUsage(ctx, providerID, serviceID int64, method string, statusCode int, latencyMs int64) error`
- `GetQuotaUsage(ctx, providerID int64) (used, limit int64, err error)`
- `GetQuotaThresholdCrossed(ctx, providerID int64) (bool, error)`
- `SetQuotaThresholdCrossed(ctx, providerID int64, crossed bool) error`
- `CreateUser(ctx, email, passwordHash string) (int64, error)`
- `GetUserByEmail(ctx, email string) (User, error)`
- `CreateProvider(ctx, userID int64, name, baseURL, apiKeyHeader string, mockEnabled bool, requestLimit int64) (Provider, error)` — single transaction
- `ListProviders(ctx, userID int64) ([]Provider, error)`
- `DeleteProvider(ctx, userID, providerID int64) error`

## Provider Struct (post migration 002)
```go
type Provider struct {
    ID, UserID   int64
    Name, BaseURL, APIKeyHeader string
    MockEnabled  bool
    // APIKeyValue removed — clients supply credentials
}
```

## Routes
- `POST /auth/register` — unauthenticated
- `POST /auth/login` — unauthenticated
- `POST /providers` — authenticated
- `GET /providers` — authenticated
- `DELETE /providers/{id}` — authenticated
- `POST /proxy/{provider}/{path...}` — authenticated

## UserIDFromContext Pattern
- JWT MapClaims numeric values are float64; cast to int64 explicitly
- Located in `internal/middleware/auth.go`

## bcrypt / JWT Config
- Cost: 12 (constant `bcryptCost` in handler/auth.go)
- JWT expiry: 24h; claims: `{user_id, email, exp}`

## Mock Responder
- openai→200, twilio→201, googlemaps→200, default→200 `{}`
- Matching: case-insensitive, whitespace-trimmed

## Goroutine Ownership
- `srv.ListenAndServe` goroutine — owned by main, stopped by srv.Shutdown
- `alert.Dispatcher.Dispatch` goroutine — owned by ProxyHandler.ServeProxy, bounded by HTTP client timeout

## No Package-Level Vars Except
- Default slog logger set in main.go via slog.SetDefault

## Database Migrations
- `db/migrations/001_init.sql` — initial schema
- `db/migrations/002_multi_tenant.sql` — users table, providers re-keyed to user_id, api_key_value removed, mock_enabled added
