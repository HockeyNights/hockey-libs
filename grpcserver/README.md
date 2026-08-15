# grpcserver

gRPC-сервер с общими для всех сервисов настройками.

Что делает: создаёт `grpc.Server`, вешает интерцепторы, включает health и reflection, слушает адрес и
корректно останавливается по отмене контекста.

## Пример

```go
cfg.GRPC.Logger = log
cfg.GRPC.Registry = registry
cfg.GRPC.UnaryInterceptors = []grpc.UnaryServerInterceptor{
	grpcserver.MustValidationUnaryInterceptor(),
	grpcserver.AuthUnaryInterceptor(grpcserver.AuthConfig{
		Authenticate:  grpcserver.TokenAuthenticator(tokens),
		PublicMethods: []string{
			authv1.AuthService_Login_FullMethodName,
			authv1.AuthService_Register_FullMethodName,
			authv1.AuthService_Refresh_FullMethodName,
		},
	}),
}

server := grpcserver.New(cfg.GRPC, func(s *grpc.Server) {
	authv1.RegisterAuthServiceServer(s, handler)
})

return server.Run(ctx)
```

## Цепочка интерцепторов

Стандартные ставятся автоматически, снаружи внутрь:

1. **Recovery** — паника превращается в `Internal` с обезличенным текстом, стек уходит в лог.
2. **RequestID** — `x-request-id` из metadata или новый UUID; кладётся в контекст, в атрибуты логгера и
   возвращается клиенту заголовком.
3. **Logging** — метод, код, длительность, request_id, peer. Ошибки клиента логируются как `warn`,
   наши — как `error`. Health-check и reflection пропускаются, иначе пробы забьют логи.
4. **Metrics** — если задан `Registry`: `grpc_server_requests_total`, `..._duration_seconds`,
   `..._in_flight`.
5. **ErrorMapping** — `*apperr.Error` → gRPC-статус, машинный код в `details`.

Ваши интерцепторы из `UnaryInterceptors` идут после них, ближе к хендлеру.

Отдельно доступны `TimeoutUnaryInterceptor`, `RequireRole`, `ValidationUnaryInterceptor`.

## Остановка

По отмене контекста сервер:

1. переводит health в `NOT_SERVING`;
2. ждёт `GRPC_DRAIN_DELAY` (по умолчанию 3 с), чтобы балансировщик увёл трафик;
3. делает `GracefulStop`, а если он не уложился в `GRPC_GRACEFUL_STOP_TIMEOUT` — рвёт соединения.

Без шага 1–2 каждый деплой даёт клиентам пачку `UNAVAILABLE`: сервис уже закрывается, а балансировщик
ещё об этом не знает. Значение `DrainDelay` держи чуть больше периода readiness-пробы.

## Умолчания, которых нет в grpc-go

- keepalive: пинг простаивающих соединений и `MaxConnectionAge` 30 мин, чтобы клиент не прилипал
  навсегда к одному поду;
- `EnforcementPolicy.MinTime` 10 с — без него клиент с частым keepalive получает `GOAWAY too_many_pings`;
- лимиты сообщений (4 МБ) и `MaxConcurrentStreams` (1000).

## Аутентификация

`PublicMethods` — белый список: метод, который забыли туда добавить, останется закрытым, а не откроется
наружу. Причина неудачной проверки клиенту не сообщается — только `invalid credentials`; детали
остаются в логах.

`TokenAuthenticator` проверяет **access**-токен и кладёт клеймы в контекст:

```go
userID := token.UserID(ctx)
claims, ok := token.FromContext(ctx)
```

## Конфигурация

`GRPC_ADDRESS` (`:50051`), `GRPC_ENABLE_HEALTH` (`true`), `GRPC_ENABLE_REFLECTION` (`false`),
`GRPC_ENABLE_TRACING` (`false`), `GRPC_DRAIN_DELAY` (`3s`), `GRPC_GRACEFUL_STOP_TIMEOUT` (`10s`),
`GRPC_MAX_RECV_MSG_SIZE`, `GRPC_MAX_SEND_MSG_SIZE`, `GRPC_MAX_CONCURRENT_STREAMS`.

Reflection раскрывает полную схему API — в проде обычно выключен.
