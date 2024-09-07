package serror

import (
	"fmt"
	"runtime"
)

type Serror struct {
	runtimeName string
	function    string
	line        int
	err         error
}

func (e Serror) Error() string {
	return fmt.Sprintf("%s[%s:%d] %v", e.runtimeName, e.function, e.line, e.err)
}

func (e Serror) Unwrap() error {
	return e.err
}

func (e Serror) Line() int {
	return e.line
}

func (e Serror) Function() string {
	return e.function
}

func (e Serror) RuntimeName() string {
	return e.runtimeName
}

func New(err error) error {
	pc, fn, line, _ := runtime.Caller(1)

	return Serror{
		err:         err,
		runtimeName: runtime.FuncForPC(pc).Name(),
		function:    fn,
		line:        line,
	}
}

func Wrap(err1, err2 error) error {
	pc, fn, line, _ := runtime.Caller(1)

	return Serror{
		err:         fmt.Errorf("%q: %q", err1, err2),
		runtimeName: runtime.FuncForPC(pc).Name(),
		function:    fn,
		line:        line,
	}
}

func Errorf(args ...any) error {
	pc, fn, line, _ := runtime.Caller(1)

	format, ok := args[0].(string)
	if !ok {
		return Serror{
			err:         fmt.Errorf("Error making error[%q]", args),
			runtimeName: runtime.FuncForPC(pc).Name(),
			function:    fn,
			line:        line,
		}
	}
	args = args[1:]
	return Serror{
		err:         fmt.Errorf(format, args...),
		runtimeName: runtime.FuncForPC(pc).Name(),
		function:    fn,
		line:        line,
	}
}
