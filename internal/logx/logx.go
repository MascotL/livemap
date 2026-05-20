package logx

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const maxHistory = 1000

var (
	mu          sync.Mutex
	history     []string
	subscribers = map[chan string]struct{}{}
)

func Infof(format string, args ...any) {
	logf("INFO", format, args...)
}

func Warnf(format string, args ...any) {
	logf("WARN", format, args...)
}

func Errorf(format string, args ...any) {
	logf("ERROR", format, args...)
}

func Debugf(format string, args ...any) {
	logf("DEBUG", format, args...)
}

func Raw(line string) {
	mu.Lock()
	defer mu.Unlock()

	message := line
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}
	fmt.Fprint(os.Stderr, message)

	cleanLine := strings.TrimRight(line, "\n")
	history = append(history, cleanLine)
	if len(history) > maxHistory {
		history = history[len(history)-maxHistory:]
	}
	for ch := range subscribers {
		select {
		case ch <- cleanLine:
		default:
		}
	}
}

func History() []string {
	mu.Lock()
	defer mu.Unlock()

	copyHistory := make([]string, len(history))
	copy(copyHistory, history)
	return copyHistory
}

func Clear() {
	mu.Lock()
	history = nil
	mu.Unlock()
}

func Subscribe() (<-chan string, func()) {
	ch := make(chan string, 100)

	mu.Lock()
	subscribers[ch] = struct{}{}
	mu.Unlock()

	unsubscribe := func() {
		mu.Lock()
		if _, ok := subscribers[ch]; ok {
			delete(subscribers, ch)
			close(ch)
		}
		mu.Unlock()
	}
	return ch, unsubscribe
}

func logf(level, format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	message := fmt.Sprintf(format, args...)
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}
	line := fmt.Sprintf("%s [%s] [pid=%d] %s", timestamp, level, os.Getpid(), message)
	fmt.Fprint(os.Stderr, line)

	history = append(history, strings.TrimRight(line, "\n"))
	if len(history) > maxHistory {
		history = history[len(history)-maxHistory:]
	}
	for ch := range subscribers {
		select {
		case ch <- strings.TrimRight(line, "\n"):
		default:
		}
	}
}
