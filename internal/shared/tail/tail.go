package tail

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
)

// ReadTailBytes reads up to maxBytes from the end of the file.
func ReadTailBytes(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return []byte{}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	if size == 0 {
		return []byte{}, nil
	}
	start := size - maxBytes
	if start < 0 {
		start = 0
	}
	_, err = f.Seek(start, io.SeekStart)
	if err != nil {
		return nil, err
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if start > 0 && len(buf) > 0 {
		// If we started in the middle of a line, drop the partial first line.
		for i, b := range buf {
			if b == '\n' {
				return buf[i+1:], nil
			}
		}
		return []byte{}, nil
	}
	return buf, nil
}

// ReadTailLines reads the last maxLines lines from file.
func ReadTailLines(path string, maxLines int) ([]byte, error) {
	if maxLines <= 0 {
		return []byte{}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	lines := make([][]byte, 0, maxLines)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		if len(lines) < maxLines {
			lines = append(lines, line)
			continue
		}
		copy(lines, lines[1:])
		lines[len(lines)-1] = line
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return []byte{}, nil
	}
	return bytes.Join(lines, []byte{'\n'}), nil
}

// ReadAll reads the whole file as bytes.
func ReadAll(path string) ([]byte, error) {
	return os.ReadFile(path)
}

var ErrNotFound = errors.New("not found")
