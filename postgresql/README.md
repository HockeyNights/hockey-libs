# postgresql

Подключение к PostgreSQL через `pgx`: пул, транзакции и перевод ошибок Postgres в доменные.

Миграции, SQL-запросы и сгенерированный `sqlc`-код лежат в конкретном сервисе — этот пакет про них
ничего не знает.

## Пул

```go
pool, err := postgresql.Open(ctx, cfg.Postgres, postgresql.WithTracing())
if err != nil {
	return err
}
defer pool.Close()

queries := sqlc.New(pool)
```

`Open` проверяет конфиг, применяет настройки пула и делает `Ping` с ограничением по
`POSTGRES_CONNECT_TIMEOUT`. Опции:

- `WithTracing()` — SQL-запросы становятся спанами OpenTelemetry;
- `WithAfterConnect(hook)` — регистрация кастомных типов Postgres (enum, composite) на каждом соединении;
- `WithPoolConfig(fn)` — прямой доступ к `pgxpool.Config`.

За PgBouncer в режиме transaction pooling выставь `POSTGRES_DISABLE_PREPARED_STATEMENTS=true`.

## Транзакции

```go
err := postgresql.InTx(ctx, pool, func(tx pgx.Tx) error {
	if err := sqlc.New(tx).CreateUser(ctx, params); err != nil {
		return err
	}
	return sqlc.New(tx).CreateProfile(ctx, profileParams)
})
```

Commit при успехе, rollback при ошибке и при панике. Откат делается по `context.WithoutCancel`: если
контекст уже отменён по дедлайну, обычный rollback до базы не доедет и соединение вернётся в пул с
открытой транзакцией.

Для явного уровня изоляции — `InTxOptions`.

## Ошибки

```go
func (r *Repository) ByEmail(ctx context.Context, email string) (*User, error) {
	row, err := r.queries.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, postgresql.MapError(err)
	}
	return toDomain(row), nil
}
```

`MapError` переводит `pgx.ErrNoRows` в `apperr.KindNotFound`, unique violation — в `KindAlreadyExists`,
deadlock и serialization failure — в `KindConflict`. Сообщение для клиента обезличено: текст ошибки
Postgres содержит имена таблиц, индексов и сами значения, из-за которых сработал constraint. Оригинал
остаётся обёрнутым и виден в логах, имя constraint — в `Fields["pg_constraint"]`.

Когда нужно различать конкретные индексы:

```go
if postgresql.IsUniqueViolation(err, "users_email_key") {
	return apperr.AlreadyExists("email_taken", "email is already registered")
}
```

## Конфигурация

Структура `Config` размечена тегами `env` и встраивается в конфиг сервиса:

```go
type Config struct {
	Postgres postgresql.Config
}

cfg, err := config.Load[Config]()
```

Одной строкой:

```env
DATABASE_URL=postgres://postgres:postgres@localhost:5432/together?sslmode=disable
```

Или по полям: `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`,
`POSTGRES_SSLMODE`, `POSTGRES_MIN_CONNS`, `POSTGRES_MAX_CONNS`, `POSTGRES_MAX_CONN_LIFETIME`.
`DATABASE_URL` имеет приоритет.

Для логов используй `cfg.SafeDSNString()` — в нём вырезан пароль.

## sqlc

В сервисе:

```yaml
sql_package: "pgx/v5"
```

Тогда сгенерированный `New()` принимает `DBTX`, и туда одинаково подходят и `*pgxpool.Pool`, и `pgx.Tx`.
