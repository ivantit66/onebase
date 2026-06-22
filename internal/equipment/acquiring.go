package equipment

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func init() {
	Register("acquiring_tcp", func() Device { return &acquiringDevice{timeout: 90 * time.Second} })
}

// acquiringDevice — драйвер платёжного терминала (эквайринг) поверх TCP.
// Команда оплаты — "PAY <сумма>\r", ответ парсится parsePayment. Таймаут больше,
// чем у весов: операция включает действия держателя карты и связь с банком.
//
// Протоколы реальных терминалов (Сбербанк, Ingenico, INPAS) сложнее; здесь —
// обобщённый текстовый протокол запрос-ответ как основа для конкретных драйверов.
type acquiringDevice struct {
	conn    rwTransport
	timeout time.Duration
}

func (d *acquiringDevice) Kind() string { return "эквайринг" }

func (d *acquiringDevice) Connect(params map[string]string) error {
	if t := firstNonEmpty(params["таймаут"], params["timeout"]); t != "" {
		if n, err := strconv.Atoi(t); err == nil && n > 0 {
			d.timeout = time.Duration(n) * time.Second
		}
	}
	conn, err := openRWTransport(params)
	if err != nil {
		return err
	}
	d.conn = conn
	return nil
}

func (d *acquiringDevice) Disconnect() error {
	if d.conn == nil {
		return nil
	}
	err := d.conn.Close()
	d.conn = nil
	return err
}

// Pay инициирует оплату картой на сумму amount и возвращает результат операции.
func (d *acquiringDevice) Pay(amount float64) (PaymentResult, error) {
	if d.conn == nil {
		return PaymentResult{}, fmt.Errorf("устройство не подключено")
	}
	if amount <= 0 {
		return PaymentResult{}, fmt.Errorf("эквайринг: сумма должна быть больше нуля")
	}
	if err := d.conn.SetReadTimeout(d.timeout); err != nil {
		return PaymentResult{}, err
	}
	cmd := fmt.Sprintf("PAY %.2f\r", amount)
	if _, err := d.conn.Write([]byte(cmd)); err != nil {
		return PaymentResult{}, err
	}
	raw, err := readFrame(d.conn)
	if err != nil {
		return PaymentResult{}, fmt.Errorf("эквайринг: чтение ответа: %w", err)
	}
	return parsePayment(string(raw))
}

// parsePayment разбирает ответ терминала: одобрение (APPROVED/ОДОБРЕНО) и поля
// RRN/CARD. Сырой ответ сохраняется в Message.
func parsePayment(s string) (PaymentResult, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return PaymentResult{}, fmt.Errorf("эквайринг: пустой ответ терминала")
	}
	up := strings.ToUpper(s)
	return PaymentResult{
		Approved: strings.Contains(up, "APPROVED") || strings.Contains(up, "ОДОБРЕНО"),
		RRN:      extractField(s, "RRN"),
		Card:     extractField(s, "CARD"),
		Message:  s,
	}, nil
}

// extractField извлекает значение поля вида "KEY=VALUE", "KEY: VALUE" из ответа.
func extractField(s, key string) string {
	re := regexp.MustCompile(`(?i)` + key + `[=:\s]+(\S+)`)
	if m := re.FindStringSubmatch(s); len(m) >= 2 {
		return m[1]
	}
	return ""
}
