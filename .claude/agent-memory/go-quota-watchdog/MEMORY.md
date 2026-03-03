# API Quota Watchdog — Agent Memory

## Package Layout (confirmed, compiles clean)
```
internal/
  apperror/   apperror.go   — AppError{Code,Message,Err}, New(), Write()
  config/     config.go     — Config/Provider structs, Load(), Get(), StartReloader()
  store/      store.go      — Store, New(), stub DB methods (TODO bodies)
  quota/      quota.go      — Enforcer, Check(), Record(), ThresholdExceeded() (pure fn)
  proxy/      proxy.go      — Proxy, Forward(), stripHopByHop()
  alert/      alert.go      — Dispatcher, Dispatch() (fire-and-forget goroutine)
  sse/        hub.go        — Hub, Register(), Deregister(), Broadcast()
  middleware/ auth.go       — Auth() middleware, ClaimsFromContext(), contextKey type
  handler/    proxy.go      — ProxyHandler, ServeProxy(), capturingResponseWriter
  server/     server.go     — Server, NewServer(*sql.DB), ServeHTTP()
              routes.go     — routes() wires POST /proxy/{provider}/{path...}
main.go                     — slog init, JWT guard, DB open, config load, graceful shutdown
```

## Approved Dependencies (go.mod)
- `github.com/golang-jwt/jwt/v5 v5.2.1`
- `github.com/lib/pq v1.10.9`
- `gopkg.in/yaml.v3 v3.0.1`

## Key Architectural Decisions
- **AppError boundary**: store/proxy/quota return plain `error`; only handler/ constructs AppError
- **Hot-reload**: `os.Stat` + `time.Ticker` (no fsnotify — not in approved dep list)
- **Proxy**: hand-rolled with `http.Client`; never `httputil.ReverseProxy`
- **Quota**: synchronous DB write on every proxied request; no in-memory cache
- **Alerts**: edge-triggered; crossing state tracked in DB (`quotas.threshold_crossed`)
- **SSE Broadcast**: copies client map under lock, then sends with `select/default` (never blocks)
- **JWT**: HS256 only; secret from `WATCHDOG_JWT_SECRET` env var; server refuses start if absent
- **Logging**: `log/slog` with `JSONHandler`, initialized once in `main.go`

## Environment Variables
- `WATCHDOG_JWT_SECRET` — required; server fatal-exits if absent
- `DATABASE_URL` — PostgreSQL DSN; required
- `WATCHDOG_WEBHOOK_URL` — optional; alert dispatcher logs errors if absent

## Config File
- Path: `config.yaml` (hardcoded in main.go)
- Hot-reload interval: 15 seconds
- YAML structure: `providers: [{name, base_url, api_key_header}]`

## Store Stubs (TODO bodies remain)
- `GetProviderByName` — SELECT from providers WHERE name=$1
- `RecordUsage` — INSERT INTO usage_records
- `GetQuotaUsage` — COUNT + JOIN quotas
- `GetQuotaThresholdCrossed` — SELECT threshold_crossed FROM quotas
- `SetQuotaThresholdCrossed` — UPDATE quotas SET threshold_crossed=$2

## Goroutine Ownership
- `config.StartReloader` goroutine — owned by main, stopped by rootCtx cancellation
- `srv.ListenAndServe` goroutine — owned by main, stopped by srv.Shutdown
- `alert.Dispatcher.Dispatch` goroutine — owned by handler.ProxyHandler.ServeProxy, bounded by HTTP client timeout

## No Package-Level Vars Except
- `config.cfg` + `config.mu` (guarded by RWMutex, accessed only via Load/Get)
- Default slog logger set in main.go via slog.SetDefault

## Detailed Notes
See: patterns.md (not yet created — add when patterns solidify)
