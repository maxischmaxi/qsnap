package logging

import (
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

type Config struct {
	Level       string // "debug","info","warn","error"
	FilePath    string // e.g. "out/snapper.log"
	MaxSizeMB   int    // per file
	MaxBackups  int
	MaxAgeDays  int
	JSON        bool // console encoder if false
	Console     bool // also log to stdout
	Development bool // zap development (stacktraces for warn+)
}

var L *zap.Logger
var S *zap.SugaredLogger

func parseLevel(s string) zapcore.Level {
	switch s {
	case "debug":
		return zap.DebugLevel
	case "info":
		return zap.InfoLevel
	case "warn", "warning":
		return zap.WarnLevel
	case "error":
		return zap.ErrorLevel
	default:
		return zap.InfoLevel
	}
}

func fileCore(path string, enc zapcore.Encoder, lvl zapcore.Level) zapcore.Core {
	w := zapcore.AddSync(&lumberjack.Logger{
		Filename:   path,
		MaxSize:    1, // default if not overridden
		MaxBackups: 5,
		MaxAge:     14,
		Compress:   true,
	})
	return zapcore.NewCore(enc, w, lvl)
}

func Init(cfg Config) (func(), error) {
	level := parseLevel(cfg.Level)

	// encoders
	jsonEncCfg := zap.NewProductionEncoderConfig()
	jsonEncCfg.TimeKey = "ts"
	jsonEncCfg.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.UTC().Format(time.RFC3339))
	}
	jsonEncCfg.EncodeLevel = zapcore.LowercaseLevelEncoder
	jsonEnc := zapcore.NewJSONEncoder(jsonEncCfg)

	consoleEncCfg := zap.NewDevelopmentEncoderConfig()
	consoleEncCfg.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("15:04:05"))
	}
	consoleEnc := zapcore.NewConsoleEncoder(consoleEncCfg)

	var cores []zapcore.Core

	// file core
	if cfg.FilePath != "" {
		enc := jsonEnc
		if !cfg.JSON {
			enc = consoleEnc
		}
		cores = append(cores, fileCore(cfg.FilePath, enc, level))
	}

	// console core
	if cfg.Console {
		w := zapcore.AddSync(os.Stdout)
		enc := consoleEnc
		if cfg.JSON {
			enc = jsonEnc
		}
		cores = append(cores, zapcore.NewCore(enc, w, level))
	}

	core := zapcore.NewTee(cores...)
	var opts []zap.Option
	if cfg.Development {
		opts = append(opts, zap.Development(), zap.AddStacktrace(zap.WarnLevel))
	} else {
		opts = append(opts, zap.AddStacktrace(zap.ErrorLevel))
	}
	opts = append(opts, zap.AddCaller())

	L = zap.New(core, opts...)
	S = L.Sugar()

	// replace global for 3rd-party helpers if you want: zap.ReplaceGlobals(L)
	cleanup := func() {
		_ = L.Sync()
	}
	return cleanup, nil
}

func With(fields ...zap.Field) *zap.Logger {
	if L == nil {
		panic("logging not initialized")
	}
	return L.With(fields...)
}
