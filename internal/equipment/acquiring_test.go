package equipment

import (
	"net"
	"testing"
	"time"
)

// terminalServer — эмулятор платёжного терминала: читает запрос PAY и отвечает reply.
func terminalServer(t *testing.T, reply string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 64)
		conn.Read(buf)
		conn.Write([]byte(reply))
	}()
	return ln.Addr().String()
}

func TestAcquiring_Pay_Approved(t *testing.T) {
	addr := terminalServer(t, "APPROVED RRN=123456789012 CARD=****1234\r\n")

	dev, err := Open("acquiring_tcp", map[string]string{"порт": addr})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dev.Disconnect()
	term, ok := dev.(PaymentTerminal)
	if !ok {
		t.Fatal("устройство не реализует PaymentTerminal")
	}
	res, err := term.Pay(100)
	if err != nil {
		t.Fatalf("Pay: %v", err)
	}
	if !res.Approved {
		t.Error("ожидалось одобрение операции")
	}
	if res.RRN != "123456789012" {
		t.Errorf("RRN = %q, ожидался 123456789012", res.RRN)
	}
	if res.Card != "****1234" {
		t.Errorf("Card = %q, ожидался ****1234", res.Card)
	}
}

func TestParsePayment(t *testing.T) {
	declined, err := parsePayment("DECLINED reason=insufficient")
	if err != nil {
		t.Fatal(err)
	}
	if declined.Approved {
		t.Error("DECLINED не должно быть одобрено")
	}
	if _, err := parsePayment("   "); err == nil {
		t.Error("ожидалась ошибка для пустого ответа")
	}
	ru, _ := parsePayment("ОДОБРЕНО RRN=999")
	if !ru.Approved || ru.RRN != "999" {
		t.Errorf("русский ответ разобран неверно: %+v", ru)
	}
}

// chunkedTerminalServer отдаёт ответ двумя записями с паузой между ними,
// имитируя фрагментированный TCP-поток (один conn.Read вернул бы лишь "APPRO").
func chunkedTerminalServer(t *testing.T, chunks ...string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 64)
		conn.Read(buf)
		for _, c := range chunks {
			conn.Write([]byte(c))
			time.Sleep(20 * time.Millisecond)
		}
	}()
	return ln.Addr().String()
}

// Регрессия #6: ответ терминала приходит двумя кусками. Драйвер обязан дочитать
// фрейм до терминатора '\r', иначе обрезанный "APPRO" дал бы ложный Approved=false
// уже после реального списания.
func TestAcquiring_Pay_ChunkedResponse(t *testing.T) {
	addr := chunkedTerminalServer(t, "APPRO", "VED RRN=123\r")

	dev, err := Open("acquiring_tcp", map[string]string{"порт": addr})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dev.Disconnect()
	res, err := dev.(PaymentTerminal).Pay(100)
	if err != nil {
		t.Fatalf("Pay: %v", err)
	}
	if !res.Approved {
		t.Errorf("ожидалось Approved=true при дочитанном ответе, got %+v", res)
	}
	if res.RRN != "123" {
		t.Errorf("RRN = %q, ожидался 123", res.RRN)
	}
}

func TestAcquiring_Pay_ZeroAmount(t *testing.T) {
	addr := terminalServer(t, "APPROVED\r\n")
	dev, err := Open("acquiring_tcp", map[string]string{"порт": addr})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dev.Disconnect()
	if _, err := dev.(PaymentTerminal).Pay(0); err == nil {
		t.Error("ожидалась ошибка для нулевой суммы")
	}
}
