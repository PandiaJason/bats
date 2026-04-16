package wal

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type WALEntry struct {
	Timestamp        int64             `json:"timestamp"`
	ActionHash       string            `json:"action_hash"`
	ProposingAgent   string            `json:"proposing_agent"`
	ValidationResult string            `json:"validation_result"`
	Annotations      map[string]string `json:"annotations,omitempty"`
	PrevHash         string            `json:"prev_hash"`
	EntryHash        string            `json:"entry_hash"`
}

type WAL struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	count    int
	lastHash string

	// RotateThreshold controls how many entries trigger an auto-rotation.
	RotateThreshold int
}

// NewWAL creates or opens a hash-chained Write-Ahead Log.
// The WAL file is opened in append mode with sync-on-write.
func NewWAL(path string) (*WAL, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create WAL directory %s: %w", dir, err)
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file %s: %w", path, err)
	}

	w := &WAL{
		file:            f,
		path:            path,
		lastHash:        "0000000000000000000000000000000000000000000000000000000000000000",
		RotateThreshold: 10000,
	}

	// Recover last hash by reading backwards from end of file.
	if err := w.recoverLastHash(); err != nil {
		// Non-fatal: start with genesis hash if recovery fails.
		fmt.Fprintf(os.Stderr, "[WAL] Warning: hash recovery failed (%v), starting with genesis hash\n", err)
	}

	return w, nil
}

// recoverLastHash reads the last line of the WAL file to recover the chain hash.
// Uses a backward scan to avoid reading the entire file into memory.
func (w *WAL) recoverLastHash() error {
	info, err := w.file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return nil // empty file, genesis hash is correct
	}

	// Read the last 4KB — enough for several entries
	readSize := int64(4096)
	if info.Size() < readSize {
		readSize = info.Size()
	}
	buf := make([]byte, readSize)
	_, err = w.file.ReadAt(buf, info.Size()-readSize)
	if err != nil && err != io.EOF {
		return err
	}

	// Find the last complete JSON line
	lines := strings.Split(string(buf), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var entry WALEntry
		if err := json.Unmarshal([]byte(line), &entry); err == nil && entry.EntryHash != "" {
			w.lastHash = entry.EntryHash
			return nil
		}
	}
	return nil
}

// Append writes a hash-chained entry to the log.
// The write is fsynced to disk before returning, guaranteeing durability.
func (w *WAL) Append(actionHash, agentID, result string, annotations map[string]string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	entry := WALEntry{
		Timestamp:        time.Now().UnixNano(),
		ActionHash:       actionHash,
		ProposingAgent:   agentID,
		ValidationResult: result,
		Annotations:      annotations,
		PrevHash:         w.lastHash,
	}

	// Cryptographic Hash Chain: SHA256(PrevHash + Timestamp + ActionHash + Result)
	raw := fmt.Sprintf("%s|%d|%s|%s", w.lastHash, entry.Timestamp, entry.ActionHash, entry.ValidationResult)
	hash := sha256.Sum256([]byte(raw))
	entry.EntryHash = hex.EncodeToString(hash[:])

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal WAL entry: %w", err)
	}

	// Write entry + newline
	if _, err := w.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write WAL entry: %w", err)
	}

	// fsync to guarantee durability — data is on disk before we return.
	// This is the critical difference between "audit trail" and "best-effort log".
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("failed to fsync WAL: %w", err)
	}

	w.lastHash = entry.EntryHash
	w.count++

	// Auto-rotate when threshold is reached
	if w.count > w.RotateThreshold {
		if err := w.rotateLocked(); err != nil {
			fmt.Fprintf(os.Stderr, "[WAL] Rotation failed: %v\n", err)
		}
	}
	return nil
}

// Write provides backward compatibility for generic writes.
func (w *WAL) Write(v interface{}) error {
	if s, ok := v.(string); ok {
		return w.Append(s, "system", "UNKNOWN", nil)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.Append(string(data), "system", "UNKNOWN", nil)
}

// rotateLocked archives the current WAL and starts a new one.
// Caller MUST hold w.mu.
func (w *WAL) rotateLocked() error {
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("pre-rotation sync failed: %w", err)
	}
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("failed to close WAL for rotation: %w", err)
	}

	archivePath := fmt.Sprintf("%s.%d", w.path, time.Now().UnixNano())
	if err := os.Rename(w.path, archivePath); err != nil {
		return fmt.Errorf("failed to archive WAL: %w", err)
	}

	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to create new WAL after rotation: %w", err)
	}
	w.file = f
	w.count = 0
	return nil
}

// ExportJSON writes the WAL history as a JSON array.
func (w *WAL) ExportJSON(writer io.Writer) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := os.Open(w.path)
	if err != nil {
		return fmt.Errorf("failed to open WAL for export: %w", err)
	}
	defer f.Close()

	var entries []WALEntry
	scanner := newLineScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e WALEntry
		if err := json.Unmarshal([]byte(line), &e); err == nil {
			entries = append(entries, e)
		}
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

// ExportCSV exports the WAL history as CSV for compliance tracking.
func (w *WAL) ExportCSV(writer io.Writer) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := os.Open(w.path)
	if err != nil {
		return fmt.Errorf("failed to open WAL for export: %w", err)
	}
	defer f.Close()

	csvWriter := csv.NewWriter(writer)
	if err := csvWriter.Write([]string{"Timestamp", "ActionHash", "Agent", "Result", "PrevHash", "EntryHash"}); err != nil {
		return err
	}

	scanner := newLineScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e WALEntry
		if err := json.Unmarshal([]byte(line), &e); err == nil {
			csvWriter.Write([]string{
				fmt.Sprintf("%d", e.Timestamp),
				e.ActionHash,
				e.ProposingAgent,
				e.ValidationResult,
				e.PrevHash,
				e.EntryHash,
			})
		}
	}
	csvWriter.Flush()
	return csvWriter.Error()
}

// Close cleanly shuts down the WAL, flushing all buffered data.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Sync(); err != nil {
		return err
	}
	return w.file.Close()
}

// newLineScanner creates a bufio.Scanner with a large enough buffer for WAL lines.
func newLineScanner(r io.Reader) *lineScanner {
	return &lineScanner{r: r, buf: make([]byte, 0, 64*1024)}
}

// lineScanner is a simple line-by-line reader that avoids reading the entire file.
type lineScanner struct {
	r    io.Reader
	buf  []byte
	line string
	err  error
}

func (s *lineScanner) Scan() bool {
	for {
		// Check if there's a newline in the buffer
		if idx := strings.IndexByte(string(s.buf), '\n'); idx >= 0 {
			s.line = string(s.buf[:idx])
			s.buf = s.buf[idx+1:]
			return true
		}

		// Read more data
		tmp := make([]byte, 4096)
		n, err := s.r.Read(tmp)
		if n > 0 {
			s.buf = append(s.buf, tmp[:n]...)
		}
		if err != nil {
			if len(s.buf) > 0 {
				s.line = string(s.buf)
				s.buf = nil
				return true
			}
			s.err = err
			return false
		}
	}
}

func (s *lineScanner) Text() string { return s.line }
