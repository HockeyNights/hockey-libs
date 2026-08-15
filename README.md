# hockey-libs

Общие библиотеки сервисов HockeyNights. Один Go-модуль — `github.com/HockeyNights/hockey-libs` — с пакетами внутри.

Почему один модуль, а не по модулю на пакет: пакетов полтора десятка, они меняются вместе, и три десятка
`go.mod` с взаимными версиями обошлись бы дороже, чем чуть более длинный `go.sum` у сервисов. Go всё равно
линкует в бинарь только реально используемые пакеты.

## Что внутри

| Пакет | Зачем |
|---|---|
| `config` | Загрузка конфига из окружения и `.env` |
| `logger` | Единый `slog`: уровни, JSON, `trace_id`, атрибуты из контекста |
| `apperr` | Доменные ошибки, независимые от транспорта |
| `requestid` | Идентификатор запроса в контексте и его ключ в metadata |
| `grpcserver` | gRPC-сервер: интерцепторы, health, graceful shutdown |
| `grpcclient` | Подключение к другим сервисам: ретраи, keepalive, проброс request-id |
| `httpserver` | HTTP-сервер и служебный порт (`/healthz`, `/readyz`, `/metrics`, pprof) |
| `postgresql` | Пул `pgx`, транзакции, маппинг ошибок Postgres в доменные |
| `redisclient` | Клиент Redis с общими настройками |
| `migrate` | Миграции goose из встроенной в бинарь FS |
| `token` | Выпуск и проверка JWT (HS256 / EdDSA) |
| `password` | Хэширование паролей argon2id |
| `secret` | Refresh-токены, коды подтверждения и их хэширование |
| `observability` | OpenTelemetry-трейсинг и реестр метрик Prometheus |
| `runner` | Запуск и совместная остановка компонентов сервиса |
| `outbox` | Транзакционная очередь заданий поверх PostgreSQL |
| `ratelimit` | Счётчик обращений в Redis |
| `grpcgateway` | HTTP-шлюз поверх gRPC (пока ни одним сервисом не используется) |

## Подключение

Модуль подключается директивой `replace` на соседний каталог — так же, как и
контракты:

```
require github.com/HockeyNights/hockey-libs v0.0.0
replace github.com/HockeyNights/hockey-libs => ../hockey-libs
```

Правка в библиотеке сразу видна сервисам: ни тегов, ни `go.work`, ни
`GOPRIVATE` для сборки не нужно, и в CI собирается ровно то же, что локально.

## Как это собирается вместе

`authv1`, `handler` и `migrationsFS` здесь — из самого сервиса.

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/HockeyNights/hockey-libs/config"
	"github.com/HockeyNights/hockey-libs/grpcserver"
	"github.com/HockeyNights/hockey-libs/httpserver"
	"github.com/HockeyNights/hockey-libs/logger"
	"github.com/HockeyNights/hockey-libs/migrate"
	"github.com/HockeyNights/hockey-libs/observability"
	"github.com/HockeyNights/hockey-libs/postgresql"
	"github.com/HockeyNights/hockey-libs/runner"
	"github.com/HockeyNights/hockey-libs/token"
	"google.golang.org/grpc"
)

type Config struct {
	Logger        logger.Config
	Postgres      postgresql.Config
	Token         token.Config
	Observability observability.Config

	GRPC grpcserver.Config
	Ops  httpserver.Config
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load[Config]()
	if err != nil {
		return err
	}

	log, err := logger.New(cfg.Logger)
	if err != nil {
		return err
	}
	slog.SetDefault(log)

	ctx, stop := runner.SignalContext(context.Background())
	defer stop()

	shutdownTracing, err := observability.Init(ctx, cfg.Observability)
	if err != nil {
		return err
	}
	defer func() { _ = shutdownTracing(ctx) }()

	if _, err := migrate.Up(ctx, cfg.Postgres, migrationsFS, migrate.Config{Dir: "migrations", Logger: log}); err != nil {
		return err
	}

	pool, err := postgresql.Open(ctx, cfg.Postgres, postgresql.WithTracing())
	if err != nil {
		return err
	}
	defer pool.Close()

	tokens, err := token.NewManager(cfg.Token)
	if err != nil {
		return err
	}

	registry := observability.NewRegistry()
	ready := httpserver.NewReady()

	cfg.GRPC.Logger = log
	cfg.GRPC.Registry = registry
	cfg.GRPC.UnaryInterceptors = []grpc.UnaryServerInterceptor{
		grpcserver.MustValidationUnaryInterceptor(),
		grpcserver.AuthUnaryInterceptor(grpcserver.AuthConfig{
			Authenticate:  grpcserver.TokenAuthenticator(tokens),
			PublicMethods: []string{authv1.AuthService_Login_FullMethodName},
		}),
	}

	grpcSrv := grpcserver.New(cfg.GRPC, func(s *grpc.Server) {
		authv1.RegisterAuthServiceServer(s, handler)
	})

	cfg.Ops.Logger = log
	cfg.Ops.Handler = httpserver.Ops(httpserver.OpsConfig{
		Registry: registry,
		Readiness: map[string]httpserver.Check{
			"service":  ready.Check(),
			"postgres": pool.Ping,
		},
	})

	ready.Set(true)

	return runner.New(log).
		Add("grpc", grpcSrv.Run).
		Add("ops", httpserver.New(cfg.Ops).Run).
		Run(ctx)
}
```

## Как ошибки доезжают до клиента

Цепочка задумана так, чтобы доменный слой ничего не знал про gRPC:

```
репозиторий            postgresql.MapError(err)   → *apperr.Error (KindAlreadyExists)
сервис                 apperr.AlreadyExists(...)  → *apperr.Error
grpcserver             ErrorMappingInterceptor    → codes.AlreadyExists + ErrorInfo{Reason: "email_taken"}
```

Два правила, которые в этой цепочке соблюдаются:

- `Kind` определяет gRPC-код, `Code` уезжает в `details` как машинный признак. Клиент разбирает `Code`,
  а не текст сообщения.
- Всё, что схлопнулось в `KindInternal`, отдаётся наружу как `internal server error` без подробностей.
  Настоящий текст остаётся в логах.

## Переменные окружения

Все структуры конфигов размечены тегами `env` и заполняются через `config.Load`.

**Общее**

| Переменная | По умолчанию | Что делает |
|---|---|---|
| `SERVICE_NAME`, `SERVICE_VERSION` | — | Попадают в логи и в трейсы |
| `ENVIRONMENT` | `development` | Атрибут окружения в трейсах |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `LOG_FORMAT` | `json` | `json` или `text` для локальной разработки |

**PostgreSQL**

| Переменная | По умолчанию |
|---|---|
| `DATABASE_URL` | — (перекрывает поля ниже) |
| `POSTGRES_HOST` / `POSTGRES_PORT` | `localhost` / `5432` |
| `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` | `postgres` / — / `postgres` |
| `POSTGRES_SSLMODE` | `disable` |
| `POSTGRES_MIN_CONNS` / `POSTGRES_MAX_CONNS` | `2` / `20` |
| `POSTGRES_DISABLE_PREPARED_STATEMENTS` | `false` (включить за PgBouncer в transaction pooling) |

**gRPC**

| Переменная | По умолчанию |
|---|---|
| `GRPC_ADDRESS` | `:50051` |
| `GRPC_ENABLE_HEALTH` | `true` |
| `GRPC_ENABLE_REFLECTION` | `false` |
| `GRPC_ENABLE_TRACING` | `false` |
| `GRPC_DRAIN_DELAY` | `3s` |
| `GRPC_GRACEFUL_STOP_TIMEOUT` | `10s` |

**JWT**

| Переменная | По умолчанию |
|---|---|
| `JWT_ALGORITHM` | `HS256` (или `EdDSA`) |
| `JWT_SECRET` | — (минимум 32 байта) |
| `JWT_PRIVATE_KEY` / `JWT_PUBLIC_KEY` | — (PEM, для EdDSA) |
| `JWT_ISSUER` | `hockeynights` |
| `JWT_ACCESS_TTL` | `15m` (срок refresh задаёт сервис: `AUTH_REFRESH_TTL`) |

**Прочее**

`REDIS_URL` или `REDIS_HOST`/`REDIS_PORT`/`REDIS_PASSWORD`/`REDIS_DB`; `HTTP_ADDRESS` (`:8080`);
`OTEL_ENABLED` (`false`), `OTEL_EXPORTER_OTLP_ENDPOINT` (`localhost:4317`).

## Разработка

```bash
go test ./...
```

```bash
go vet ./... && gofmt -l .
```

Линтер настроен в `.golangci.yml`, но сам не установлен:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
```
