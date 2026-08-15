# grpcclient

Подключение к другим gRPC-сервисам Together.

Что делает: создаёт `*grpc.ClientConn`, включает балансировку и ретраи, keepalive, пробрасывает
request-id и ставит дедлайн вызовам, у которых его нет.

## Пример

```go
conn, err := grpcclient.New(ctx, grpcclient.Config{
	Target: "dns:///user-service:50051",
	Logger: log,
})
if err != nil {
	return err
}
defer conn.Close()

users := userv1.NewUserServiceClient(conn)
```

Соединение потокобезопасно и создаётся один раз на процесс — не открывай его на каждый вызов.

Для нескольких реплик используй схему `dns:///host:port`: без неё адрес резолвится один раз при старте,
и новые поды трафик не получат.

## Что включено по умолчанию

- **round_robin** вместо `pick_first`: иначе весь трафик уходит в одну реплику.
- **Ретраи** — до 4 попыток на `UNAVAILABLE` и `RESOURCE_EXHAUSTED` с экспоненциальной паузой.
  `INTERNAL` и `DEADLINE_EXCEEDED` не ретраятся намеренно: первый может означать частично применённое
  изменение, второй — повтор поверх уже истёкшего дедлайна.
- **Дедлайн** `GRPC_CLIENT_TIMEOUT` (5 с) на вызовы без своего дедлайна.
- **keepalive** 30 с — согласован с `EnforcementPolicy` сервера, иначе `GOAWAY too_many_pings`.
- **request-id** из контекста уезжает в metadata: без этого цепочка вызовов через несколько сервисов
  распадается в логах на несвязанные куски.

Для неидемпотентных методов ретраи нужно выключать — `DisableRetries: true`.

## Ожидание подключения

```go
conn, err := grpcclient.New(ctx, grpcclient.Config{
	Target:            "dns:///user-service:50051",
	WaitForConnection: true,
})
```

`WaitForConnection: false` (по умолчанию) — сервис поднимется, даже если зависимость временно лежит.
Включай `true` только если работать без этой зависимости он всё равно не может: иначе один лежащий
сервис не даст стартовать остальным.

## TLS

По умолчанию соединение идёт открытым текстом — внутри кластера шифрование обычно делает service mesh.
Для публичного адреса задай `TLS` и включи `RequireTLS: true`, тогда забытый конфиг уронит сервис на
старте, а не молча уведёт трафик в открытый канал.

## Service-to-service аутентификация

```go
grpcclient.Config{
	UnaryInterceptors: []grpc.UnaryClientInterceptor{
		grpcclient.StaticTokenInterceptor(func(ctx context.Context) (string, error) {
			return tokens.ServiceToken(ctx)
		}),
	},
}
```

Функция, а не строка: токен сервиса истекает, и брать его нужно свежим.

## Конфигурация

`GRPC_TARGET`, `GRPC_CLIENT_TIMEOUT` (`5s`), `GRPC_CLIENT_CONNECT_TIMEOUT` (`5s`),
`GRPC_CLIENT_WAIT_FOR_CONNECTION` (`false`), `GRPC_CLIENT_REQUIRE_TLS` (`false`),
`GRPC_CLIENT_DISABLE_RETRIES` (`false`), `GRPC_CLIENT_ENABLE_TRACING` (`false`).
