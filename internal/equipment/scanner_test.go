package equipment

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// closeRecorderConn — транспорт-заглушка: один EOF-источник, фиксирует факт
// вызова Close (его делает только горутина-«сторож» Stream).
type closeRecorderConn struct {
	mu       sync.Mutex
	closed   bool
	closedCh chan struct{}
}

func newCloseRecorderConn() *closeRecorderConn {
	return &closeRecorderConn{closedCh: make(chan struct{})}
}

func (c *closeRecorderConn) Read(p []byte) (int, error) { return 0, io.EOF }
func (c *closeRecorderConn) Write(p []byte) (int, error) {
	return len(p), nil
}
func (c *closeRecorderConn) SetReadTimeout(time.Duration) error { return nil }
func (c *closeRecorderConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.closedCh)
	}
	return nil
}

// Регрессия #15: при неотменяемом контексте источник отдаёт EOF → Stream
// возвращается, а горутина-«сторож» НЕ должна зависнуть. defer cancel()
// производного контекста разблокирует её, и она вызывает conn.Close().
func TestScanner_Stream_GoroutineReleasedOnEOF(t *testing.T) {
	conn := newCloseRecorderConn()
	dev := &scannerDevice{conn: conn}

	done := make(chan error, 1)
	go func() { done <- dev.Stream(context.Background(), func(string) {}) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stream вернул ошибку: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stream не вернулся после EOF")
	}

	// Горутина-сторож должна была разблокироваться (defer cancel) и закрыть conn.
	select {
	case <-conn.closedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("горутина-сторож зависла: conn.Close не вызван после выхода Stream")
	}
}

func TestScanner_Stream(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Write([]byte("12345\n67890\n"))
		conn.Close() // EOF завершает Stream
	}()

	dev, err := Open("scanner_tcp", map[string]string{"порт": ln.Addr().String()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dev.Disconnect()
	src, ok := dev.(EventSource)
	if !ok {
		t.Fatal("устройство не реализует EventSource")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var codes []string
	if err := src.Stream(ctx, func(c string) { codes = append(codes, c) }); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(codes) != 2 || codes[0] != "12345" || codes[1] != "67890" {
		t.Errorf("коды = %v, ожидались [12345 67890]", codes)
	}
}
