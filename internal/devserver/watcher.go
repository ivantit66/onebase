package devserver

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	oblog "github.com/ivantit66/onebase/internal/logging"
)

func watcherLog() *slog.Logger {
	return oblog.Component("devserver.watcher")
}

// Watch watches dir and all its subdirectories, calling onChange after a
// debounce period. fsnotify is not recursive, so every subdirectory is added
// explicitly; directories created later are picked up on the fly.
func Watch(dir string, onChange func()) error {
	_, err := WatchContext(context.Background(), dir, onChange)
	return err
}

// WatchContext is Watch with explicit lifecycle control. done is closed after
// the watcher goroutine has stopped, so callers can safely release resources
// referenced by onChange.
func WatchContext(ctx context.Context, dir string, onChange func()) (<-chan struct{}, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// addTree рекурсивно добавляет root и все его подкаталоги в наблюдение.
	addTree := func(root string) {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr == nil && d.IsDir() {
				_ = w.Add(path)
			}
			return nil
		})
	}
	addTree(dir)

	debounce := time.NewTimer(0)
	<-debounce.C // drain initial tick

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer w.Close()
		defer debounce.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-w.Events:
				if !ok {
					return
				}
				// Новый подкаталог — начинаем следить и за ним, иначе
				// файлы внутри него не отслеживались бы.
				if event.Has(fsnotify.Create) {
					if fi, statErr := os.Stat(event.Name); statErr == nil && fi.IsDir() {
						addTree(event.Name)
					}
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
					debounce.Reset(300 * time.Millisecond)
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				watcherLog().Warn("watcher error", "err", err)
			case <-debounce.C:
				onChange()
			}
		}
	}()
	return done, nil
}
