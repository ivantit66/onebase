package equipment

import (
	"io"
	"testing"
)

// chunkedReader отдаёт заранее заданные куски по одному на Read, имитируя
// фрагментированный TCP/serial-поток (один Read != весь фрейм).
type chunkedReader struct {
	chunks [][]byte
	i      int
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if c.i >= len(c.chunks) {
		return 0, io.EOF
	}
	n := copy(p, c.chunks[c.i])
	c.i++
	return n, nil
}

// readFrame должен дочитывать ответ до терминатора '\r', даже если фрейм пришёл
// несколькими кусками (регрессия #6: один Read давал обрезанный ответ).
func TestReadFrame_ChunkedUntilTerminator(t *testing.T) {
	r := &chunkedReader{chunks: [][]byte{[]byte("APPRO"), []byte("VED RRN=123\r")}}
	out, err := readFrame(r)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if string(out) != "APPROVED RRN=123\r" {
		t.Errorf("собран фрейм %q, ожидался \"APPROVED RRN=123\\r\"", string(out))
	}
}

// Без терминатора фрейм собирается до EOF/паузы и возвращается накопленное.
func TestReadFrame_NoTerminatorReturnsOnEOF(t *testing.T) {
	r := &chunkedReader{chunks: [][]byte{[]byte("0.250 "), []byte("kg")}}
	out, err := readFrame(r)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if string(out) != "0.250 kg" {
		t.Errorf("собран фрейм %q, ожидался \"0.250 kg\"", string(out))
	}
}

// Если не пришло вообще ничего — отдаём ошибку чтения.
func TestReadFrame_EmptyIsError(t *testing.T) {
	r := &chunkedReader{}
	if _, err := readFrame(r); err == nil {
		t.Error("ожидалась ошибка при пустом ответе")
	}
}

func TestIsSerialAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:9100":     false,
		"localhost:9100":     false,
		"COM3":               true,
		"/dev/ttyUSB0":       true,
		"/dev/tty.usbserial": true,
	}
	for addr, want := range cases {
		if got := isSerialAddr(addr); got != want {
			t.Errorf("isSerialAddr(%q) = %v, ожидалось %v", addr, got, want)
		}
	}
}

func TestOpenWriteTransport_TCP(t *testing.T) {
	addr, _ := captureServer(t)
	wc, err := openWriteTransport(map[string]string{"порт": addr})
	if err != nil {
		t.Fatalf("openWriteTransport TCP: %v", err)
	}
	wc.Close()
}

// serial-путь выбирается по адресу без двоеточия; несуществующий порт даёт ошибку
// (полная проверка реальной печати по COM требует железа).
func TestOpenWriteTransport_SerialError(t *testing.T) {
	if _, err := openWriteTransport(map[string]string{"порт": "/dev/onebase_nonexistent_tty"}); err == nil {
		t.Error("ожидалась ошибка открытия несуществующего serial-порта")
	}
}

func TestOpenWriteTransport_NoPort(t *testing.T) {
	if _, err := openWriteTransport(map[string]string{}); err == nil {
		t.Error("ожидалась ошибка при отсутствии параметра Порт")
	}
}

func TestNeutralDriverNames(t *testing.T) {
	for _, name := range []string{"escpos", "display"} {
		found := false
		for _, d := range Drivers() {
			if d == name {
				found = true
			}
		}
		if !found {
			t.Errorf("драйвер %q не зарегистрирован: %v", name, Drivers())
		}
	}
}

// Запрос-ответ драйвер (весы) теперь тоже выбирает serial по адресу без двоеточия.
func TestRWTransport_SerialError(t *testing.T) {
	if _, err := Open("scale_tcp", map[string]string{"порт": "/dev/onebase_nonexistent_tty"}); err == nil {
		t.Error("ожидалась ошибка serial для весов на несуществующем порту")
	}
}

func TestRWTransport_NoPort(t *testing.T) {
	if _, err := Open("scanner_tcp", map[string]string{}); err == nil {
		t.Error("ожидалась ошибка при отсутствии параметра Порт")
	}
}
