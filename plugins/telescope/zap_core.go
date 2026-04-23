package telescope

import (
	"go.uber.org/zap/zapcore"
)

type telescopeCore struct {
	zapcore.LevelEnabler
}

func zapInfoLevel() zapcore.LevelEnabler {
	return zapcore.LevelOf(zapcore.InfoLevel)
}

// NewTelescopeZapCore returns a zap core that mirrors log lines into Telescope.
func NewTelescopeZapCore(enab zapcore.LevelEnabler) zapcore.Core {
	if enab == nil {
		enab = zapInfoLevel()
	}
	return &telescopeCore{LevelEnabler: enab}
}

func (c *telescopeCore) With([]zapcore.Field) zapcore.Core { return c }

func (c *telescopeCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *telescopeCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	ctx := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f.Type {
		case zapcore.StringType:
			ctx[f.Key] = f.String
		case zapcore.Int64Type:
			ctx[f.Key] = f.Integer
		default:
			if f.Interface != nil {
				ctx[f.Key] = f.Interface
			} else {
				ctx[f.Key] = f.String
			}
		}
	}
	RecordLog(ent.Level.String(), ent.Message, ctx)
	return nil
}

func (c *telescopeCore) Sync() error { return nil }

var _ zapcore.Core = (*telescopeCore)(nil)
