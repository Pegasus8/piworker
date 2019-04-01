package log

import (
	"log"
	"io"
)

var (
	// Info logger
	Info *log.Logger
	// Warning logger
	Warning *log.Logger
	// Error logger
	Error *log.Logger
	// Critical logger
	Critical *log.Logger
	// Fatal logger
	Fatal *log.Logger
)

// Init is a function to initialize the logger.
func Init(infoHandle io.Writer, warningHandle io.Writer, errorHandle io.Writer,
	criticalHandle io.Writer, fatalHandle io.Writer) {

	flags := log.Ldate | log.Ltime
	
	Info = log.New(infoHandle, "| INFO | ", flags)
	Warning = log.New(warningHandle, "| WARNING | ", flags)
	Error = log.New(errorHandle, "| ERROR | ", flags)
	Critical = log.New(criticalHandle, "| CRITICAL | ", flags)
	Fatal = log.New(fatalHandle, "| FATAL | ", flags)
}

// Infoln ...
func Infoln(v ...interface{}) {
	Info.Println(v...)
}

// Infof ...
func Infof(format string, v ...interface{}) {
	Info.Printf(format, v...)
}

// Warningln ...
func Warningln(v ...interface{}) {
	Warning.Println(v...)
}

// Warningf ...
func Warningf(format string, v ...interface{}) {
	Warning.Printf(format, v...)
}

// Errorln ...
func Errorln(v ...interface{}) {
	Error.Println(v...)
}

// Errorf ...
func Errorf(format string, v ...interface{}) {
	Error.Printf(format, v...)
}

// Criticalln ...
func Criticalln(v ...interface{}) {
	Critical.Println(v...)
}

// Criticalf ...
func Criticalf(format string, v ...interface{}) {
	Critical.Printf(format, v...)
}

// Fatalln ...
func Fatalln(v ...interface{}) {
	Fatal.Fatalln(v...)
}

// Fatalf ...
func Fatalf(format string, v...interface{}) {
	Fatal.Fatalf(format, v...)
}
