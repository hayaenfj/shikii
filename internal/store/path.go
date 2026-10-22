package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// parentRoot は保存先から末端の親までを通常のフォルダだけで開く。
// 各フォルダを開いた後も同じ実体か確かめ、以後は名前の差し替えに影響されない Root を使う。
func (s Store) parentRoot(path string, create bool) (*os.Root, string, error) {
	rootPath, err := filepath.Abs(s.Dir)
	if err != nil {
		return nil, "", err
	}
	fullPath, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	relative, err := filepath.Rel(rootPath, fullPath)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, "", errors.New("保存先の外のファイルは開けません")
	}
	if create {
		if err := os.MkdirAll(rootPath, 0700); err != nil {
			return nil, "", err
		}
	}
	info, err := os.Lstat(rootPath)
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, "", errors.New("保存先にリンクは使えません")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, "", err
	}
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		root.Close()
		return nil, "", errors.New("保存先のフォルダが変わりました")
	}
	parts := strings.Split(relative, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		if create {
			if err := root.Mkdir(part, 0700); err != nil && !os.IsExist(err) {
				root.Close()
				return nil, "", err
			}
		}
		info, err := root.Lstat(part)
		if err != nil {
			root.Close()
			return nil, "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, "", errors.New("記録のフォルダにリンクは使えません")
		}
		child, err := root.OpenRoot(part)
		if err != nil {
			root.Close()
			return nil, "", err
		}
		actual, err := child.Stat(".")
		root.Close()
		if err != nil || !os.SameFile(info, actual) {
			child.Close()
			return nil, "", errors.New("記録のフォルダが変わりました")
		}
		root = child
	}
	return root, parts[len(parts)-1], nil
}

func (s Store) openFile(path string, flags int) (*os.File, error) {
	root, name, err := s.parentRoot(path, flags&os.O_CREATE != 0)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if info, err := root.Lstat(name); err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("記録はリンクではない通常のファイルにしてください")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// 同じ新しいファイルを同時に作ると(hook の並行する判定、wiki の差し込み先の並行する判定)、os.Root の作成付きの open が
	// ENOENT を返すことがある(2026-10-23、darwin で再現: TestFirstRecordsCanBeAppendedConcurrently)。作成付きなら短く待って開き直す。
	var f *os.File
	for attempt := 0; ; attempt++ {
		f, err = root.OpenFile(name, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
		if err == nil || flags&os.O_CREATE == 0 || !os.IsNotExist(err) || attempt >= 3 {
			break
		}
		time.Sleep(time.Duration(attempt+1) * time.Millisecond)
	}
	if err != nil {
		return nil, err // 包まない(呼ぶ側が os.IsNotExist で「まだ無い」を見分ける)
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("記録のファイルを確かめられません")
	}
	return f, nil
}
