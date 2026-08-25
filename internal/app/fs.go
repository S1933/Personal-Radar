package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func nowDateString() string { return time.Now().Format("2006-01-02") }

func mkdirAll(dir string) error { return os.MkdirAll(dir, 0o755) }

// vaultSuccess normalizes mkdir errors: existing dir is fine.
func vaultSuccess(err error) bool { return err == nil || os.IsExist(err) }

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

var _ = fmt.Sprintf
