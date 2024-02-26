package utils

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

type LogLevel int

var LogFile bool
var LogFunc bool

const (
	LogLevelError = LogLevel(1 << iota)
	LogLevelInfo
	LogLevelNotice
	LogLevelDebug
)

var GlobalLogLevel = LogLevelError | LogLevelInfo

func Panic(v ...any) {
	s := fmt.Sprint(v...)
	innerPrint("", "PANIC", s)
	panic(s)
}

func Panicf(format string, v ...any) {
	s := fmt.Sprintf(format, v...)
	innerPrint("", "PANIC", s)
	panic(s)
}

func Fatalf(format string, v ...any) {
	innerPrint("", "FATAL", fmt.Sprintf(format, v...))
	os.Exit(1)
}

func Error(v ...any) {
	if GlobalLogLevel&LogLevelError == 0 {
		return
	}
	innerPrint("", "ERROR", fmt.Sprint(v...))
}

func Errorf(prefix, format string, v ...any) {
	if GlobalLogLevel&LogLevelError == 0 {
		return
	}
	innerPrint(prefix, "ERROR", fmt.Sprintf(format, v...))
}

func Print(v ...any) {
	if GlobalLogLevel&LogLevelInfo == 0 {
		return
	}
	innerPrint("", "INFO", fmt.Sprint(v...))
}

func Logf(prefix, format string, v ...any) {
	if GlobalLogLevel&LogLevelInfo == 0 {
		return
	}
	innerPrint(prefix, "INFO", fmt.Sprintf(format, v...))
}

func Noticef(format string, v ...any) {
	if GlobalLogLevel&LogLevelNotice == 0 {
		return
	}
	innerPrint("", "NOTICE", fmt.Sprintf(format, v...))
}

func Debugf(format string, v ...any) {
	if GlobalLogLevel&LogLevelDebug == 0 {
		return
	}
	innerPrint("", "DEBUG", fmt.Sprintf(format, v...))
}

func innerPrint(prefix, class, v string) {
	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05.000")
	if LogFile {
		var function string
		pc, file, line, ok := runtime.Caller(2)
		if !ok {
			file = "???"
			line = 0
			pc = 0
		}
		short := file
		for i := len(file) - 1; i > 0; i-- {
			if file[i] == '/' {
				short = file[i+1:]
				break
			}
		}

		if LogFunc {
			if pc != 0 {
				if details := runtime.FuncForPC(pc); details != nil {
					function = details.Name()
				}
			}
			shortFunc := function
			for i := len(function) - 1; i > 0; i-- {
				if function[i] == '/' {
					shortFunc = function[i+1:]
					break
				}
			}
			funcItems := strings.Split(shortFunc, ".")
			fmt.Printf("%s %s:%d:%s [%s] %s %s\n", timestamp, short, line, funcItems[len(funcItems)-1], prefix, class, strings.TrimSpace(v))
		} else {
			fmt.Printf("%s %s:%d [%s] %s %s\n", timestamp, short, line, prefix, class, strings.TrimSpace(v))
		}
	} else {
		fmt.Printf("%s [%s] %s %s\n", timestamp, prefix, class, strings.TrimSpace(v))
	}
}
