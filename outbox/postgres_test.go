package outbox_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HockeyNights/hockey-libs/migrate"
	"github.com/HockeyNights/hockey-libs/outbox"
	"github.com/HockeyNights/hockey-libs/postgresql"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// Оба теста в этом файле работают с одной и той же таблицей, а порядок внутри
// пакета не определён: go test запускается с -shuffle=on. Поэтому состояние
// базы готовит общий помощник — миграции и пустая таблица, — а не первый
// попавшийся тест. Иначе набор проходит или падает в зависимости от жребия.
func testPool(t *testing.T) (*pgxpool.Pool, postgresql.Config) {
	t.Helper()

	dsn := os.Getenv("OUTBOX_TEST_DSN")
	if dsn == "" {
		t.Skip("OUTBOX_TEST_DSN не задан")
	}

	ctx := context.Background()
	cfg := postgresql.Config{DSN: dsn}

	_, err := migrate.Up(ctx, cfg, outbox.Migrations, migrate.Config{Dir: "migrations", Table: "outbox_migrations"})
	require.NoError(t, err)

	pool, err := postgresql.Open(ctx, cfg)
	require.NoError(t, err)
	// t.Cleanup, а не defer у вызывающего: defer в тесте срабатывает РАНЬШЕ
	// зарегистрированных cleanup'ов, и всё, что они делают с базой, уходит в
	// закрытый пул.
	t.Cleanup(pool.Close)

	// Таблица общая, а тесты считают строки целиком (CountFailed). Чистим
	// здесь, а не в конце: так прогон по уже использованной базе — например
	// повторный запуск локально — ведёт себя как первый.
	_, err = pool.Exec(ctx, "TRUNCATE outbox")
	require.NoError(t, err)

	return pool, cfg
}

func TestAgainstRealPostgres(t *testing.T) {
	ctx := context.Background()
	pool, cfg := testPool(t)

	// Повторная миграция обязана быть no-op: сервисы вызывают Up на каждом
	// старте.
	_, err := migrate.Up(ctx, cfg, outbox.Migrations, migrate.Config{Dir: "migrations", Table: "outbox_migrations"})
	require.NoError(t, err)

	repo := outbox.NewRepository(pool)

	id, err := outbox.Enqueue(ctx, pool, "email", []byte(`{"to":"a@b.c"}`), 5)
	require.NoError(t, err)

	claimed, err := repo.Claim(ctx, 10, 2*time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, id, claimed[0].ID)
	require.Equal(t, "email", claimed[0].Kind)
	require.Equal(t, int16(1), claimed[0].Attempts, "Claim обязан увеличить attempts")

	again, err := repo.Claim(ctx, 10, 2*time.Minute)
	require.NoError(t, err)
	require.Empty(t, again)

	require.NoError(t, repo.Retry(ctx, id, time.Second, "temporary"))
	require.NoError(t, repo.Fail(ctx, id, "permanent"))

	count, err := repo.CountFailed(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	deleted, err := repo.DeleteOldFailed(ctx, -time.Second)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	id2, err := outbox.Enqueue(ctx, pool, "email", []byte(`{}`), 5)
	require.NoError(t, err)
	require.NoError(t, repo.Complete(ctx, id2))
	left, err := repo.Claim(ctx, 10, time.Minute)
	require.NoError(t, err)
	require.Empty(t, left, "Complete обязан удалить строку")
}

func TestLongReasonIsTruncatedSafely(t *testing.T) {
	ctx := context.Background()
	pool, _ := testPool(t)

	repo := outbox.NewRepository(pool)
	id, err := outbox.Enqueue(ctx, pool, "email", []byte(`{}`), 5)
	require.NoError(t, err)

	require.NoError(t, repo.Retry(ctx, id, time.Second, strings.Repeat("отказ сервиса ", 200)))
	require.NoError(t, repo.Fail(ctx, id, strings.Repeat("неизвестный тип ", 200)))
}
