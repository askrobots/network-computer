//go:build linux || darwin

package main

import (
	"os"
	"syscall"
)

func freeBytes(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// chownLike gives a received file to whoever owns the directory it lands in
// (the host runs as root; the file belongs to the desk user).
func chownLike(path, dir string) {
	if fi, err := os.Stat(dir); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			os.Chown(path, int(st.Uid), int(st.Gid))
		}
	}
}

// ownerGroup is the group of whoever owns dir (the desk user).
func ownerGroup(dir string) (int, bool) {
	fi, err := os.Stat(dir)
	if err != nil {
		return 0, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Gid), true
}
