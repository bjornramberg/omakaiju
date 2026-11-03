package fs

import (
	"errors"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	watcher *fsnotify.Watcher
}

func NewWatcher() (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{watcher: w}, nil
}

func (w *Watcher) Watch(path string) error {
	return w.watcher.Add(path)
}

// Remove stops watching path. A path that is not watched is not an error, so a
// pane can stop following a directory that vanished without having to know
// whether its watch was still in place.
func (w *Watcher) Remove(path string) error {
	err := w.watcher.Remove(path)
	if errors.Is(err, fsnotify.ErrNonExistentWatch) {
		return nil
	}
	return err
}

func (w *Watcher) Events() chan fsnotify.Event {
	return w.watcher.Events
}

func (w *Watcher) Errors() chan error {
	return w.watcher.Errors
}

func (w *Watcher) Close() error {
	return w.watcher.Close()
}
