package logger

type Logger interface {
	Debug(message string)
	Debugf(format string, a ...any)

	Info(message string)
	Infof(format string, args ...any)

	Warn(message string)
	Warnf(format string, args ...any)

	Error(message string)
	Errorf(format string, args ...any)

	Success(msg string)
	Successf(format string, args ...any)
}
