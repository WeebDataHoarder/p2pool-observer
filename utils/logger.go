package utils

import "log"

type LogLevel int

const (
	LogLevelError = LogLevel(1 << iota)
	LogLevelInfo
	LogLevelNotice
	LogLevelDebug
)

var GlobalLogLevel = LogLevelError | LogLevelInfo

func Errorf(format string, v ...any) {
	if GlobalLogLevel&LogLevelError == 0 {
		return
	}
	log.Printf(format, v...)
}

func Logf(format string, v ...any) {
	if GlobalLogLevel&LogLevelInfo == 0 {
		return
	}
	log.Printf(format, v...)
}

func Noticef(format string, v ...any) {
	if GlobalLogLevel&LogLevelNotice == 0 {
		return
	}
	log.Printf(format, v...)
}

func Debugf(format string, v ...any) {
	if GlobalLogLevel&LogLevelDebug == 0 {
		return
	}
	log.Printf(format, v...)
}
