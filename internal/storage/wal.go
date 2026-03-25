package storage

import (
	"encoding/json"
	"os"
	"sync"
)

type WAL struct {
	mu    sync.Mutex
	file  *os.File
	path  string
	count int
}

func NewWAL(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &WAL{file: f, path: path}, nil
}

func (w *WAL) Write(v interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.count++
	if w.count > 10 {
		w.Rotate()
	}

	data, _ := json.Marshal(v)
	_, err := w.file.Write(append(data, '\n'))
	return err
}

func (w *WAL) Rotate() {
	w.file.Close()
	// Simulate pruning: Rename current log to .old and start fresh
	os.Rename(w.path, w.path+".old")
	f, _ := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	w.file = f
	w.count = 0
	// Write Checkpoint Marker
	w.file.Write([]byte("{\"CHECKPOINT\":\"STATE_SAVED\"}\n"))
}
