package borego

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
)

// Authenticator replies to server challenges using a shared secret.
type Authenticator struct {
	key []byte
}

// NewAuthenticator constructs an authenticator from a secret string.
func NewAuthenticator(secret string) *Authenticator {
	hashed := sha256.Sum256([]byte(secret))
	return &Authenticator{key: hashed[:]}
}

// Answer creates the HMAC tag for a challenge UUID.
func (a *Authenticator) Answer(challenge uuid.UUID) string {
	mac := hmac.New(sha256.New, a.key)
	mac.Write(challenge[:])
	return hex.EncodeToString(mac.Sum(nil))
}

// ClientHandshake answers the server's challenge when authentication is enabled.
func (a *Authenticator) ClientHandshake(conn *Delimited) error {
	msg, ok, err := conn.RecvServer(true)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnexpectedEOF
	}
	if msg.Kind != ServerChallenge {
		return ErrUnexpectedHandshake
	}
	tag := a.Answer(msg.ID)
	return conn.SendJSON(encodeAuthenticate(tag))
}
