// Package fsutil holds the one way the agent reads a configuration or an
// application file: a regular file only, never a FIFO, a device or a
// directory, without blocking and with a size cap. Files are read through
// an afero.Fs, so that tests run on an in-memory file system.
package fsutil

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"syscall"

	"github.com/spf13/afero"
)

const (
	// MaxConfigSize caps a configuration file: nginx, Apache, HAProxy,
	// supervisor, PHP-FPM.
	MaxConfigSize int64 = 4 << 20
	// MaxAppFileSize caps an application file: .env, composer.lock,
	// package.json…
	MaxAppFileSize int64 = 8 << 20
	// MaxLinkHops is the number of symbolic links ReadRegularLinks follows:
	// a Capistrano or Deployer release links its .env to shared/ (one hop);
	// a longer chain is refused.
	MaxLinkHops = 5
)

var (
	ErrNotRegular   = errors.New("not a regular file")
	ErrTooLarge     = errors.New("file too large")
	ErrLinkLoop     = errors.New("symbolic link loop")
	ErrTooManyLinks = errors.New("too many symbolic links")
)

// ReadRegular reads a regular file of at most max bytes, refusing a
// symbolic link: a .env linked to /dev/zero or to a file of root never
// gets read. It opens with O_NONBLOCK, so that a FIFO does not block.
func ReadRegular(fsys afero.Fs, name string, max int64) ([]byte, error) {
	return read(fsys, name, max, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK)
}

// ReadRegularFollow is ReadRegular following a symbolic link: the
// configuration of a webserver is made of links (sites-enabled), and is
// read with the rights of its process (see system.ReadAs).
func ReadRegularFollow(fsys afero.Fs, name string, max int64) ([]byte, error) {
	return read(fsys, name, max, os.O_RDONLY|syscall.O_NONBLOCK)
}

// ReadRegularLinks is ReadRegular following at most hops symbolic links,
// resolved one by one: releases/X/.env -> shared/.env is read, a loop or a
// longer chain is refused. The target must be a regular file, opened
// without following a link (a link swapped in after the resolution is
// refused), without blocking and size capped. Only the last component is
// resolved here; the directories of a path are followed by the kernel, as
// ReadRegular does.
func ReadRegularLinks(fsys afero.Fs, name string, max int64, hops int) ([]byte, error) {
	target, err := ResolveLinks(fsys, name, hops)
	if err != nil {
		return nil, err
	}
	return ReadRegular(fsys, target, max)
}

// ReadAppFile reads an application file (.env, composer.json, a PHP
// config…): ReadRegularLinks capped at MaxAppFileSize and MaxLinkHops.
func ReadAppFile(fsys afero.Fs, name string) ([]byte, error) {
	return ReadRegularLinks(fsys, name, MaxAppFileSize, MaxLinkHops)
}

// ResolveLinks follows the symbolic links of the last component of name,
// at most hops of them; a file system that cannot read links returns name.
// A relative target is resolved against the directory of the link.
func ResolveLinks(fsys afero.Fs, name string, hops int) (string, error) {
	lr, ok := fsys.(afero.LinkReader)
	if !ok {
		return name, nil
	}
	current := path.Clean(name)
	seen := map[string]bool{current: true}
	for n := 0; ; n++ {
		target, err := lr.ReadlinkIfPossible(current)
		if err != nil {
			// Not a link, or missing: the open tells which.
			return current, nil
		}
		if n >= hops {
			return "", &fs.PathError{Op: "readlink", Path: name, Err: fmt.Errorf("%w (more than %d)", ErrTooManyLinks, hops)}
		}
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(current), target)
		}
		target = path.Clean(target)
		if seen[target] {
			return "", &fs.PathError{Op: "readlink", Path: name, Err: fmt.Errorf("%w at %s", ErrLinkLoop, target)}
		}
		seen[target] = true
		current = target
	}
}

func read(fsys afero.Fs, name string, max int64, flag int) ([]byte, error) {
	f, err := fsys.OpenFile(name, flag, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, &fs.PathError{Op: "read", Path: name, Err: ErrNotRegular}
	}
	if fi.Size() > max {
		return nil, &fs.PathError{Op: "read", Path: name, Err: ErrTooLarge}
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, &fs.PathError{Op: "read", Path: name, Err: ErrTooLarge}
	}
	return b, nil
}

// Exit codes of "lacis-agent internal read", so that the agent knows why
// a file read as another user failed.
const (
	ExitOK = iota
	ExitFailure
	ExitNotExist
	ExitPermission
	ExitNotRegular
	ExitTooLarge
)

var exitErrors = map[int]error{
	ExitNotExist:   fs.ErrNotExist,
	ExitPermission: fs.ErrPermission,
	ExitNotRegular: ErrNotRegular,
	ExitTooLarge:   ErrTooLarge,
}

// ExitCode returns the exit code telling err.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	for code, target := range exitErrors {
		if errors.Is(err, target) {
			return code
		}
	}
	return ExitFailure
}

// ErrorOf returns the error told by an exit code of internal read,
// wrapping msg; nil for ExitOK.
func ErrorOf(code int, msg string) error {
	if code == ExitOK {
		return nil
	}
	if target, ok := exitErrors[code]; ok {
		return fmt.Errorf("%s: %w", msg, target)
	}
	return errors.New(msg)
}
