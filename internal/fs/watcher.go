package fs

import (
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

func (w *Watcher) Events() chan fsnotify.Event {
	return w.watcher.Events
}

func (w *Watcher) Errors() chan error {
	return w.watcher.Errors
}

func (w *Watcher) Close() error {
	return w.watcher.Close()
}
