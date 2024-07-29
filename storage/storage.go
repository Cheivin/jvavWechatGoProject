package storage

import (
	"io"
	"os"
	"path/filepath"
	"time"
)

type Storage interface {
	Reader(filename string) (io.ReadCloser, error)
	Writer(filename string) (io.WriteCloser, string, error)
}

type LocalStorage struct {
	DataDir string
}

func NewLocalStorage(dataDir string) *LocalStorage {
	if err := os.MkdirAll(dataDir, os.ModePerm); err != nil {
		panic(err)
	}
	return &LocalStorage{
		DataDir: dataDir,
	}
}

func (s LocalStorage) Reader(filename string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(s.DataDir, filename))
}
func (s LocalStorage) Writer(filename string) (io.WriteCloser, string, error) {
	filename = filepath.Join(time.Now().Format("2006/01/02"), filename)
	savePath := filepath.Join(s.DataDir, filename)
	if err := os.MkdirAll(filepath.Dir(savePath), os.ModePerm); err != nil {
		return nil, filename, err
	}
	file, err := os.Create(savePath)
	if err != nil {
		return nil, filename, err
	}
	return file, filename, nil
}
