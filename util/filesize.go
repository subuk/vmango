package util

import (
	"os"
)

func GetFileSize(filename string) (uint64, error) {
	file, err := os.Open(filename)
	if err != nil {
		return 0, err
	}
	size, err := file.Seek(0, os.SEEK_END)
	if err != nil {
		return 0, err
	}
	return uint64(size), nil
}
