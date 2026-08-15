package token

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Type string

const (
	TypeAccess Type = "access"

	TypeRefresh Type = "refresh"
)

type Algorithm string

const (
	AlgHS256 Algorithm = "HS256"
	AlgEdDSA Algorithm = "EdDSA"
)

const MinSecretLength = 32

const RoleService = "service"

var (
	ErrInvalidToken = errors.New("token: invalid token")
	ErrWrongType    = errors.New("token: unexpected token type")
)

type Claims struct {
	jwt.RegisteredClaims

	Type      Type     `json:"typ"`
	SessionID string   `json:"sid,omitempty"`
	Roles     []string `json:"roles,omitempty"`
}

func (c *Claims) UserID() string {
	if c == nil {
		return ""
	}
	return c.Subject
}

func (c *Claims) HasRole(role string) bool {
	if c == nil {
		return false
	}
	for _, existing := range c.Roles {
		if existing == role {
			return true
		}
	}
	return false
}

type Config struct {
	Algorithm Algorithm `env:"JWT_ALGORITHM" envDefault:"HS256"`

	Secret        string `env:"JWT_SECRET"`
	PrivateKeyPEM string `env:"JWT_PRIVATE_KEY"`
	PublicKeyPEM  string `env:"JWT_PUBLIC_KEY"`

	Issuer   string   `env:"JWT_ISSUER" envDefault:"hockeynights"`
	Audience []string `env:"JWT_AUDIENCE" envSeparator:","`

	AccessTTL time.Duration `env:"JWT_ACCESS_TTL" envDefault:"15m"`

	Leeway time.Duration `env:"JWT_LEEWAY" envDefault:"30s"`
}

type Manager struct {
	cfg    Config
	method jwt.SigningMethod
	sign   any
	verify any
	parser *jwt.Parser
	now    func() time.Time
}

func NewManager(cfg Config) (*Manager, error) {
	cfg = cfg.withDefaults()

	manager := &Manager{cfg: cfg, now: time.Now}

	switch cfg.Algorithm {
	case AlgHS256:
		if len(cfg.Secret) < MinSecretLength {
			return nil, fmt.Errorf(
				"token: HS256 secret must be at least %d bytes, got %d",
				MinSecretLength, len(cfg.Secret),
			)
		}
		manager.method = jwt.SigningMethodHS256
		manager.sign = []byte(cfg.Secret)
		manager.verify = []byte(cfg.Secret)

	case AlgEdDSA:
		manager.method = jwt.SigningMethodEdDSA

		if cfg.PrivateKeyPEM != "" {
			key, err := parseEd25519PrivateKey(cfg.PrivateKeyPEM)
			if err != nil {
				return nil, err
			}
			manager.sign = key
			manager.verify = key.Public()
		}

		if cfg.PublicKeyPEM != "" {
			key, err := parseEd25519PublicKey(cfg.PublicKeyPEM)
			if err != nil {
				return nil, err
			}
			manager.verify = key
		}

		if manager.verify == nil {
			return nil, errors.New("token: EdDSA requires JWT_PRIVATE_KEY or JWT_PUBLIC_KEY")
		}

	default:
		return nil, fmt.Errorf("token: unsupported algorithm %q", cfg.Algorithm)
	}

	options := []jwt.ParserOption{
		jwt.WithValidMethods([]string{manager.method.Alg()}),
		jwt.WithIssuer(cfg.Issuer),
		jwt.WithLeeway(cfg.Leeway),
		jwt.WithExpirationRequired(),
	}
	for _, audience := range cfg.Audience {
		options = append(options, jwt.WithAudience(audience))
	}

	manager.parser = jwt.NewParser(options...)

	return manager, nil
}

type IssueOptions struct {
	SessionID string
	Roles     []string
	TTL       time.Duration
	ID        string
}

func (m *Manager) Issue(userID string, tokenType Type, opts IssueOptions) (string, *Claims, error) {
	if m.sign == nil {
		return "", nil, errors.New("token: signing key is not configured")
	}
	if userID == "" {
		return "", nil, errors.New("token: user id is required")
	}

	ttl := opts.TTL
	if ttl == 0 {
		if tokenType != TypeAccess {

			return "", nil, fmt.Errorf("token: TTL is required for %q tokens", tokenType)
		}
		ttl = m.cfg.AccessTTL
	}

	now := m.now()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    m.cfg.Issuer,
			Audience:  m.cfg.Audience,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			ID:        opts.ID,
		},
		Type:      tokenType,
		SessionID: opts.SessionID,
		Roles:     opts.Roles,
	}

	signed, err := jwt.NewWithClaims(m.method, claims).SignedString(m.sign)
	if err != nil {
		return "", nil, fmt.Errorf("token: sign: %w", err)
	}

	return signed, claims, nil
}

func (m *Manager) IssueAccess(userID string, opts IssueOptions) (string, *Claims, error) {
	return m.Issue(userID, TypeAccess, opts)
}

func (m *Manager) Parse(raw string, expected Type) (*Claims, error) {
	claims := &Claims{}

	parsed, err := m.parser.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) {
		return m.verify, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if !parsed.Valid {
		return nil, ErrInvalidToken
	}

	if expected != "" && claims.Type != expected {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrWrongType, claims.Type, expected)
	}

	return claims, nil
}

func (m *Manager) ParseAccess(raw string) (*Claims, error) {
	return m.Parse(raw, TypeAccess)
}

func (m *Manager) AccessTTL() time.Duration { return m.cfg.AccessTTL }

func (c Config) withDefaults() Config {
	if c.Algorithm == "" {
		c.Algorithm = AlgHS256
	}
	if c.Issuer == "" {
		c.Issuer = "hockeynights"
	}
	if c.AccessTTL == 0 {
		c.AccessTTL = 15 * time.Minute
	}
	if c.Leeway == 0 {
		c.Leeway = 30 * time.Second
	}
	return c
}

func parseEd25519PrivateKey(encoded string) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode([]byte(encoded))
	if block == nil {
		return nil, errors.New("token: private key is not valid PEM")
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("token: parse private key: %w", err)
	}

	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("token: private key must be ed25519, got %T", parsed)
	}

	return key, nil
}

func parseEd25519PublicKey(encoded string) (ed25519.PublicKey, error) {
	block, _ := pem.Decode([]byte(encoded))
	if block == nil {
		return nil, errors.New("token: public key is not valid PEM")
	}

	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("token: parse public key: %w", err)
	}

	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("token: public key must be ed25519, got %T", parsed)
	}

	return key, nil
}

func GenerateEd25519Keys() (privatePEM, publicPEM string, err error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("token: generate key: %w", err)
	}

	privateBytes, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return "", "", fmt.Errorf("token: marshal private key: %w", err)
	}

	publicBytes, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		return "", "", fmt.Errorf("token: marshal public key: %w", err)
	}

	privatePEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateBytes}))
	publicPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicBytes}))

	return privatePEM, publicPEM, nil
}
