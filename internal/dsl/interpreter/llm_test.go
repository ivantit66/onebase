package interpreter_test

import (
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAI реализует interpreter.AIAssistant для тестов: запоминает последний
// запрос и отдаёт заранее заданный ответ.
type stubAI struct {
	configured bool
	last       interpreter.AIRequest
	reply      string
	calls      int
}

func (s *stubAI) Configured() bool { return s.configured }
func (s *stubAI) Ask(req interpreter.AIRequest) (string, error) {
	s.last = req
	s.calls++
	return s.reply, nil
}

func TestЗапросИИ(t *testing.T) {
	ai := &stubAI{configured: true, reply: "Закупить: гвозди 100 шт"}
	src := `Процедура Тест()
  Возврат ЗапросИИ("Что закупить?");
КонецПроцедуры`
	result := runHTTPSrc(t, src, interpreter.NewLLMFunctions(ai))
	assert.Equal(t, "Закупить: гвозди 100 шт", result)
	assert.Equal(t, 1, ai.calls)
	assert.Equal(t, "Что закупить?", ai.last.Prompt)
	assert.Equal(t, "анализ", ai.last.Task)
	assert.False(t, ai.last.JSON)
}

func TestЗапросИИСПараметрами(t *testing.T) {
	ai := &stubAI{configured: true, reply: "{}"}
	src := `Процедура Тест()
  П = Новый Структура("Задача, Система, Формат", "документы", "Ты бухгалтер", "json");
  Возврат ЗапросИИ("Разбери", П);
КонецПроцедуры`
	runHTTPSrc(t, src, interpreter.NewLLMFunctions(ai))
	assert.Equal(t, "документы", ai.last.Task)
	assert.Equal(t, "Ты бухгалтер", ai.last.System)
	assert.True(t, ai.last.JSON)
}

func TestЗапросИИДжейсонВключаетJSON(t *testing.T) {
	ai := &stubAI{configured: true, reply: "[]"}
	src := `Процедура Тест()
  Возврат ЗапросИИДжейсон("дай список");
КонецПроцедуры`
	runHTTPSrc(t, src, interpreter.NewLLMFunctions(ai))
	assert.True(t, ai.last.JSON)
}

func TestРаспознатьИзображение(t *testing.T) {
	ai := &stubAI{configured: true, reply: `{"поставщик":"ООО Ромашка"}`}
	src := `Процедура Тест()
  Возврат РаспознатьИзображение("QUJD", "image/jpeg", "Извлеки поставщика");
КонецПроцедуры`
	result := runHTTPSrc(t, src, interpreter.NewLLMFunctions(ai))
	assert.Equal(t, `{"поставщик":"ООО Ромашка"}`, result)
	assert.Equal(t, "документы", ai.last.Task)
	assert.Equal(t, "QUJD", ai.last.ImageB64)
	assert.Equal(t, "image/jpeg", ai.last.MimeType)
	assert.Equal(t, "Извлеки поставщика", ai.last.Prompt)
}

func TestИИНеНастроен(t *testing.T) {
	src := `Процедура Тест()
  Попытка
    ЗапросИИ("привет");
    Возврат "no error";
  Исключение
    Возврат "caught: " + ОписаниеОшибки();
  КонецПопытки;
КонецПроцедуры`
	result := runHTTPSrc(t, src, interpreter.NewLLMFunctions(&stubAI{configured: false}))
	msg, ok := result.(string)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(msg, "caught:"))
	assert.Contains(t, msg, "не настроен")
}
