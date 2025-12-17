package borego

import "errors"

var (
	// ErrUnexpectedEOF indicates that the server closed the connection unexpectedly.
	ErrUnexpectedEOF = errors.New("unexpected EOF from server")
	// ErrUnexpectedHandshake indicates an unexpected message during authentication.
	ErrUnexpectedHandshake = errors.New("unexpected handshake message")
)
