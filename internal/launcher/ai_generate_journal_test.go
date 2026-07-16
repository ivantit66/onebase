package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestKindSubdir_Journal — генератор каркаса должен знать тип «журнал» и класть
// его в journals/. Раньше журнала не было ни в kindSubdir, ни в белом списке,
// поэтому модель сваливала журнальный YAML в pages/ и widgets/ (битый каркас).
func TestKindSubdir_Journal(t *testing.T) {
	for _, kind := range []string{"журнал", "Журнал", "  ЖУРНАЛ  ", "journal", "журнал документов"} {
		sub, ok := kindSubdir(kind)
		if !ok || sub != "journals" {
			t.Errorf("kindSubdir(%q) = (%q, %v), хотим (journals, true)", kind, sub, ok)
		}
	}
}

// TestApplyableSubdirs_Journal — путь journals/*.yaml должен проходить проверку
// применения (иначе apply отклонит сгенерированный журнал).
func TestApplyableSubdirs_Journal(t *testing.T) {
	if !applyableSubdirs["journals"] {
		t.Fatal("journals не в applyableSubdirs — cfgAIApply отклонит журнал")
	}
	if _, err := safeGeneratedRelPath("journals/расписаниедокладов.yaml"); err != nil {
		t.Errorf("safeGeneratedRelPath(journals/…) = %v, хотим nil", err)
	}
}

// TestGenSession_CreateJournalObject — «создать_объект» с типом «журнал» пишет
// файл именно в journals/ и помечает его изменённым.
func TestGenSession_CreateJournalObject(t *testing.T) {
	tmp := t.TempDir()
	g := &genSession{overlay: tmp, changed: map[string]bool{}}
	const y = `name: РасписаниеДокладов
title: Расписание докладов
documents: [Доклад]
columns:
  - {field: Дата, label: Дата, format: date}
`
	if err := g.createObject("Журнал", "РасписаниеДокладов", y); err != nil {
		t.Fatalf("createObject(журнал): %v", err)
	}
	const rel = "journals/расписаниедокладов.yaml"
	if !g.changed[rel] {
		t.Errorf("changed не содержит %q: %v", rel, g.changed)
	}
	if _, err := os.Stat(filepath.Join(tmp, "journals", "расписаниедокладов.yaml")); err != nil {
		t.Errorf("файл журнала не создан в journals/: %v", err)
	}
}

// TestJournalFormatGuide_InSystemPrompt — схема журнала (field/label, отсутствие
// type/tableparts) должна попадать в системный промпт генератора.
func TestJournalFormatGuide_InSystemPrompt(t *testing.T) {
	for _, sub := range []string{"journals/<Имя>.yaml", "ключом field", "НЕТ ключа type"} {
		if !strings.Contains(aiGenerateSystem, sub) {
			t.Errorf("в aiGenerateSystem нет %q — модель снова будет угадывать схему журнала", sub)
		}
	}
}

// TestSubsystemFormatGuide_InSystemPrompt — схема подсистемы (contents/home_page
// + предупреждение «все имена должны существовать») должна быть в промпте, чтобы
// модель не угадывала ключи и создавала подсистему последней.
func TestSubsystemFormatGuide_InSystemPrompt(t *testing.T) {
	for _, sub := range []string{"subsystems/<Имя>.yaml", "contents:", "home_page:", "создавай ПОСЛЕДНЕЙ"} {
		if !strings.Contains(aiGenerateSystem, sub) {
			t.Errorf("в aiGenerateSystem нет %q — модель снова будет угадывать схему подсистемы", sub)
		}
	}
}
