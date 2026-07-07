package launcher

// Изолированные окна Предприятия (план 78, фаза 3): «Новое окно» открывает
// внешний Chromium-браузер (Edge/Chrome/Chromium) с отдельным user-data-dir.
// У каждого окна свой cookie-jar, поэтому в одной базе можно одновременно
// работать под разными пользователями — обычные окна на одном origin
// перетирают друг другу cookie onebase_session.
//
// Модель «рабочих мест»: клик открывает младший свободный (не запущенный)
// профиль, если все заняты — создаёт следующий. Профиль помнит вход (cookie
// персистентна): повторное открытие «Окна N» вернёт пользователя без логина.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ivantit66/onebase/internal/i18n/i18nerr"
)

const maxIsolatedProfiles = 50

// isolatedBrowser абстрагирует запуск внешнего браузера; в тестах подменяется
// фейком, чтобы не открывать реальные окна.
type isolatedBrowser interface {
	// Open запускает окно браузера с профилем profileDir на url.
	Open(profileDir, url string) error
}

// systemBrowser — боевая реализация: находит установленный Chromium-браузер
// (платформенный isolatedBrowserCommand) и запускает его отсоединённо.
//
// Нативные WebView2-окна вместо внешнего браузера (план 78, п. 4.2) остаются
// отдельной подзадачей: кандидат с переменной окружения
// WEBVIEW2_USER_DATA_FOLDER проверен и НЕ работает — vendored webview.h
// (webview_go) сам вычисляет userDataFolder как %APPDATA%\<имя exe> и передаёт
// его явным параметром CreateCoreWebView2EnvironmentWithOptions, который
// сильнее переменной. Нужен патч/форк биндинга.
type systemBrowser struct{}

func (systemBrowser) Open(profileDir, url string) error {
	cmd, err := isolatedBrowserCommand(profileDir, url)
	if err != nil {
		return err
	}
	return cmd.Start()
}

// chromiumArgs — аргументы изолированного окна. --app даёт окно без адресной
// строки (визуально ближе к нативному окну Предприятия).
func chromiumArgs(profileDir, url string) []string {
	return []string{
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--new-window",
		"--app=" + url,
	}
}

// profilesRootOverride — для тестов: подменяет корень каталога профилей,
// чтобы не трогать реальный ~/.onebase.
var profilesRootOverride string

// profilesRoot возвращает каталог изолированных профилей базы:
// ~/.onebase/browser-profiles/<base-id>/.
func profilesRoot(baseID string) (string, error) {
	if profilesRootOverride != "" {
		return filepath.Join(profilesRootOverride, baseID), nil
	}
	return OnebasePath("browser-profiles", baseID)
}

// pickProfileDir выбирает каталог профиля: младший свободный или новый.
// Занятость определяет profileInUse (платформенная проверка lock-файла
// Chromium) — PID запущенного процесса ненадёжен: браузер с уже открытым
// профилем делегирует существующему процессу и сразу выходит.
func pickProfileDir(root string) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	for i := 1; i <= maxIsolatedProfiles; i++ {
		dir := filepath.Join(root, fmt.Sprintf("profile-%d", i))
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", err
			}
			return dir, nil
		}
		if !profileInUse(dir) {
			return dir, nil
		}
	}
	return "", i18nerr.Errorf("слишком много изолированных профилей (%d) — выполните «Очистить изолированные профили»", maxIsolatedProfiles)
}

// cleanIsolatedProfiles удаляет свободные профили в root; занятые пропускает
// (браузер держит файлы профиля открытыми — удалять живой профиль нельзя, и
// ОС этого и не даст). Возвращает число удалённых профилей.
func cleanIsolatedProfiles(root string) (removed int, err error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if profileInUse(dir) {
			continue
		}
		if err := os.RemoveAll(dir); err == nil {
			removed++
		}
	}
	return removed, nil
}
