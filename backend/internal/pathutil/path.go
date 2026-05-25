package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

func AbsFrom(baseDir string, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	expanded, err := ExpandHome(value)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(baseDir, expanded)
	}
	return filepath.Abs(expanded)
}

func ExpandHome(value string) (string, error) {
	if value != "~" && !strings.HasPrefix(value, "~/") {
		return value, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if value == "~" {
		return homeDir, nil
	}
	return filepath.Join(homeDir, strings.TrimPrefix(value, "~/")), nil
}
