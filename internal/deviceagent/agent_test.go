package deviceagent

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// captureServer — TCP-эмулятор сетевого принтера (железо не требуется).
func captureServer(t *testing.T) (string, <-chan []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	out := make(chan []byte, 1)
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			out <- nil
			return
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(time.Second))
		data, _ := io.ReadAll(conn)
		out <- data
	}()
	return ln.Addr().String(), out
}

func post(t *testing.T, url, token, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	if token != "" {
		req.Header.Set("X-Agent-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// Сквозь весь стек: HTTP-команда → агент → драйвер escpos_tcp → сокет-принтер.
func TestAgent_Print(t *testing.T) {
	addr, received := captureServer(t)
	srv := httptest.NewServer(New("secret").Handler())
	defer srv.Close()

	body := `{"driver":"escpos_tcp","params":{"порт":"` + addr + `"},` +
		`"receipt":{"header":["Магазин"],"items":[{"name":"Хлеб","qty":2,"price":30,"sum":60}],"total":60,"payment":"Наличные"}}`
	resp := post(t, srv.URL+"/print", "secret", body)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("статус %d: %s", resp.StatusCode, b)
	}

	got := <-received
	for _, want := range []string{"Магазин", "Хлеб", "ИТОГО:", "Наличные"} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("в чеке нет %q", want)
		}
	}
}

func TestAgent_Print_NoToken(t *testing.T) {
	srv := httptest.NewServer(New("secret").Handler())
	defer srv.Close()

	resp := post(t, srv.URL+"/print", "", `{"driver":"escpos_tcp","params":{}}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("ожидался 401, получен %d", resp.StatusCode)
	}
}

func TestAgent_Health(t *testing.T) {
	srv := httptest.NewServer(New("secret").Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("статус %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(b, []byte("escpos_tcp")) {
		t.Errorf("health не содержит escpos_tcp: %s", b)
	}
}

func TestAgent_Drawer(t *testing.T) {
	addr, received := captureServer(t)
	srv := httptest.NewServer(New("secret").Handler())
	defer srv.Close()

	resp := post(t, srv.URL+"/drawer", "secret", `{"driver":"escpos_tcp","params":{"порт":"`+addr+`"}}`)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("статус %d: %s", resp.StatusCode, b)
	}

	got := <-received
	want := []byte{0x1B, 0x70, 0x00, 0x19, 0xFA} // ESC p 0 25 250 — импульс денежного ящика
	if !bytes.Equal(got, want) {
		t.Errorf("импульс ящика = % x, ожидался % x", got, want)
	}
}

func TestAgent_Display(t *testing.T) {
	addr, received := captureServer(t)
	srv := httptest.NewServer(New("secret").Handler())
	defer srv.Close()

	body := `{"driver":"display_tcp","params":{"порт":"` + addr + `"},"lines":["Добро пожаловать","Касса №1"]}`
	resp := post(t, srv.URL+"/display", "secret", body)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("статус %d: %s", resp.StatusCode, b)
	}

	got := <-received
	for _, want := range []string{"Добро пожаловать", "Касса №1"} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("на дисплее нет %q", want)
		}
	}
}

// scaleTCP — эмулятор весов: отвечает строкой reply на запрос веса.
func scaleTCP(t *testing.T, reply string) string {
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
		buf := make([]byte, 16)
		conn.Read(buf)
		conn.Write([]byte(reply))
	}()
	return ln.Addr().String()
}

func TestAgent_Weight(t *testing.T) {
	addr := scaleTCP(t, "ST,GS,+002.500 kg\r\n")
	srv := httptest.NewServer(New("secret").Handler())
	defer srv.Close()

	resp := post(t, srv.URL+"/weight", "secret", `{"driver":"scale_tcp","params":{"порт":"`+addr+`"}}`)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("статус %d: %s", resp.StatusCode, b)
	}
	b, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(b, []byte(`"weight":2.5`)) {
		t.Errorf("ответ не содержит weight=2.5: %s", b)
	}
}

func TestAgent_Pay(t *testing.T) {
	addr := scaleTCP(t, "APPROVED RRN=777 CARD=****4321\r\n") // generic request→reply эмулятор
	srv := httptest.NewServer(New("secret").Handler())
	defer srv.Close()

	resp := post(t, srv.URL+"/pay", "secret", `{"driver":"acquiring_tcp","params":{"порт":"`+addr+`"},"amount":250}`)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("статус %d: %s", resp.StatusCode, b)
	}
	b, _ := io.ReadAll(resp.Body)
	for _, want := range []string{`"approved":true`, `"rrn":"777"`} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("ответ не содержит %s: %s", want, b)
		}
	}
}

// atolEmulator — эмулятор сервиса АТОЛ v10 для агентского теста /fiscal.
func atolEmulator(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/requests", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"isError":false}`))
	})
	mux.HandleFunc("/api/v2/requests/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ready":true,"isError":false,"results":[{"result":` +
			`{"fnNumber":"9999078900012345","fiscalDocumentNumber":40,"fiscalDocumentSign":"2143256432"}}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// Сквозь весь стек: HTTP-команда → агент → драйвер atol_kkt → эмулятор АТОЛ.
func TestAgent_Fiscal(t *testing.T) {
	atolURL := atolEmulator(t)
	srv := httptest.NewServer(New("secret").Handler())
	defer srv.Close()

	body := `{"driver":"atol_kkt","params":{"порт":"` + atolURL + `"},` +
		`"receipt":{"type":"приход","taxation":"уснДоход",` +
		`"items":[{"name":"Хлеб","qty":2,"price":30,"sum":60,"vat":"ндс10","itemType":"товар"}],` +
		`"payments":[{"type":"наличные","sum":60}]}}`
	resp := post(t, srv.URL+"/fiscal", "secret", body)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("статус %d: %s", resp.StatusCode, b)
	}
	b, _ := io.ReadAll(resp.Body)
	for _, want := range []string{`"fn":"9999078900012345"`, `"fd":"40"`, `"fp":"2143256432"`} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("ответ не содержит %s: %s", want, b)
		}
	}
}

// Безопасность 54-ФЗ: агент без токена обязан ОТКЛОНЯТЬ фискализацию и оплату.
// При пустом токене auth-middleware пропускает всё, поэтому денежные маршруты
// защищены отдельной проверкой (иначе любая вкладка в браузере кассы пробила бы
// чек/оплату).
func TestAgent_Fiscal_Pay_RequireToken(t *testing.T) {
	srv := httptest.NewServer(New("").Handler()) // агент запущен без токена
	defer srv.Close()

	resp := post(t, srv.URL+"/fiscal", "", `{"driver":"atol_kkt","params":{}}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("/fiscal без токена: статус %d, ожидался 403", resp.StatusCode)
	}

	resp2 := post(t, srv.URL+"/pay", "", `{"driver":"acquiring_tcp","params":{},"amount":100}`)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("/pay без токена: статус %d, ожидался 403", resp2.StatusCode)
	}
}

// SSE: события сканера долетают до клиента через text/event-stream (push-канал).
func TestAgent_Events_SSE(t *testing.T) {
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
		conn.Write([]byte("AAA111\nBBB222\n"))
		conn.Close()
	}()

	srv := httptest.NewServer(New("").Handler()) // без токена
	defer srv.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(srv.URL + "/events?driver=scanner_tcp&port=" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q", ct)
	}

	var codes []string
	rd := bufio.NewReader(resp.Body)
	for len(codes) < 2 {
		line, err := rd.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "data: ") {
			codes = append(codes, strings.TrimPrefix(line, "data: "))
		}
	}
	if len(codes) != 2 || codes[0] != "AAA111" || codes[1] != "BBB222" {
		t.Errorf("SSE-события = %v, ожидались [AAA111 BBB222]", codes)
	}
}

func TestAgent_Page(t *testing.T) {
	srv := httptest.NewServer(New("secret").Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("статус %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
	b, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"Рабочее место кассира", "Напечатать чек", "Получить вес"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("страница не содержит %q", want)
		}
	}
}

// #7: токен сравнивается constant-time; семантика прежняя — верный проходит,
// неверный отклоняется, а пустой токен агента отключает проверку.
func TestAgent_Token_ConstantTime_Semantics(t *testing.T) {
	a := New("secret")
	if !a.tokenOK("secret") {
		t.Error("верный токен должен проходить")
	}
	if a.tokenOK("wrong") {
		t.Error("неверный токен не должен проходить")
	}
	if a.tokenOK("") {
		t.Error("пустой предъявленный токен не должен проходить при заданном токене агента")
	}

	open := New("")
	if !open.tokenOK("") || !open.tokenOK("что-угодно") {
		t.Error("пустой токен агента должен отключать проверку (доступ открыт)")
	}
}

// #8: при заданном токене ВСЕ командные маршруты железа закрыты без токена.
func TestAgent_AllHardwareRoutes_RequireToken(t *testing.T) {
	srv := httptest.NewServer(New("secret").Handler())
	defer srv.Close()

	for _, path := range []string{"/print", "/drawer", "/display", "/weight", "/pay", "/fiscal"} {
		resp := post(t, srv.URL+path, "", `{"driver":"escpos_tcp","params":{}}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s без токена: статус %d, ожидался 401", path, resp.StatusCode)
		}
	}
}

// #8: /events без верного токена (при заданном токене агента) отклоняется.
func TestAgent_Events_RequireToken(t *testing.T) {
	srv := httptest.NewServer(New("secret").Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events?driver=scanner_tcp&port=127.0.0.1:9103")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/events без токена: статус %d, ожидался 401", resp.StatusCode)
	}
}

// #8 (анти-SSRF): без токена /events нельзя направить на внешний host:port.
func TestAgent_Events_AntiSSRF_External(t *testing.T) {
	srv := httptest.NewServer(New("").Handler()) // без токена
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events?driver=scanner_tcp&port=93.184.216.34:80")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("/events на внешний адрес: статус %d, ожидался 403 (%s)", resp.StatusCode, b)
	}
}

// #8 (анти-SSRF): loopback-адрес при пустом токене по-прежнему разрешён.
func TestAgent_Events_AntiSSRF_LoopbackAllowed(t *testing.T) {
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
		conn.Write([]byte("AAA111\n"))
		conn.Close()
	}()

	srv := httptest.NewServer(New("").Handler())
	defer srv.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(srv.URL + "/events?driver=scanner_tcp&port=" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/events на loopback: статус %d, ожидался 200", resp.StatusCode)
	}
}

// #8 (CORS): без токена чужой origin не получает CORS-разрешение, а локальный —
// получает (поверхность сужена, но локальная отладка работает).
func TestAgent_CORS_NoToken_OriginRestricted(t *testing.T) {
	srv := httptest.NewServer(New("").Handler())
	defer srv.Close()

	foreign, _ := http.NewRequest("OPTIONS", srv.URL+"/print", nil)
	foreign.Header.Set("Origin", "https://evil.example.com")
	fr, err := http.DefaultClient.Do(foreign)
	if err != nil {
		t.Fatal(err)
	}
	fr.Body.Close()
	if got := fr.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("чужой origin без токена не должен получать CORS, получено %q", got)
	}

	local, _ := http.NewRequest("OPTIONS", srv.URL+"/print", nil)
	local.Header.Set("Origin", "http://localhost:8080")
	lr, err := http.DefaultClient.Do(local)
	if err != nil {
		t.Fatal(err)
	}
	lr.Body.Close()
	if got := lr.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:8080" {
		t.Errorf("локальный origin должен получать CORS, получено %q", got)
	}
}

func TestAgent_CORS_Preflight(t *testing.T) {
	srv := httptest.NewServer(New("secret").Handler())
	defer srv.Close()

	req, _ := http.NewRequest("OPTIONS", srv.URL+"/print", nil)
	req.Header.Set("Origin", "http://localhost:8080")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("preflight статус %d, ожидался 204", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") == "" {
		t.Error("нет заголовка Access-Control-Allow-Origin")
	}
}
