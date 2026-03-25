package storage

import (
	"encoding/json"
	"os"
	"sync"
)

type WAL struct {
	mu   sync.Mutex
	file *os.File
}

func NewWAL(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &WAL{file: f}, nil
}

func (w *WAL) Write(v interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, _ := json.Marshal(v)
	_, err := w.file.Write(append(data, '\n'))
	return err
}
