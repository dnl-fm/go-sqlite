package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

var sourceSuffixes = []string{"", "-log", "-wal", "-shm", "-journal"}

type fileState struct {
	info os.FileInfo
	hash [32]byte
}

// Query advisory locks through a read-only descriptor. F_GETLK does not
// acquire a lock, and flock changes no file bytes.
func inspectFile(path string) (fileState, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return fileState{}, err
	}
	if !info.Mode().IsRegular() {
		return fileState{}, fmt.Errorf("%s is not a regular file (symlinks are refused)", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return fileState{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return fileState{}, err
	}
	if !os.SameFile(info, opened) {
		return fileState{}, fmt.Errorf("source changed while opening %s; stop the owning service", path)
	}
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: io.SeekStart, Start: 0, Len: 0}
	if err = unix.FcntlFlock(f.Fd(), unix.F_GETLK, &lock); err != nil {
		return fileState{}, fmt.Errorf("inspect locks on %s: %w", path, err)
	}
	if lock.Type != unix.F_UNLCK {
		return fileState{}, fmt.Errorf("source %s is in use (fcntl lock, pid %d); stop the owning service", path, lock.Pid)
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB); err != nil {
		return fileState{}, fmt.Errorf("source %s is in use (flock): %w; stop the owning service", path, err)
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		return fileState{}, err
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return fileState{}, err
	}
	after, err := f.Stat()
	if err != nil {
		return fileState{}, err
	}
	if opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) {
		return fileState{}, fmt.Errorf("source %s changed while reading; stop the owning service", path)
	}
	return fileState{info: opened, hash: [32]byte(h.Sum(nil))}, nil
}

func snapshot(src string) (map[string]fileState, error) {
	files := make(map[string]fileState)
	for _, suffix := range sourceSuffixes {
		state, err := inspectFile(src + suffix)
		if suffix != "" && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		files[suffix] = state
	}
	return files, nil
}

func unchanged(src string, before map[string]fileState) error {
	after, err := snapshot(src)
	if err != nil {
		return err
	}
	if len(after) != len(before) {
		return fmt.Errorf("source sidecars changed; stop the owning service")
	}
	for suffix, old := range before {
		now, ok := after[suffix]
		if !ok || !os.SameFile(old.info, now.info) || old.info.Size() != now.info.Size() || !old.info.ModTime().Equal(now.info.ModTime()) || old.hash != now.hash {
			return fmt.Errorf("source %s changed during conversion; stop the owning service", src+suffix)
		}
	}
	return nil
}

func stage(src, dir string, before map[string]fileState) (string, error) {
	path := filepath.Join(dir, "converted.db")
	for _, suffix := range sourceSuffixes {
		old, exists := before[suffix]
		if !exists {
			continue
		}
		in, err := os.Open(src + suffix)
		if err != nil {
			return "", err
		}
		out, err := os.OpenFile(path+suffix, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			in.Close()
			return "", err
		}
		h := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(out, h), in)
		err = errors.Join(copyErr, in.Close(), out.Sync(), out.Close())
		if err != nil {
			return "", err
		}
		if old.hash != [32]byte(h.Sum(nil)) {
			return "", fmt.Errorf("source %s changed while copying; stop the owning service", src+suffix)
		}
	}
	return path, unchanged(src, before)
}

func destinationFree(dst string) error {
	for _, suffix := range sourceSuffixes {
		_, err := os.Lstat(dst + suffix)
		if err == nil {
			return fmt.Errorf("destination %s already exists", dst+suffix)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func install(staged, dst string) error {
	// Unlike os.Rename, RENAME_NOREPLACE cannot overwrite a destination that
	// another process creates after our final existence check.
	if err := unix.Renameat2(unix.AT_FDCWD, staged, unix.AT_FDCWD, dst, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("install %s without overwrite: %w", dst, err)
	}
	d, err := os.Open(filepath.Dir(dst))
	if err != nil {
		_ = os.Remove(dst)
		return err
	}
	err = errors.Join(d.Sync(), d.Close())
	if err != nil {
		_ = os.Remove(dst)
	}
	return err
}
