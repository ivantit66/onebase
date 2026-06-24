package launcher

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivantit66/onebase/internal/llm"
	"github.com/ivantit66/onebase/internal/storage"
)

func TestLogCfgAI_RespectsFlag(t *testing.T) {
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// флаг выключен — ничего не пишем
	logCfgAI(ctx, db, llm.Config{LogHistory: false}, "admin",
		"конфигуратор-генерация", "ТЗ", "ответ", llm.ChatResponse{Model: "m"})
	if e, _ := db.ListAIAudit(ctx, 10); len(e) != 0 {
		t.Fatalf("при выключенном флаге запись не должна создаваться, есть %d", len(e))
	}

	// флаг включён — пишем запрос и ответ
	logCfgAI(ctx, db, llm.Config{LogHistory: true}, "admin",
		"конфигуратор-генерация", "ТЗ", "ответ",
		llm.ChatResponse{Model: "glm-4.6", InputTokens: 5, OutputTokens: 7})
	e, _ := db.ListAIAudit(ctx, 10)
	if len(e) != 1 || e[0].Response != "ответ" || e[0].Task != "конфигуратор-генерация" || e[0].OutputTokens != 7 {
		t.Fatalf("запись журнала неверна: %+v", e)
	}
}

func TestRenderAIHistory(t *testing.T) {
	if out := renderAIHistory(nil); !strings.Contains(out, "Журнал пуст") {
		t.Error("пустой журнал должен подсказывать про включение записи")
	}
	out := renderAIHistory([]storage.AIAuditEntry{{
		Task: "конфигуратор-генерация", Model: "glm", Query: "<b>ТЗ</b>",
		Response: "готово", InputTokens: 5, OutputTokens: 6, At: time.Now(),
	}})
	if !strings.Contains(out, "конфигуратор-генерация") || !strings.Contains(out, "готово") {
		t.Error("запись журнала не отрендерена")
	}
	if strings.Contains(out, "<b>ТЗ</b>") {
		t.Error("HTML в запросе должен экранироваться")
	}
}

func TestRenderAIHistory_StructuredToolTrace(t *testing.T) {
	response := "готово <script>\n\nTool trace:\n- создать_файл [ok]: записан file\n- check [error]: bad <x>\n\nОбъекты:\n- новый: documents/заказ.yaml"
	out := renderAIHistory([]storage.AIAuditEntry{{
		Task: "конфигуратор-генерация", Model: "glm", Query: "ТЗ",
		Response: response, At: time.Now(),
	}})
	for _, want := range []string{"Tool trace", "создать_файл", "check", "documents/заказ.yaml"} {
		if !strings.Contains(out, want) {
			t.Fatalf("structured history output does not contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<script>") || strings.Contains(out, "bad <x>") {
		t.Fatalf("structured history output must escape HTML:\n%s", out)
	}
}

func TestParseAIHistoryResponse(t *testing.T) {
	parts := parseAIHistoryResponse("текст\n\nTool trace:\n- a [ok]: one\n- b [error]: two\n\nОбъекты:\n- новый: x.yaml")
	if parts.Text != "текст" {
		t.Fatalf("text: %q", parts.Text)
	}
	if len(parts.Trace) != 2 || parts.Trace[1].Name != "b" || parts.Trace[1].Status != "error" || parts.Trace[1].Result != "two" {
		t.Fatalf("trace parsed incorrectly: %+v", parts.Trace)
	}
	if len(parts.Objects) != 1 || parts.Objects[0] != "новый: x.yaml" {
		t.Fatalf("objects parsed incorrectly: %+v", parts.Objects)
	}
}

func TestGenResponseSummary_IncludesToolTraceAndChanges(t *testing.T) {
	out := genResponseSummary("готово", []GenChange{{
		Path: "documents/заказ.yaml",
		Kind: "новый",
	}}, []GenToolTrace{
		{Name: "создать_файл", Result: "записан файл documents/заказ.yaml"},
		{Name: "проверить_конфигурацию", Result: "Нет ошибок."},
	})
	for _, want := range []string{
		"готово",
		"Tool trace:",
		"создать_файл [ok]",
		"проверить_конфигурацию [ok]",
		"новый: documents/заказ.yaml",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("summary does not contain %q:\n%s", want, out)
		}
	}
}
