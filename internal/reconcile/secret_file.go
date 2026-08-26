package reconcile

import (
	"errors"
	"fmt"
	"os"
)

type regularNonEmptyFileReader func(path string) ([]byte, error)

func readRegularNonEmptyFile(path string) ([]byte, error) {
	return readRegularNonEmptyFileWith(path, os.Stat, os.ReadFile)
}

func readRegularNonEmptyFileWith(
	path string,
	stat func(string) (os.FileInfo, error),
	readFile func(string) ([]byte, error),
) ([]byte, error) {
	info, err := stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("does not exist: %w", os.ErrNotExist)
		}
		return nil, fmt.Errorf("cannot be inspected: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("is not a regular file")
	}
	if info.Size() == 0 {
		return nil, errors.New("is empty")
	}

	contents, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot be read: %w", err)
	}
	if len(contents) == 0 {
		return nil, errors.New("is empty")
	}
	return contents, nil
}
