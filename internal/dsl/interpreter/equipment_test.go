package interpreter_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runEqSrc(t *testing.T, src string, extra map[string]any) any {
	t.Helper()
	l := lexer.New(src, "test.os")
	p := parser.New(l)
	prog, err := p.ParseProgram()
	require.NoError(t, err)
	require.NotEmpty(t, prog.Procedures)

	interp := interpreter.New()
	var result any
	err = interp.RunWithResult(prog.Procedures[0], nil, &result, extra)
	require.NoError(t, err)
	return result
}

func captureTCP(t *testing.T) (string, <-chan []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
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

// Сквозной тест: DSL-код собирает чек и печатает его через драйвер escpos_tcp.
func TestEquipment_PrintReceipt_DSL(t *testing.T) {
	addr, received := captureTCP(t)

	src := fmt.Sprintf(`Функция Тест()
  Касса = ПодключитьОборудование("escpos_tcp", Новый Структура("Порт", "%s"));
  Чек = Новый Структура("Заголовок,Итого,Оплата", "ООО Ромашка", 60, "Наличные");
  Позиции = Новый Массив();
  Позиции.Добавить(Новый Структура("Наименование,Количество,Цена,Сумма", "Хлеб", 2, 30, 60));
  Чек.Вставить("Позиции", Позиции);
  Касса.НапечататьЧек(Чек);
  Касса.Отключить();
  Возврат "ок";
КонецФункции`, addr)

	result := runEqSrc(t, src, interpreter.NewEquipmentFunctions())
	assert.Equal(t, "ок", result)

	s := string(<-received)
	assert.Contains(t, s, "ООО Ромашка")
	assert.Contains(t, s, "Хлеб")
	assert.Contains(t, s, "Наличные")
}

// Тот же путь, что у РМК-обработки печать_чека: чек собирается присваиванием
// полей структуры (Чек.Поле = ...), а не конструктором с ключами — проверяем,
// что receiptFromArg читает такие поля.
func TestEquipment_PrintReceipt_FieldAssign_DSL(t *testing.T) {
	addr, received := captureTCP(t)

	src := fmt.Sprintf(`Функция Тест()
  Поз = Новый Структура;
  Поз.Наименование = "Молоко";
  Поз.Количество = 3;
  Поз.Цена = 50;
  Поз.Сумма = 150;
  Позиции = Новый Массив;
  Позиции.Добавить(Поз);
  Чек = Новый Структура;
  Чек.Заголовок = "Магазин у дома";
  Чек.Позиции = Позиции;
  Чек.Итого = 150;
  Чек.Оплата = "Карта";
  Касса = ПодключитьОборудование("escpos_tcp", Новый Структура("Порт", "%s"));
  Касса.НапечататьЧек(Чек);
  Касса.Отключить();
  Возврат "ок";
КонецФункции`, addr)

	result := runEqSrc(t, src, interpreter.NewEquipmentFunctions())
	assert.Equal(t, "ок", result)

	s := string(<-received)
	assert.Contains(t, s, "Магазин у дома")
	assert.Contains(t, s, "Молоко")
	assert.Contains(t, s, "Карта")
	assert.Contains(t, s, "150")
}

// Второй тип устройства через тот же DSL-объект: дисплей покупателя.
func TestEquipment_Display_DSL(t *testing.T) {
	addr, received := captureTCP(t)

	src := fmt.Sprintf(`Функция Тест()
  Дисплей = ПодключитьОборудование("display_tcp", Новый Структура("Порт", "%s"));
  Дисплей.Показать("Молоко 3 шт", "ИТОГО: 150 руб");
  Дисплей.Отключить();
  Возврат "ок";
КонецФункции`, addr)

	result := runEqSrc(t, src, interpreter.NewEquipmentFunctions())
	assert.Equal(t, "ок", result)

	s := string(<-received)
	assert.Contains(t, s, "Молоко 3 шт")
	assert.Contains(t, s, "ИТОГО: 150 руб")
}

// scaleTCP — эмулятор весов: отвечает строкой reply на запрос веса.
func scaleTCP(t *testing.T, reply string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
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

// Третий тип устройства — весы; вводит чтение значения (запрос-ответ).
func TestEquipment_Weight_DSL(t *testing.T) {
	addr := scaleTCP(t, "ST,GS,+001.250 kg\r\n")

	src := fmt.Sprintf(`Функция Тест()
  Весы = ПодключитьОборудование("scale_tcp", Новый Структура("Порт", "%s"));
  Вес = Весы.ПолучитьВес();
  Весы.Отключить();
  Возврат Вес;
КонецФункции`, addr)

	result := runEqSrc(t, src, interpreter.NewEquipmentFunctions())
	assert.Equal(t, 1.25, result)
}

// Четвёртый тип — эквайринг; метод возвращает структуру результата (одобрено/RRN).
// scaleTCP — generic эмулятор «запрос → ответ», годится и для терминала.
func TestEquipment_Pay_DSL(t *testing.T) {
	addr := scaleTCP(t, "APPROVED RRN=555000111222 CARD=****9876\r\n")

	src := fmt.Sprintf(`Функция Тест()
  Терминал = ПодключитьОборудование("acquiring_tcp", Новый Структура("Порт", "%s"));
  Результат = Терминал.ОплатитьКартой(150);
  Терминал.Отключить();
  Если Результат.Одобрено Тогда
    Возврат Результат.RRN;
  КонецЕсли;
  Возврат "отказ";
КонецФункции`, addr)

	result := runEqSrc(t, src, interpreter.NewEquipmentFunctions())
	assert.Equal(t, "555000111222", result)
}

// Декларативный драйвер: протокол весов задан ПАРАМЕТРАМИ из конфигурации,
// а не Go-кодом, но доступен через тот же метод ПолучитьВес. Добавление новой
// модели не требует пересборки платформы.
func TestEquipment_ScriptedScale_DSL(t *testing.T) {
	addr := scaleTCP(t, "ST,GS,+002500 g\r\n") // 2500 граммов

	src := fmt.Sprintf(`Функция Тест()
  Весы = ПодключитьОборудование("scripted", Новый Структура(
    "Порт,Запрос_hex,Шаблон,Множитель,Тип",
    "%s", "05", "[-+]?[0-9]+(?:[.,][0-9]+)?", "0.001", "весы"));
  Вес = Весы.ПолучитьВес();
  Весы.Отключить();
  Возврат Вес;
КонецФункции`, addr)

	result := runEqSrc(t, src, interpreter.NewEquipmentFunctions())
	assert.Equal(t, 2.5, result)
}

// Декларативный эквайринг через DSL: протокол оплаты задан параметрами,
// но доступен через тот же ОплатитьКартой. ([0-9] вместо \d — чтобы не
// зависеть от экранирования backslash в строках DSL.)
func TestEquipment_ScriptedPay_DSL(t *testing.T) {
	addr := scaleTCP(t, "APPROVED RRN=111222333 CARD=****0001\r\n")

	src := fmt.Sprintf(`Функция Тест()
  Терминал = ПодключитьОборудование("scripted_pay", Новый Структура(
    "Порт,ШаблонЗапроса,ПризнакОдобрения,ШаблонRRN",
    "%s", "PAY {amount}", "APPROVED", "RRN=([0-9]+)"));
  Результат = Терминал.ОплатитьКартой(300);
  Терминал.Отключить();
  Если Результат.Одобрено Тогда
    Возврат Результат.RRN;
  КонецЕсли;
  Возврат "отказ";
КонецФункции`, addr)

	result := runEqSrc(t, src, interpreter.NewEquipmentFunctions())
	assert.Equal(t, "111222333", result)
}

// atolEmulatorHTTP — эмулятор сервиса АТОЛ v10: принимает задание и отдаёт
// готовый фискальный результат на опрос статуса.
func atolEmulatorHTTP(t *testing.T) string {
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

// Шестой тип устройства — фискальный регистратор (ККТ): метод возвращает
// структуру с фискальными реквизитами пробитого чека (ФН/ФД/ФП).
func TestEquipment_Fiscal_DSL(t *testing.T) {
	url := atolEmulatorHTTP(t)

	src := fmt.Sprintf(`Функция Тест()
  ККТ = ПодключитьОборудование("atol_kkt", Новый Структура("Порт", "%s"));
  Чек = Новый Структура;
  Чек.Тип = "приход";
  Чек.Налогообложение = "уснДоход";
  Позиции = Новый Массив;
  Позиции.Добавить(Новый Структура("Наименование,Количество,Цена,Сумма,НДС,Предмет", "Хлеб", 2, 30, 60, "ндс10", "товар"));
  Чек.Позиции = Позиции;
  Результат = ККТ.ЗарегистрироватьЧек(Чек);
  ККТ.Отключить();
  Возврат Результат.ФД;
КонецФункции`, url)

	result := runEqSrc(t, src, interpreter.NewEquipmentFunctions())
	assert.Equal(t, "40", result)
}

// Декларативный дисплей покупателя через DSL: протокол (hex-команды + {text})
// и кодировка заданы параметрами, но доступны через тот же метод Показать —
// драйвер scripted_display реализует CustomerDisplay, слой DSL не меняется.
func TestEquipment_ScriptedDisplay_DSL(t *testing.T) {
	addr, received := captureTCP(t)

	src := fmt.Sprintf(`Функция Тест()
  Дисплей = ПодключитьОборудование("scripted_display", Новый Структура(
    "Порт,КомандаИниц,КомандаОчистки,ШаблонСтроки1,ШаблонСтроки2,Ширина",
    "%s", "1B40", "0C", "1B5141{text}0D", "1B5142{text}0D", "20"));
  Дисплей.Показать("Молоко", "ИТОГО 150");
  Дисплей.Отключить();
  Возврат "ок";
КонецФункции`, addr)

	result := runEqSrc(t, src, interpreter.NewEquipmentFunctions())
	assert.Equal(t, "ок", result)

	got := string(<-received)
	assert.Contains(t, got, "\x1bQA", "нет hex-префикса верхней строки (ESC Q A)")
	assert.Contains(t, got, "\x1bQB", "нет hex-префикса нижней строки (ESC Q B)")
}

// ПодключитьОборудование должна быть известна синтакс-чекеру: иначе обработки/
// формы конфигураций, вызывающие оборудование, валятся в onebase check как
// «unknown function» (функция инжектится в рантайм через dslvars).
func TestEquipment_InKnownBuiltins(t *testing.T) {
	known := interpreter.KnownBuiltinNames()
	for _, name := range []string{"подключитьоборудование", "connectequipment"} {
		if _, ok := known[name]; !ok {
			t.Errorf("KnownBuiltinNames не содержит %q — onebase check будет ругаться на конфигурации с оборудованием", name)
		}
	}
}
