// Package password hashes and verifies passwords with argon2id.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Parameters are the argon2id cost parameters.
type Parameters struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultParameters mirror the reference kit's hasher defaults.
var DefaultParameters = Parameters{
	MemoryKiB:   19456,
	Iterations:  2,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

// Hasher hashes and verifies passwords in the PHC argon2id format.
type Hasher struct {
	params Parameters
}

// New builds a hasher with the default parameters.
func New() *Hasher {
	return &Hasher{params: DefaultParameters}
}

// NewWithParameters builds a hasher with explicit cost parameters.
func NewWithParameters(params Parameters) *Hasher {
	return &Hasher{params: params}
}

// Hash returns the PHC-encoded argon2id hash of password.
func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, h.params.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.params.MemoryKiB, h.params.Iterations, h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// maxSegmentLength bounds parsed salt and key segments; the reference kit
// produces 16-byte salts and 32-byte keys.
const maxSegmentLength = 1 << 20

// Verify reports whether password matches the encoded argon2id hash.
func (h *Hasher) Verify(encoded, password string) (bool, error) {
	params, salt, key, err := decode(encoded)
	if err != nil {
		return false, err
	}
	keyLength, ok := boundedUint32(len(key))
	if !ok {
		return false, fmt.Errorf("password: key length out of range")
	}
	computed := argon2.IDKey([]byte(password), salt, params.Iterations, params.MemoryKiB, params.Parallelism, keyLength)
	return subtle.ConstantTimeCompare(computed, key) == 1, nil
}

func boundedUint32(value int) (uint32, bool) {
	if value <= 0 || value > maxSegmentLength {
		return 0, false
	}
	return uint32(value), true
}

func decode(encoded string) (Parameters, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Parameters{}, nil, nil, fmt.Errorf("password: malformed argon2id hash")
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || !strings.HasPrefix(parts[2], "v=") || version != argon2.Version {
		return Parameters{}, nil, nil, fmt.Errorf("password: unsupported argon2 version in hash")
	}
	var params Parameters
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.MemoryKiB, &params.Iterations, &params.Parallelism); err != nil {
		return Parameters{}, nil, nil, fmt.Errorf("password: malformed argon2id parameters: %w", err)
	}
	if params.MemoryKiB == 0 || params.Iterations == 0 || params.Parallelism == 0 {
		return Parameters{}, nil, nil, fmt.Errorf("password: invalid argon2id parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Parameters{}, nil, nil, fmt.Errorf("password: malformed salt")
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Parameters{}, nil, nil, fmt.Errorf("password: malformed key")
	}
	saltLength, ok := boundedUint32(len(salt))
	if !ok {
		return Parameters{}, nil, nil, fmt.Errorf("password: salt length out of range")
	}
	keyLength, ok := boundedUint32(len(key))
	if !ok {
		return Parameters{}, nil, nil, fmt.Errorf("password: key length out of range")
	}
	params.SaltLength = saltLength
	params.KeyLength = keyLength
	return params, salt, key, nil
}
