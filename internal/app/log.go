package app

import (
	"fmt"
	"os"
	"sync"
)

// rotatingLog keeps the current file and three 10 MB archives.
type rotatingLog struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
}

func openLog(path string) (*rotatingLog, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	return &rotatingLog{path: path, file: file, size: info.Size()}, nil
}
func (l *rotatingLog) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return 0, os.ErrClosed
	}
	if l.size+int64(len(data)) > 10<<20 {
		if err := l.file.Close(); err != nil {
			return 0, err
		}
		l.file = nil
		for index := 3; index >= 1; index-- {
			source := l.path
			if index > 1 {
				source = fmt.Sprintf("%s.%d", l.path, index-1)
			}
			destination := fmt.Sprintf("%s.%d", l.path, index)
			if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
				return 0, err
			}
			if err := os.Rename(source, destination); err != nil && !os.IsNotExist(err) {
				return 0, err
			}
		}
		file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return 0, err
		}
		l.file = file
		l.size = 0
	}
	count, err := l.file.Write(data)
	l.size += int64(count)
	return count, err
}
func (l *rotatingLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
