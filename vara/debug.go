package vara

import (
	"fmt"
	"log"
	"os"
	"strconv"
)

var debugLogger = func() *log.Logger {
	if t, _ := strconv.ParseBool(os.Getenv("VARA_DEBUG")); !t {
		return nil
	}
	return log.New(os.Stderr, "[VARA] ", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)
}()

func debugPrint(format string, args ...interface{}) {
	if debugLogger == nil {
		return
	}
	debugLogger.Output(2, fmt.Sprintf(format, args...))
}

// debugPrint3 is like debugPrint but with increased call depth for use
// with writeCmd so we're able to trace the origin of the write call.
func debugPrint3(format string, args ...interface{}) {
	if debugLogger == nil {
		return
	}
	debugLogger.Output(3, fmt.Sprintf(format, args...))
}
