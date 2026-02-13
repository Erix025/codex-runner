package tail

import (
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

var ErrNotFound = errors.New("not found")
