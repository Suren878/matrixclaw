package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type safeFile struct {
	abs  string
	info fs.FileInfo
}

func resolveStoreRoot(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(realRoot)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("storage root is not a directory: %s", root)
	}
	return filepath.Clean(realRoot), nil
}

func (s *LocalStore) ensureMetadataRoot() error {
	_, err := ensureSafeDir(s.root, metadataDirName, nil)
	return err
}

func (s *LocalStore) ensureTempRoot() error {
	_, err := ensureSafeDir(s.root, tempDirName, nil)
	return err
}

func (s *LocalStore) fileForRead(cleanPath string) (safeFile, error) {
	return resolveExistingFile(s.root, cleanPath, s.reservedAbs)
}

func (s *LocalStore) fileForWrite(cleanPath string) (string, error) {
	return resolveWritableFile(s.root, cleanPath, s.reservedAbs)
}

func (s *LocalStore) fileForDelete(cleanPath string) (safeFile, error) {
	return resolveDeletableFile(s.root, cleanPath, s.reservedAbs)
}

func (s *LocalStore) tempFileForRead(cleanPath string) (safeFile, error) {
	if err := s.ensureTempRoot(); err != nil {
		return safeFile{}, err
	}
	return resolveExistingFile(s.tempRoot, cleanPath, nil)
}

func (s *LocalStore) tempFileForWrite(cleanPath string) (string, error) {
	if err := s.ensureTempRoot(); err != nil {
		return "", err
	}
	return resolveWritableFile(s.tempRoot, cleanPath, nil)
}

func (s *LocalStore) tempFileForDelete(cleanPath string) (safeFile, error) {
	if err := s.ensureTempRoot(); err != nil {
		return safeFile{}, err
	}
	return resolveDeletableFile(s.tempRoot, cleanPath, nil)
}

func (s *LocalStore) reservedAbs(absPath string) bool {
	rel, ok := relUnderRoot(s.root, absPath)
	if !ok || rel == "." {
		return false
	}
	first := rel
	if idx := strings.IndexRune(rel, filepath.Separator); idx >= 0 {
		first = rel[:idx]
	}
	return first == metadataDirName || first == tempDirName
}

func resolveExistingFile(root string, cleanPath string, reject func(string) bool) (safeFile, error) {
	absPath, err := joinedUnderRoot(root, cleanPath)
	if err != nil {
		return safeFile{}, err
	}
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return safeFile{}, err
	}
	realPath = filepath.Clean(realPath)
	if !isPathUnderRoot(root, realPath) || rejectsPath(realPath, reject) {
		return safeFile{}, ErrInvalidPath
	}
	info, err := os.Stat(realPath)
	if err != nil {
		return safeFile{}, err
	}
	return safeFile{abs: realPath, info: info}, nil
}

func resolveWritableFile(root string, cleanPath string, reject func(string) bool) (string, error) {
	parentClean := path.Dir(cleanPath)
	if parentClean == "." {
		parentClean = ""
	}
	parentAbs, err := ensureSafeDir(root, parentClean, reject)
	if err != nil {
		return "", err
	}
	target := filepath.Join(parentAbs, filepath.Base(filepath.FromSlash(cleanPath)))
	if !isPathUnderRoot(root, target) || rejectsPath(target, reject) {
		return "", ErrInvalidPath
	}
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return target, nil
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", ErrInvalidPath
	}
	if info.IsDir() {
		return "", fmt.Errorf("storage path is a directory: %s", cleanPath)
	}
	return target, nil
}

func resolveDeletableFile(root string, cleanPath string, reject func(string) bool) (safeFile, error) {
	parentClean := path.Dir(cleanPath)
	if parentClean == "." {
		parentClean = ""
	}
	parentAbs, err := existingSafeDir(root, parentClean, reject)
	if err != nil {
		return safeFile{}, err
	}
	target := filepath.Join(parentAbs, filepath.Base(filepath.FromSlash(cleanPath)))
	if !isPathUnderRoot(root, target) || rejectsPath(target, reject) {
		return safeFile{}, ErrInvalidPath
	}
	info, err := os.Lstat(target)
	if err != nil {
		return safeFile{}, err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return safeFile{}, fmt.Errorf("storage path is a directory: %s", cleanPath)
	}
	return safeFile{abs: target, info: info}, nil
}

func writeCheckedFile(absPath string, content []byte, perm fs.FileMode, cleanPath string) error {
	if err := ensureFinalWriteTarget(absPath, cleanPath, true); err != nil {
		return err
	}
	return os.WriteFile(absPath, content, perm)
}

func removeCheckedFile(file safeFile, cleanPath string) error {
	if err := ensureFinalRemoveTarget(file.abs, cleanPath); err != nil {
		return err
	}
	return os.Remove(file.abs)
}

func ensureFinalWriteTarget(absPath string, cleanPath string, allowMissing bool) error {
	parent := filepath.Dir(absPath)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPath
	}
	if !parentInfo.IsDir() {
		return fmt.Errorf("storage path parent is not a directory: %s", cleanPath)
	}

	info, err := os.Lstat(absPath)
	if err != nil {
		if allowMissing && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPath
	}
	if info.IsDir() {
		return fmt.Errorf("storage path is a directory: %s", cleanPath)
	}
	return nil
}

func ensureFinalRemoveTarget(absPath string, cleanPath string) error {
	parent := filepath.Dir(absPath)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPath
	}
	if !parentInfo.IsDir() {
		return fmt.Errorf("storage path parent is not a directory: %s", cleanPath)
	}

	info, err := os.Lstat(absPath)
	if err != nil {
		return err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("storage path is a directory: %s", cleanPath)
	}
	return nil
}

func ensureSafeDir(root string, cleanDir string, reject func(string) bool) (string, error) {
	return walkSafeDir(root, cleanDir, reject, true)
}

func existingSafeDir(root string, cleanDir string, reject func(string) bool) (string, error) {
	return walkSafeDir(root, cleanDir, reject, false)
}

func walkSafeDir(root string, cleanDir string, reject func(string) bool, create bool) (string, error) {
	root = filepath.Clean(root)
	if cleanDir == "" || cleanDir == "." {
		return root, nil
	}
	current := root
	for _, part := range strings.Split(filepath.FromSlash(cleanDir), string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		next := filepath.Join(current, part)
		if !isPathUnderRoot(root, next) || rejectsPath(next, reject) {
			return "", ErrInvalidPath
		}
		info, err := os.Lstat(next)
		if err != nil {
			if create && errors.Is(err, fs.ErrNotExist) {
				if err := os.Mkdir(next, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
					return "", err
				}
				info, err = os.Lstat(next)
			}
			if err != nil {
				return "", err
			}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", ErrInvalidPath
		}
		if !info.IsDir() {
			return "", fmt.Errorf("storage path parent is not a directory: %s", cleanDir)
		}
		current = next
	}
	return current, nil
}

func joinedUnderRoot(root string, cleanPath string) (string, error) {
	absPath := filepath.Clean(filepath.Join(root, filepath.FromSlash(cleanPath)))
	if !isPathUnderRoot(root, absPath) {
		return "", ErrInvalidPath
	}
	return absPath, nil
}

func rejectsPath(absPath string, reject func(string) bool) bool {
	return reject != nil && reject(filepath.Clean(absPath))
}

func isPathUnderRoot(root string, absPath string) bool {
	_, ok := relUnderRoot(root, absPath)
	return ok
}

func relUnderRoot(root string, absPath string) (string, bool) {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(absPath))
	if err != nil {
		return "", false
	}
	if rel == "." {
		return rel, true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}
