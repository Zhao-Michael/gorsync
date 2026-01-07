//go:build windows

package receiver

import (
	"crypto/rand"
	"encoding/base32"
	"os"
	"path/filepath"
	"strings"
)

type pendingFile struct {
	fn string
	f  *os.File
}

func newPendingFile(fn string) (*pendingFile, error) {
	f, err := os.CreateTemp(filepath.Dir(fn), "temp-rsync-*")
	if err != nil {
		return nil, err
	}
	return &pendingFile{
		fn: fn,
		f:  f,
	}, nil
}

func (p *pendingFile) Write(buf []byte) (n int, _ error) {
	return p.f.Write(buf)
}

func (p *pendingFile) CloseAtomicallyReplace() error {
	if err := p.f.Close(); err != nil {
		return err
	}
	if err := rename(p.f.Name(), p.fn); err != nil {
		return err
	}
	return nil
}

func (p *pendingFile) Cleanup() error {
	tmpName := p.f.Name()
	err := p.f.Close()
	if err := os.Remove(tmpName); err != nil {
		return err
	}
	return err
}

func makeTempName(origname, prefix string) (tempname string, err error) {
	origname = filepath.Clean(origname)
	if len(origname) == 0 || origname[len(origname)-1] == filepath.Separator {
		return "", os.ErrInvalid
	}
	// Generate 10 random bytes.
	// This gives 80 bits of entropy, good enough
	// for making temporary file name unpredictable.
	var rnd [10]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	name := prefix + "-" + strings.ToLower(base32.StdEncoding.EncodeToString(rnd[:])) + ".tmp"
	return filepath.Join(filepath.Dir(origname), name), nil
}

func rename(oldname, newname string) error {
	err := os.Rename(oldname, newname)
	if err != nil {
		// If newname exists ("original"), we will try renaming it to a
		// new temporary name, then renaming oldname to the newname,
		// and deleting the renamed original. If system crashes between
		// renaming and deleting, the original file will still be available
		// under the temporary name, so users can manually recover data.
		// (No automatic recovery is possible because after crash the
		// temporary name is not known.)
		var origtmp string
		for {
			origtmp, err = makeTempName(newname, filepath.Base(newname))
			if err != nil {
				return err
			}
			_, err = os.Stat(origtmp)
			if err == nil {
				continue // most likely will never happen
			}
			break
		}
		err = os.Rename(newname, origtmp)
		if err != nil {
			return err
		}
		err = os.Rename(oldname, newname)
		if err != nil {
			// Rename still fails, try to revert original rename,
			// ignoring errors.
			os.Rename(origtmp, newname)
			return err
		}
		// Rename succeeded, now delete original file.
		os.Remove(origtmp)
	}
	return nil
}
