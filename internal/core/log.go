package core

import (
	"log"
)

var Logger *log.Logger

func Logf(format string, args ...any) {
	if Logger != nil {
		Logger.Printf(format, args...)
	}
}
