package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/sync/semaphore"
)

var (
	ErrMismatch           = errors.New("password: hash and password do not match")
	ErrInvalidHash        = errors.New("password: invalid hash format")
	ErrUnsupportedVersion = errors.New("password: unsupported argon2 version")
)

type Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

func DefaultParams() Params {
	parallelism := runtime.NumCPU()
	if parallelism > 4 {
		parallelism = 4
	}
	if parallelism < 1 {
		parallelism = 1
	}

	return Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: uint8(parallelism),
		SaltLength:  16,
		KeyLength:   32,
	}
}

type Hasher struct {
	params Params
	slots  *semaphore.Weighted
}

type Option func(*Hasher)

func WithMaxConcurrent(n int) Option {
	return func(h *Hasher) {
		if n > 0 {
			h.slots = semaphore.NewWeighted(int64(n))
		}
	}
}

func NewHasher(params Params, opts ...Option) *Hasher {
	defaults := DefaultParams()

	if params.Memory == 0 {
		params.Memory = defaults.Memory
	}
	if params.Iterations == 0 {
		params.Iterations = defaults.Iterations
	}
	if params.Parallelism == 0 {
		params.Parallelism = defaults.Parallelism
	}
	if params.SaltLength == 0 {
		params.SaltLength = defaults.SaltLength
	}
	if params.KeyLength == 0 {
		params.KeyLength = defaults.KeyLength
	}

	hasher := &Hasher{
		params: params,
		slots:  semaphore.NewWeighted(int64(max(runtime.GOMAXPROCS(0), 1))),
	}

	for _, opt := range opts {
		opt(hasher)
	}

	return hasher
}

var ErrBusy = errors.New("password: hasher is busy")

func (h *Hasher) acquire(ctx context.Context) error {
	if err := h.slots.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("%w: %w", ErrBusy, err)
	}
	return nil
}

func (h *Hasher) Hash(ctx context.Context, plain string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.slots.Release(1)

	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: generate salt: %w", err)
	}

	key := argon2.IDKey(
		[]byte(plain),
		salt,
		h.params.Iterations,
		h.params.Memory,
		h.params.Parallelism,
		h.params.KeyLength,
	)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.params.Memory,
		h.params.Iterations,
		h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func (h *Hasher) Verify(ctx context.Context, plain, encoded string) error {
	if err := h.acquire(ctx); err != nil {
		return err
	}
	defer h.slots.Release(1)

	return Verify(plain, encoded)
}

func (h *Hasher) NeedsRehash(encoded string) (bool, error) {
	params, _, _, err := decode(encoded)
	if err != nil {
		return false, err
	}

	return params.Memory < h.params.Memory ||
		params.Iterations < h.params.Iterations ||
		params.KeyLength < h.params.KeyLength, nil
}

func Hash(ctx context.Context, plain string) (string, error) {
	return NewHasher(DefaultParams()).Hash(ctx, plain)
}

func Verify(plain, encoded string) error {
	params, salt, want, err := decode(encoded)
	if err != nil {
		return err
	}

	got := argon2.IDKey(
		[]byte(plain),
		salt,
		params.Iterations,
		params.Memory,
		params.Parallelism,
		params.KeyLength,
	)

	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}

	return nil
}

func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return Params{}, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return Params{}, nil, nil, ErrUnsupportedVersion
	}

	var params Params
	if _, err := fmt.Sscanf(
		parts[3],
		"m=%d,t=%d,p=%d",
		&params.Memory,
		&params.Iterations,
		&params.Parallelism,
	); err != nil {
		return Params{}, nil, nil, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, ErrInvalidHash
	}

	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, ErrInvalidHash
	}

	params.SaltLength = uint32(len(salt))
	params.KeyLength = uint32(len(key))

	if params.Memory == 0 || params.Iterations == 0 || params.Parallelism == 0 || params.KeyLength == 0 {
		return Params{}, nil, nil, ErrInvalidHash
	}

	return params, salt, key, nil
}
