package equipment

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func init() {
	Register("scripted", func() Device { return &scriptedDevice{timeout: 3 * time.Second} })
}

// scriptedDevice — декларативный драйвер устройств «запрос-ответ», возвращающих
// число (весы и подобные). Протокол описывается ПАРАМЕТРАМИ из конфигурации,
// а не Go-кодом: добавить новую модель = задать другие Запрос/Шаблон/Множитель,
// без пересборки платформы. Это сдвигает простые протоколы в конфигурацию —
// аналог «внешней компоненты» 1С, только описанной данными.
//
// Поскольку драйвер реализует интерфейс Scale, он работает через существующий
// DSL-метод ПолучитьВес и агентский /weight без каких-либо их изменений.
type scriptedDevice struct {
	conn    rwTransport
	timeout time.Duration

	request []byte         // что отправить для запроса значения
	pattern *regexp.Regexp // как извлечь число из ответа
	factor  float64        // множитель результата (например, граммы→кг = 0.001)
	kind    string         // отображаемый тип устройства
}

func (d *scriptedDevice) Kind() string {
	if d.kind != "" {
		return d.kind
	}
	return "устройство"
}

func (d *scriptedDevice) Connect(params map[string]string) error {
	// Запрос: hex-строка (Запрос_hex, напр. "05" = ENQ) или сырой текст (Запрос).
	if h := firstNonEmpty(params["запрос_hex"], params["request_hex"]); h != "" {
		b, err := hex.DecodeString(h)
		if err != nil {
			return fmt.Errorf("scripted: неверный Запрос_hex: %w", err)
		}
		d.request = b
	} else {
		d.request = []byte(firstNonEmpty(params["запрос"], params["request"]))
	}

	pat := firstNonEmpty(params["шаблон"], params["pattern"])
	if pat == "" {
		pat = `[-+]?[0-9]+(?:[.,][0-9]+)?`
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return fmt.Errorf("scripted: неверный Шаблон: %w", err)
	}
	d.pattern = re

	d.factor = 1.0
	if f := firstNonEmpty(params["множитель"], params["factor"]); f != "" {
		if v, err := strconv.ParseFloat(f, 64); err == nil && v != 0 {
			d.factor = v
		}
	}
	d.kind = firstNonEmpty(params["тип"], params["kind"])

	conn, err := openRWTransport(params)
	if err != nil {
		return err
	}
	d.conn = conn
	return nil
}

func (d *scriptedDevice) Disconnect() error {
	if d.conn == nil {
		return nil
	}
	err := d.conn.Close()
	d.conn = nil
	return err
}

// Weight исполняет описанный протокол: шлёт Запрос, читает ответ, извлекает
// число по Шаблону и применяет Множитель.
func (d *scriptedDevice) Weight() (float64, error) {
	if d.conn == nil {
		return 0, fmt.Errorf("устройство не подключено")
	}
	if err := d.conn.SetReadTimeout(d.timeout); err != nil {
		return 0, err
	}
	if len(d.request) > 0 {
		if _, err := d.conn.Write(d.request); err != nil {
			return 0, err
		}
	}
	raw, err := readFrame(d.conn)
	if err != nil {
		return 0, fmt.Errorf("scripted: чтение ответа: %w", err)
	}
	resp := string(raw)
	m := d.pattern.FindString(resp)
	if m == "" {
		return 0, fmt.Errorf("scripted: значение не найдено в %q", strings.TrimSpace(resp))
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m, ",", "."), 64)
	if err != nil {
		return 0, fmt.Errorf("scripted: %w", err)
	}
	return v * d.factor, nil
}
