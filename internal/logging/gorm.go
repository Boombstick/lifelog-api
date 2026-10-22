package logging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type GormLogger struct {
	logger *slog.Logger

	slowQuery time.Duration
}

func NewGormLogger(logger *slog.Logger, slowQuery time.Duration) gormlogger.Interface {
	if slowQuery <= 0 {
		slowQuery = 200 * time.Millisecond
	}
	return &GormLogger{logger: logger, slowQuery: slowQuery}
}

func (l *GormLogger) log() *slog.Logger {
	if l.logger != nil {
		return l.logger
	}
	return slog.Default()
}

func (l *GormLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }

func (l *GormLogger) Info(ctx context.Context, msg string, data ...any) {
	l.log().InfoContext(ctx, fmt.Sprintf(msg, data...), "source", "gorm")
}

func (l *GormLogger) Warn(ctx context.Context, msg string, data ...any) {
	l.log().WarnContext(ctx, fmt.Sprintf(msg, data...), "source", "gorm")
}

func (l *GormLogger) Error(ctx context.Context, msg string, data ...any) {
	l.log().ErrorContext(ctx, fmt.Sprintf(msg, data...), "source", "gorm")
}

func (l *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)

	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):

		sql, rows := traced(fc)
		l.log().ErrorContext(ctx, "запрос к базе не выполнен",
			"source", "gorm", "error", err.Error(),
			"sql", sql, "rows", rows, "duration_ms", millis(elapsed))

	case elapsed > l.slowQuery:
		sql, rows := traced(fc)
		l.log().WarnContext(ctx, "медленный запрос к базе",
			"source", "gorm", "sql", sql, "rows", rows, "duration_ms", millis(elapsed))
	}
}

func millis(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

func (l *GormLogger) ParamsFilter(_ context.Context, sql string, _ ...any) (string, []any) {
	return sql, nil
}

var explainedPlaceholder = regexp.MustCompile(`\$(\d+)\$`)

func traced(fc func() (string, int64)) (string, int64) {
	sql, rows := fc()
	return explainedPlaceholder.ReplaceAllString(sql, "$$$1"), rows
}
