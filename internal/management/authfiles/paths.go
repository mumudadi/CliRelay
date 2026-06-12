package authfiles

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
)

const invalidFilePathPlaceholder = "__invalid_auth_file_name__"

func ValidateFileQueryName(name string, requireJSON bool) (string, error) {
	name = strings.TrimSpace(name)
	if !isSafeSingleFileName(name) {
		return "", fmt.Errorf("invalid name")
	}
	if requireJSON && !IsJSONFileName(name) {
		return "", fmt.Errorf("name must end with .json")
	}
	return name, nil
}

func ValidateUploadedFileName(filename string) (string, error) {
	name := singleFileBaseName(filename)
	if !isSafeSingleFileName(name) {
		return "", fmt.Errorf("invalid name")
	}
	if !IsJSONFileName(name) {
		return "", fmt.Errorf("file must be .json")
	}
	return name, nil
}

func singleFileBaseName(name string) string {
	return path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
}

func isSafeSingleFileName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return false
	}
	if strings.ContainsAny(name, "\r\n\t") {
		return false
	}
	return filepath.Base(name) == name && singleFileBaseName(name) == name
}

func IsDeleteAllValue(value string) bool {
	return value == "true" || value == "1" || value == "*"
}

func IsJSONFileName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".json")
}

func FilePath(authDir, name string) string {
	baseName := singleFileBaseName(name)
	if !isSafeSingleFileName(baseName) {
		baseName = invalidFilePathPlaceholder
	}
	full := filepath.Join(authDir, baseName)
	if filepath.IsAbs(full) {
		return full
	}
	if abs, errAbs := filepath.Abs(full); errAbs == nil {
		return abs
	}
	return full
}

func AuthIDForPath(authDir, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.TrimSpace(authDir) == "" {
		return filepath.Clean(path)
	}
	resolvedAuthDir, errResolve := util.ResolveAuthDir(authDir)
	if errResolve != nil || strings.TrimSpace(resolvedAuthDir) == "" {
		return filepath.Clean(path)
	}
	if !filepath.IsAbs(resolvedAuthDir) {
		if abs, errAbs := filepath.Abs(resolvedAuthDir); errAbs == nil {
			resolvedAuthDir = abs
		}
	}
	if evaluated, errEval := filepath.EvalSymlinks(resolvedAuthDir); errEval == nil {
		resolvedAuthDir = evaluated
	}
	normalizedPath := filepath.Clean(path)
	if !filepath.IsAbs(normalizedPath) {
		if abs, errAbs := filepath.Abs(normalizedPath); errAbs == nil {
			normalizedPath = abs
		}
	}
	if evaluated, errEval := filepath.EvalSymlinks(normalizedPath); errEval == nil {
		normalizedPath = evaluated
	}
	if rel, err := filepath.Rel(resolvedAuthDir, normalizedPath); err == nil && rel != "" && rel != "." && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return normalizedPath
}
