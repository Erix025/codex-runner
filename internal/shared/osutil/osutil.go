package osutil

import (
	"errors"
	"os"
	"path/filepath"
)

func ExpandUser(p string) (string, error) {
	if p == "" || p[0] != '~' {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if p == "~" {
		return home, nil
	}
	if len(p) >= 2 && (p[1] == '/' || p[1] == '\\') {
		return filepath.Join(home, p[2:]), nil
	}
	return "", errors.New("only current user home expansion (~ or ~/...) is supported")
}
