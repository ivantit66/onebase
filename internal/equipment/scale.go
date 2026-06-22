package equipment

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func init() {
	Register("scale_tcp", func() Device { return &scaleDevice{timeout: 3 * time.Second} })
}

// scaleDevice — драйвер электронных весов поверх TCP. В отличие от принтера и
// дисплея это двунаправленный обмен: драйвер шлёт запрос (ENQ) и читает ответ,
// поэтому хранит net.Conn (нужен Read), а не io.WriteCloser.
//
// Формат ответа зависит от модели; parseWeight извлекает первое число из строки
// вида "ST,GS,+000.250 kg" или просто "0.250".
type scaleDevice struct {
	conn    rwTransport
	timeout time.Duration
}

// scaleEnq — запрос текущего веса. ENQ (0x05) понимают многие POS-весы;
// для конкретной модели команда может отличаться.
var scaleEnq = []byte{0x05}

func (d *scaleDevice) Kind() string { return "весы" }

func (d *scaleDevice) Connect(params map[string]string) error {
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

func (d *scaleDevice) Disconnect() error {
	if d.conn == nil {
		return nil
	}
	err := d.conn.Close()
	d.conn = nil
	return err
}

// Weight запрашивает текущий вес: шлёт ENQ и парсит ответ весов.
func (d *scaleDevice) Weight() (float64, error) {
	if d.conn == nil {
		return 0, fmt.Errorf("устройство не подключено")
	}
	if err := d.conn.SetReadTimeout(d.timeout); err != nil {
		return 0, err
	}
	if _, err := d.conn.Write(scaleEnq); err != nil {
		return 0, err
	}
	raw, err := readFrame(d.conn)
	if err != nil {
		return 0, fmt.Errorf("весы: чтение ответа: %w", err)
	}
	return parseWeight(string(raw))
}

var weightRe = regexp.MustCompile(`[-+]?[0-9]+(?:[.,][0-9]+)?`)

// parseWeight извлекает первое число из ответа весов и возвращает его в кг.
func parseWeight(s string) (float64, error) {
	m := weightRe.FindString(s)
	if m == "" {
		return 0, fmt.Errorf("весы: не удалось разобрать вес из %q", strings.TrimSpace(s))
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(m, ",", "."), 64)
	if err != nil {
		return 0, fmt.Errorf("весы: %w", err)
	}
	return f, nil
}
