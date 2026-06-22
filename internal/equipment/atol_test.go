package equipment

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// atolEmulator — эмулятор сервиса «Драйвер ККТ АТОЛ v10»: принимает задание
// POST /api/v2/requests, отдаёт готовый фискальный результат на опрос GET.
// Тело принятого задания сохраняется в gotTask для проверки маппинга.
func atolEmulator(t *testing.T, fiscal string) (string, *atolTask) {
	t.Helper()
	var got atolTask
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/requests", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("эмулятор: некорректное задание: %v", err)
		}
		w.Write([]byte(`{"isError":false}`))
	})
	mux.HandleFunc("/api/v2/requests/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ready":true,"isError":false,"results":[{"result":` + fiscal + `}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, &got
}

func sampleFiscalReceipt() FiscalReceipt {
	return FiscalReceipt{
		Type:     "приход",
		Taxation: "уснДоход",
		Items: []FiscalItem{
			{Name: "Хлеб", Qty: 2, Price: 30, Sum: 60, VAT: "ндс10", ItemType: "товар", PaymentType: "полныйРасчёт"},
		},
		Payments: []FiscalPayment{{Type: "наличные", Sum: 60}},
	}
}

func TestAtol_RegisterReceipt_Result(t *testing.T) {
	url, _ := atolEmulator(t, `{"fnNumber":"9999078900012345","fiscalDocumentNumber":40,"fiscalDocumentSign":"2143256432","receiptDatetime":"2026-06-19T17:30:00+03:00","total":60}`)

	dev, err := Open("atol_kkt", map[string]string{"порт": url})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dev.Disconnect()
	kkt, ok := dev.(FiscalRegistrar)
	if !ok {
		t.Fatal("устройство не реализует FiscalRegistrar")
	}

	res, err := kkt.RegisterReceipt(sampleFiscalReceipt())
	if err != nil {
		t.Fatalf("RegisterReceipt: %v", err)
	}
	if res.FN != "9999078900012345" {
		t.Errorf("FN = %q", res.FN)
	}
	if res.FD != "40" {
		t.Errorf("FD = %q, ожидался 40", res.FD)
	}
	if res.FP != "2143256432" {
		t.Errorf("FP = %q", res.FP)
	}
	want := "t=20260619T1730&s=60.00&fn=9999078900012345&i=40&fp=2143256432&n=1"
	if res.QR != want {
		t.Errorf("QR =\n  %q\nожидался\n  %q", res.QR, want)
	}
}

func TestAtol_TaskMapping(t *testing.T) {
	url, got := atolEmulator(t, `{"fnNumber":"1","fiscalDocumentNumber":1,"fiscalDocumentSign":"1"}`)
	dev, err := Open("atol_kkt", map[string]string{"порт": url})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dev.Disconnect()
	if _, err := dev.(FiscalRegistrar).RegisterReceipt(sampleFiscalReceipt()); err != nil {
		t.Fatalf("RegisterReceipt: %v", err)
	}

	if got.UUID == "" {
		t.Error("в задании нет uuid")
	}
	if len(got.Request) != 1 {
		t.Fatalf("ожидался 1 документ, получено %d", len(got.Request))
	}
	doc := got.Request[0]
	if doc.Type != "sell" {
		t.Errorf("type = %q, ожидался sell", doc.Type)
	}
	if doc.TaxationType != "usnIncome" {
		t.Errorf("taxationType = %q, ожидался usnIncome", doc.TaxationType)
	}
	if doc.Total != 60 {
		t.Errorf("total = %v, ожидался 60", doc.Total)
	}
	if len(doc.Items) != 1 {
		t.Fatalf("ожидалась 1 позиция, получено %d", len(doc.Items))
	}
	it := doc.Items[0]
	if it.Type != "position" || it.Name != "Хлеб" || it.Amount != 60 {
		t.Errorf("позиция разобрана неверно: %+v", it)
	}
	if it.Tax.Type != "vat10" {
		t.Errorf("tax = %q, ожидался vat10", it.Tax.Type)
	}
	if it.PaymentObject != "commodity" || it.PaymentMethod != "fullPayment" {
		t.Errorf("признаки расчёта неверны: object=%q method=%q", it.PaymentObject, it.PaymentMethod)
	}
	if len(doc.Payments) != 1 || doc.Payments[0].Type != "cash" || doc.Payments[0].Sum != 60 {
		t.Errorf("оплаты разобраны неверно: %+v", doc.Payments)
	}
}

// Возврат прихода и безналичная оплата без явных позиций-сумм: проверяем
// маппинг типа операции, вида оплаты и автodополнение оплаты до итога.
func TestAtol_ReturnAndDefaults(t *testing.T) {
	url, got := atolEmulator(t, `{"fnNumber":"1","fiscalDocumentNumber":1,"fiscalDocumentSign":"1"}`)
	dev, _ := Open("atol_kkt", map[string]string{"порт": url})
	defer dev.Disconnect()

	r := FiscalReceipt{
		Type:     "возвратПрихода",
		Taxation: "осн",
		Items:    []FiscalItem{{Name: "Возврат", Qty: 1, Price: 100}}, // Sum не задан → Qty*Price
	}
	if _, err := dev.(FiscalRegistrar).RegisterReceipt(r); err != nil {
		t.Fatalf("RegisterReceipt: %v", err)
	}
	doc := got.Request[0]
	if doc.Type != "sellReturn" {
		t.Errorf("type = %q, ожидался sellReturn", doc.Type)
	}
	if doc.Items[0].Amount != 100 {
		t.Errorf("amount = %v, ожидался 100 (Qty*Price)", doc.Items[0].Amount)
	}
	if len(doc.Payments) != 1 || doc.Payments[0].Sum != 100 {
		t.Errorf("оплата не автодополнена до итога: %+v", doc.Payments)
	}
}

func TestAtol_ServiceError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/requests", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"isError":false}`))
	})
	mux.HandleFunc("/api/v2/requests/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ready":true,"isError":true,"results":[{"error":{"description":"нет бумаги"}}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dev, _ := Open("atol_kkt", map[string]string{"порт": srv.URL})
	defer dev.Disconnect()
	_, err := dev.(FiscalRegistrar).RegisterReceipt(sampleFiscalReceipt())
	if err == nil || !strings.Contains(err.Error(), "нет бумаги") {
		t.Errorf("ожидалась ошибка с описанием от ККТ, получено: %v", err)
	}
}

func TestAtol_NoItems(t *testing.T) {
	url, _ := atolEmulator(t, `{}`)
	dev, _ := Open("atol_kkt", map[string]string{"порт": url})
	defer dev.Disconnect()
	if _, err := dev.(FiscalRegistrar).RegisterReceipt(FiscalReceipt{Type: "приход"}); err == nil {
		t.Error("ожидалась ошибка для чека без позиций")
	}
}

// Регрессия (безопасность 54-ФЗ): задание принято (POST ok), но опрос статуса
// падает — чек мог быть пробит в ФН. Драйвер обязан вернуть FiscalStateUnknownError
// c uuid, а не обычную ошибку, чтобы вызывающий не повторил пробитие вслепую.
func TestAtol_StateUnknownOnPollFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/requests", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"isError":false}`)) // задание принято — чек ушёл в ФН
	})
	mux.HandleFunc("/api/v2/requests/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "сервис недоступен", http.StatusInternalServerError) // опрос падает
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dev, _ := Open("atol_kkt", map[string]string{"порт": srv.URL})
	defer dev.Disconnect()
	_, err := dev.(FiscalRegistrar).RegisterReceipt(sampleFiscalReceipt())
	var unknown *FiscalStateUnknownError
	if !errors.As(err, &unknown) {
		t.Fatalf("ожидалась *FiscalStateUnknownError, получено: %v", err)
	}
	if unknown.UUID == "" {
		t.Error("в ошибке нет uuid для ручной сверки чека")
	}
}

// Регрессия #4: денежные величины округляются до копеек. Позиция 12.45 × 7 даёт
// 87.14999… в float64 — без round2 это переживает json.Marshal (уйдёт в ФН на
// копейку ниже), а QR считается через %.2f и даёт 87.15 → ФН и QR расходятся.
// Проверяем: Amount/Total сериализуются как "87.15" И совпадают со значением в QR.
func TestAtol_RoundsMoneyToKopecks(t *testing.T) {
	url, got := atolEmulator(t, `{"fnNumber":"9999078900012345","fiscalDocumentNumber":7,"fiscalDocumentSign":"111","receiptDatetime":"2026-06-19T10:00:00+03:00","total":87.15}`)
	dev, _ := Open("atol_kkt", map[string]string{"порт": url})
	defer dev.Disconnect()

	r := FiscalReceipt{
		Type:     "приход",
		Taxation: "осн",
		Items:    []FiscalItem{{Name: "Товар", Qty: 7, Price: 12.45, VAT: "ндс20"}},
		Payments: []FiscalPayment{{Type: "наличные", Sum: 87.15}},
	}
	res, err := dev.(FiscalRegistrar).RegisterReceipt(r)
	if err != nil {
		t.Fatalf("RegisterReceipt: %v", err)
	}

	// Сериализованное задание (то, что реально уходит в ФН) не должно нести шум.
	body, _ := json.Marshal(*got)
	js := string(body)
	if !strings.Contains(js, `"amount":87.15`) {
		t.Errorf("amount позиции сериализован с шумом, ожидался 87.15 в:\n%s", js)
	}
	if !strings.Contains(js, `"total":87.15`) {
		t.Errorf("total сериализован с шумом, ожидался 87.15 в:\n%s", js)
	}
	if got.Request[0].Items[0].Amount != 87.15 {
		t.Errorf("Amount = %v, ожидался 87.15", got.Request[0].Items[0].Amount)
	}
	if got.Request[0].Total != 87.15 {
		t.Errorf("Total = %v, ожидался 87.15", got.Request[0].Total)
	}
	// QR должен нести ту же сумму, что и Total задания ФН.
	if !strings.Contains(res.QR, "s=87.15&") {
		t.Errorf("QR не совпадает с суммой ФН (ожидалось s=87.15): %q", res.QR)
	}
}

// Регрессия #5: сумма позиций не сходится с суммой оплат → чек не сбалансирован,
// ошибка ДО обращения к устройству (POST не выполняется).
func TestAtol_RejectsUnbalancedReceipt(t *testing.T) {
	posted := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/requests", func(w http.ResponseWriter, r *http.Request) {
		posted = true
		w.Write([]byte(`{"isError":false}`))
	})
	mux.HandleFunc("/api/v2/requests/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ready":true,"isError":false,"results":[{"result":{}}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dev, _ := Open("atol_kkt", map[string]string{"порт": srv.URL})
	defer dev.Disconnect()

	// Позиции на 100, оплата на 90 — расхождение должно отсекаться валидацией.
	r := FiscalReceipt{
		Type:     "приход",
		Taxation: "осн",
		Items:    []FiscalItem{{Name: "Товар", Qty: 1, Price: 100, Sum: 100}},
		Payments: []FiscalPayment{{Type: "наличные", Sum: 90}},
	}
	if _, err := dev.(FiscalRegistrar).RegisterReceipt(r); err == nil {
		t.Error("ожидалась ошибка для несбалансированного чека")
	}
	if posted {
		t.Error("POST не должен выполняться при несбалансированном чеке")
	}
}

// Регрессия #24: два RegisterReceipt с одинаковым IdempotencyKey формируют
// задание с одинаковым uuid (ФН не пробьёт дубль при повторе).
func TestAtol_IdempotencyKeyReusesUUID(t *testing.T) {
	url, got := atolEmulator(t, `{"fnNumber":"1","fiscalDocumentNumber":1,"fiscalDocumentSign":"1"}`)
	dev, _ := Open("atol_kkt", map[string]string{"порт": url})
	defer dev.Disconnect()
	kkt := dev.(FiscalRegistrar)

	r := sampleFiscalReceipt()
	r.IdempotencyKey = "11111111-2222-4333-8444-555555555555"

	if _, err := kkt.RegisterReceipt(r); err != nil {
		t.Fatalf("RegisterReceipt #1: %v", err)
	}
	first := got.UUID
	if first != r.IdempotencyKey {
		t.Errorf("uuid задания = %q, ожидался ключ %q", first, r.IdempotencyKey)
	}
	if _, err := kkt.RegisterReceipt(r); err != nil {
		t.Fatalf("RegisterReceipt #2: %v", err)
	}
	if got.UUID != first {
		t.Errorf("повтор с тем же ключом дал другой uuid: %q != %q", got.UUID, first)
	}
}

// ResolveByUUID дозапрашивает результат по сохранённому uuid, не пробивая чек.
// Вызывается через интерфейс FiscalRegistrar (контракт #24): метод восстановления
// после FiscalStateUnknownError должен быть доступен любому фискальному драйверу,
// а не только конкретному *atolDevice.
func TestAtol_ResolveByUUID(t *testing.T) {
	url, _ := atolEmulator(t, `{"fnNumber":"9999078900012345","fiscalDocumentNumber":42,"fiscalDocumentSign":"222","receiptDatetime":"2026-06-19T10:00:00+03:00","total":60}`)
	dev, _ := Open("atol_kkt", map[string]string{"порт": url})
	defer dev.Disconnect()
	kkt := dev.(FiscalRegistrar)

	res, err := kkt.ResolveByUUID("some-saved-uuid")
	if err != nil {
		t.Fatalf("ResolveByUUID: %v", err)
	}
	if res.FD != "42" {
		t.Errorf("FD = %q, ожидался 42", res.FD)
	}
	if _, err := kkt.ResolveByUUID("  "); err == nil {
		t.Error("ожидалась ошибка для пустого uuid")
	}
}

// Валидация ДО обращения к ФН: некорректный чек (нулевой итог, отрицательные
// значения) должен отсекаться драйвером, а не уходить на фискализацию.
func TestAtol_RejectsInvalidReceipt(t *testing.T) {
	url, _ := atolEmulator(t, `{}`)
	dev, _ := Open("atol_kkt", map[string]string{"порт": url})
	defer dev.Disconnect()
	kkt := dev.(FiscalRegistrar)

	if _, err := kkt.RegisterReceipt(FiscalReceipt{Type: "приход", Items: []FiscalItem{{Name: "X", Qty: 1, Price: 0}}}); err == nil {
		t.Error("ожидалась ошибка для нулевого итога чека")
	}
	neg := FiscalReceipt{
		Type:     "приход",
		Payments: []FiscalPayment{{Type: "наличные", Sum: 100}},
		Items:    []FiscalItem{{Name: "X", Qty: -1, Price: 100, Sum: 100}},
	}
	if _, err := kkt.RegisterReceipt(neg); err == nil {
		t.Error("ожидалась ошибка для отрицательного количества")
	}
}
