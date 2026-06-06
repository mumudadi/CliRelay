package authfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

var ErrAuthManagerUnavailable = errors.New("core auth manager unavailable")

type UploadService struct {
	AuthDir    string
	Manager    *coreauth.Manager
	Repository Repository
	Now        time.Time
}

type UploadResult struct {
	Name string
	Path string
}

func (s UploadService) Available() bool {
	return s.Manager != nil
}

func IsUploadValidationError(err error) bool {
	if err == nil {
		return false
	}
	switch err.Error() {
	case "invalid name", "name must end with .json", "file must be .json":
		return true
	default:
		return false
	}
}

func (s UploadService) ValidateMultipartFilename(filename string) (string, error) {
	return ValidateUploadedFileName(filename)
}

func (s UploadService) ValidateRawName(name string) (string, error) {
	return ValidateFileQueryName(name, true)
}

func (s UploadService) UploadMultipart(ctx context.Context, filename string, data []byte) (UploadResult, error) {
	name, errValidate := s.ValidateMultipartFilename(filename)
	if errValidate != nil {
		return UploadResult{}, errValidate
	}
	return s.upload(ctx, name, data, "failed to save file")
}

func (s UploadService) UploadRaw(ctx context.Context, name string, data []byte) (UploadResult, error) {
	name, errValidate := s.ValidateRawName(name)
	if errValidate != nil {
		return UploadResult{}, errValidate
	}
	return s.upload(ctx, name, data, "failed to write file")
}

func (s UploadService) upload(ctx context.Context, name string, data []byte, writeMessage string) (UploadResult, error) {
	if !s.Available() {
		return UploadResult{}, ErrAuthManagerUnavailable
	}
	if strings.TrimSpace(name) == "" {
		return UploadResult{}, fmt.Errorf("invalid name")
	}
	dst := FilePath(s.AuthDir, name)
	if errWrite := os.WriteFile(dst, data, 0o600); errWrite != nil {
		return UploadResult{}, fmt.Errorf("%s: %w", writeMessage, errWrite)
	}
	if errRegister := (Registrar{
		Manager: s.Manager,
		AuthDir: s.AuthDir,
		Now:     s.Now,
	}).RegisterFile(ctx, dst, data); errRegister != nil {
		return UploadResult{}, errRegister
	}
	if errPersist := s.Repository.PersistChange(ctx, "Update auth "+name, dst); errPersist != nil {
		return UploadResult{}, errPersist
	}
	return UploadResult{Name: name, Path: dst}, nil
}
