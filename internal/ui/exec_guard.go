package ui

// Переключатель команд ОС (план 67): единая проверка перед запуском процесса из
// DSL (ВыполнитьКоманду). Флаг exec.enabled читается из _settings, поэтому
// переключение в конфигураторе действует без перезапуска сервера. Зеркалит
// предохранитель сети (план 62, network_guard.go).

import (
	"context"
	"errors"
)

// ErrExecLocked — текст отказа, видимый пользователю (DSL-ошибка, ловится
// Попыткой). Прямо подсказывает, где включить.
var ErrExecLocked = errors.New("выполнение команд ОС отключено — включите «Разрешить выполнение команд ОС» в конфигураторе (Система → Настройки)")

// execEnabled сообщает, разрешён ли запуск команд ОС для текущей базы.
func (s *Server) execEnabled(ctx context.Context) bool {
	return s.store.GetExecEnabled(ctx)
}

// execGuard возвращает замыкание-страж для DSL (ВыполнитьКоманду): nil-ошибка
// при разрешённых командах, ErrExecLocked при запрете.
func (s *Server) execGuard(ctx context.Context) func() error {
	return func() error {
		if s.execEnabled(ctx) {
			return nil
		}
		return ErrExecLocked
	}
}
