package borego

import (
	"io"
	"net"
)

// proxy copies data between two network connections until either side closes.
func proxy(a, b net.Conn) error {
	errCh := make(chan error, 2)

	go func() {
		_, err := io.Copy(a, b)
		errCh <- err
	}()

	go func() {
		_, err := io.Copy(b, a)
		errCh <- err
	}()

	err1 := <-errCh
	// Close both sides to make sure the opposite goroutine exits.
	_ = a.Close()
	_ = b.Close()
	err2 := <-errCh

	if err1 != nil && err1 != io.EOF {
		return err1
	}
	if err2 != nil && err2 != io.EOF {
		return err2
	}
	return nil
}
