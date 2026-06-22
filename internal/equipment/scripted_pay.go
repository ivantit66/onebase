package equipment

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

func init() {
	Register("scripted_pay", func() Device { return &scriptedPayDevice{timeout: 90 * time.Second} })
}

// scriptedPayDevice — декларативный драйвер эквайринга: текстовый протокол
// оплаты задан ПАРАМЕТРАМИ (ШаблонЗапроса с плейсхолдером {amount},
// ПризнакОдобрения, шаблоны RRN/Карты), а не Go-кодом. Реализует
// PaymentTerminal → работает через DSL ОплатитьКартой и агент /pay без их
// изменения. Завершает декларативную историю: весы дают число, эквайринг —
// составной результат, и оба описываются данными в конфигурации.
type scriptedPayDevice struct {
	conn        rwTransport
	timeout     time.Duration
	reqTemplate string // "PAY {amount}"
	approveMark string // "APPROVED" (сравнение в верхнем регистре)
	rrnRe       *regexp.Regexp
	cardRe      *regexp.Regexp
}

func (d *scriptedPayDevice) Kind() string { return "эквайринг" }

func (d *scriptedPayDevice) Connect(params map[string]string) error {
	d.reqTemplate = firstNonEmpty(params["шаблонзапроса"], params["request_template"])
	if d.reqTemplate == "" {
		d.reqTemplate = "PAY {amount}"
	}
	d.approveMark = strings.ToUpper(firstNonEmpty(params["признакодобрения"], params["approve_mark"]))
	if d.approveMark == "" {
		d.approveMark = "APPROVED"
	}
	if p := firstNonEmpty(params["шаблонrrn"], params["rrn_pattern"]); p != "" {
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("scripted_pay: неверный ШаблонRRN: %w", err)
		}
		d.rrnRe = re
	}
	if p := firstNonEmpty(params["шаблонкарты"], params["card_pattern"]); p != "" {
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("scripted_pay: неверный ШаблонКарты: %w", err)
		}
		d.cardRe = re
	}
	conn, err := openRWTransport(params)
	if err != nil {
		return err
	}
	d.conn = conn
	return nil
}

func (d *scriptedPayDevice) Disconnect() error {
	if d.conn == nil {
		return nil
	}
	err := d.conn.Close()
	d.conn = nil
	return err
}

func (d *scriptedPayDevice) Pay(amount float64) (PaymentResult, error) {
	if d.conn == nil {
		return PaymentResult{}, fmt.Errorf("устройство не подключено")
	}
	if amount <= 0 {
		return PaymentResult{}, fmt.Errorf("эквайринг: сумма должна быть больше нуля")
	}
	if err := d.conn.SetReadTimeout(d.timeout); err != nil {
		return PaymentResult{}, err
	}
	req := strings.ReplaceAll(d.reqTemplate, "{amount}", fmt.Sprintf("%.2f", amount)) + "\r"
	if _, err := d.conn.Write([]byte(req)); err != nil {
		return PaymentResult{}, err
	}
	raw, err := readFrame(d.conn)
	if err != nil {
		return PaymentResult{}, fmt.Errorf("эквайринг: чтение ответа: %w", err)
	}
	resp := strings.TrimSpace(string(raw))
	if resp == "" {
		return PaymentResult{}, fmt.Errorf("эквайринг: пустой ответ терминала")
	}
	return PaymentResult{
		Approved: strings.Contains(strings.ToUpper(resp), d.approveMark),
		RRN:      firstGroupOrMatch(d.rrnRe, resp),
		Card:     firstGroupOrMatch(d.cardRe, resp),
		Message:  resp,
	}, nil
}

// firstGroupOrMatch возвращает первую группу захвата шаблона (если есть) или всё
// совпадение; пустую строку, если шаблон не задан или не совпал.
func firstGroupOrMatch(re *regexp.Regexp, s string) string {
	if re == nil {
		return ""
	}
	if m := re.FindStringSubmatch(s); len(m) >= 2 {
		return m[1]
	} else if len(m) == 1 {
		return m[0]
	}
	return ""
}
