package logger

import (
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var teeOnce sync.Once

// TeeCore wraps the global logger core with an additional core (e.g. Telescope).
// Safe to call once; subsequent calls are ignored.
func TeeCore(extra zapcore.Core) error {
	if extra == nil || Log == nil {
		return nil
	}
	var err error
	teeOnce.Do(func() {
		base := Log.Desugar()
		merged := zapcore.NewTee(base.Core(), extra)
		Set(zap.New(merged))
	})
	return err
}
