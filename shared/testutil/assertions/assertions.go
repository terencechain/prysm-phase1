package assertions

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

// AssertionTestingTB exposes enough testing.TB methods for assertions.
type AssertionTestingTB interface {
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
}

type assertionLoggerFn func(string, ...interface{})

// Equal compares values using comparison operator.
func Equal(loggerFn assertionLoggerFn, expected, actual interface{}, msg ...interface{}) {
	errMsg := parseMsg("Values are not equal", msg...)
	if expected != actual {
		_, file, line, _ := runtime.Caller(2)
		loggerFn("%s:%d %s, want: %[4]v (%[4]T), got: %[5]v (%[5]T)", filepath.Base(file), line, errMsg, expected, actual)
	}
}

// NotEqual compares values using comparison operator.
func NotEqual(loggerFn assertionLoggerFn, expected, actual interface{}, msg ...interface{}) {
	errMsg := parseMsg("Values are equal", msg...)
	if expected == actual {
		_, file, line, _ := runtime.Caller(2)
		loggerFn("%s:%d %s, both values are equal: %[4]v (%[4]T)", filepath.Base(file), line, errMsg, expected)
	}
}

// DeepEqual compares values using DeepEqual.
func DeepEqual(loggerFn assertionLoggerFn, expected, actual interface{}, msg ...interface{}) {
	errMsg := parseMsg("Values are not equal", msg...)
	if !reflect.DeepEqual(expected, actual) {
		_, file, line, _ := runtime.Caller(2)
		loggerFn("%s:%d %s, want: %v, got: %v", filepath.Base(file), line, errMsg, expected, actual)
	}
}

// NoError asserts that error is nil.
func NoError(loggerFn assertionLoggerFn, err error, msg ...interface{}) {
	errMsg := parseMsg("Unexpected error", msg...)
	if err != nil {
		_, file, line, _ := runtime.Caller(2)
		loggerFn("%s:%d %s: %v", filepath.Base(file), line, errMsg, err)
	}
}

// ErrorContains asserts that actual error contains wanted message.
func ErrorContains(loggerFn assertionLoggerFn, want string, err error, msg ...interface{}) {
	errMsg := parseMsg("Expected error not returned", msg...)
	if err == nil || !strings.Contains(err.Error(), want) {
		_, file, line, _ := runtime.Caller(2)
		loggerFn("%s:%d %s, got: %v, want: %s", filepath.Base(file), line, errMsg, err, want)
	}
}

// NotNil asserts that passed value is not nil.
func NotNil(loggerFn assertionLoggerFn, obj interface{}, msg ...interface{}) {
	errMsg := parseMsg("Unexpected nil value", msg...)
	if obj == nil {
		_, file, line, _ := runtime.Caller(2)
		loggerFn("%s:%d %s", filepath.Base(file), line, errMsg)
	}
}

func parseMsg(defaultMsg string, msg ...interface{}) string {
	if len(msg) >= 1 {
		msgFormat, ok := msg[0].(string)
		if !ok {
			return defaultMsg
		}
		return fmt.Sprintf(msgFormat, msg[1:]...)
	}
	return defaultMsg
}

// TBMock exposes enough testing.TB methods for assertions.
type TBMock struct {
	ErrorfMsg string
	FatalfMsg string
}

// Errorf writes testing logs to ErrorfMsg.
func (tb *TBMock) Errorf(format string, args ...interface{}) {
	tb.ErrorfMsg = fmt.Sprintf(format, args...)
}

// Fatalf writes testing logs to FatalfMsg.
func (tb *TBMock) Fatalf(format string, args ...interface{}) {
	tb.FatalfMsg = fmt.Sprintf(format, args...)
}
