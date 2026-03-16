package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// RotatingWriter is an io.Writer that automatically rotates log files
// when they exceed a configured maximum size. Old log files are kept
// up to a configurable count.
//
// Usage:
//
//	w, _ := logger.NewRotatingWriter(logger.RotationConfig{
//	    Path:       "storage/logs/app.log",
//	    MaxSizeMB:  50,
//	    MaxBackups: 7,
//	})
//	// Use w as a zap WriteSyncer or io.Writer.
type RotatingWriter struct {
	mu          sync.Mutex
	cfg         RotationConfig
	file        *os.File
	currentSize int64
}

// RotationConfig configures log file rotation.
type RotationConfig struct {
	// Path is the log file path (e.g. "storage/logs/app.log").
	Path string

	// MaxSizeMB is the maximum file size in megabytes before rotation (default: 100).
	MaxSizeMB int

	// MaxBackups is the number of rotated files to keep (default: 5).
	MaxBackups int
}

// NewRotatingWriter creates a new rotating log file writer.
func NewRotatingWriter(cfg RotationConfig) (*RotatingWriter, error) {
	if cfg.MaxSizeMB <= 0 {
		cfg.MaxSizeMB = 100
	}
	if cfg.MaxBackups <= 0 {
		cfg.MaxBackups = 5
	}

	dir := filepath.Dir(cfg.Path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("logger: create dir %s: %w", dir, err)
		}
	}

	w := &RotatingWriter{cfg: cfg}
	if err := w.openFile(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *RotatingWriter) openFile() error {
	f, err := os.OpenFile(w.cfg.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("logger: open %s: %w", w.cfg.Path, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("logger: stat %s: %w", w.cfg.Path, err)
	}
	w.file = f
	w.currentSize = info.Size()
	return nil
}

// Write implements io.Writer. Rotates the file if the write would exceed MaxSizeMB.
func (w *RotatingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	maxBytes := int64(w.cfg.MaxSizeMB) * 1024 * 1024
	if w.currentSize+int64(len(p)) > maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err = w.file.Write(p)
	w.currentSize += int64(n)
	return n, err
}

// Sync flushes the file to disk (implements zapcore.WriteSyncer).
func (w *RotatingWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Sync()
	}
	return nil
}

// Close closes the underlying file.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

func (w *RotatingWriter) rotate() error {
	// Close current file.
	if w.file != nil {
		w.file.Close()
	}

	// Rename current → timestamped backup.
	timestamp := time.Now().Format("2006-01-02T15-04-05")
	ext := filepath.Ext(w.cfg.Path)
	base := w.cfg.Path[:len(w.cfg.Path)-len(ext)]
	backupPath := fmt.Sprintf("%s-%s%s", base, timestamp, ext)

	if err := os.Rename(w.cfg.Path, backupPath); err != nil {
		return fmt.Errorf("logger: rotate rename: %w", err)
	}

	// Clean up old backups.
	w.cleanOldBackups()

	// Open new file.
	return w.openFile()
}

func (w *RotatingWriter) cleanOldBackups() {
	dir := filepath.Dir(w.cfg.Path)
	base := filepath.Base(w.cfg.Path)
	ext := filepath.Ext(base)
	prefix := base[:len(base)-len(ext)]

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var backups []string
	for _, e := range entries {
		name := e.Name()
		if name == base {
			continue
		}
		if len(name) > len(prefix) && name[:len(prefix)] == prefix && filepath.Ext(name) == ext {
			backups = append(backups, filepath.Join(dir, name))
		}
	}

	if len(backups) <= w.cfg.MaxBackups {
		return
	}

	// Sort oldest first, remove excess.
	sort.Strings(backups)
	toRemove := backups[:len(backups)-w.cfg.MaxBackups]
	for _, f := range toRemove {
		os.Remove(f)
	}
}
