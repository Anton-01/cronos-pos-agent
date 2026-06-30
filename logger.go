package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	logFileName    = "cronos-agent.log"
	logMaxBytes    = 10 * 1024 * 1024 // 10 MB
	logMaxBackups  = 3
)

type RotatingLogger struct {
	mu       sync.Mutex
	file     *os.File
	filePath string
	size     int64
}

func NewRotatingLogger() (*RotatingLogger, error) {
	logPath := filepath.Join(agentDir(), logFileName)

	rl := &RotatingLogger{filePath: logPath}
	if err := rl.openOrCreate(); err != nil {
		return nil, err
	}
	return rl, nil
}

func (rl *RotatingLogger) Write(p []byte) (int, error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.size+int64(len(p)) > logMaxBytes {
		if err := rl.rotate(); err != nil {
			return 0, err
		}
	}

	n, err := rl.file.Write(p)
	rl.size += int64(n)
	return n, err
}

func (rl *RotatingLogger) Close() error {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.file != nil {
		return rl.file.Close()
	}
	return nil
}

func (rl *RotatingLogger) openOrCreate() error {
	f, err := os.OpenFile(rl.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("error abriendo log: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("error obteniendo info del log: %w", err)
	}

	rl.file = f
	rl.size = info.Size()
	return nil
}

func (rl *RotatingLogger) rotate() error {
	rl.file.Close()

	for i := logMaxBackups; i >= 1; i-- {
		src := rl.backupName(i - 1)
		dst := rl.backupName(i)
		if i == logMaxBackups {
			os.Remove(dst)
		}
		os.Rename(src, dst)
	}

	os.Rename(rl.filePath, rl.backupName(1))

	return rl.openOrCreate()
}

func (rl *RotatingLogger) backupName(index int) string {
	if index == 0 {
		return rl.filePath
	}
	return fmt.Sprintf("%s.%d", rl.filePath, index)
}

func SetupLogger() (io.Closer, error) {
	rl, err := NewRotatingLogger()
	if err != nil {
		return nil, err
	}

	multiWriter := io.MultiWriter(os.Stdout, rl)
	log.SetOutput(multiWriter)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)

	return rl, nil
}
