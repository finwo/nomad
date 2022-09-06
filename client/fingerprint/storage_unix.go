//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris
// +build darwin dragonfly freebsd linux netbsd openbsd solaris

package fingerprint

import (
	"fmt"
	"path/filepath"
	"syscall"
	"github.com/moby/sys/mountinfo"
)

// diskFree inspects the filesystem for path and returns the volume name and
// the total and free bytes available on the file system.
func (f *StorageFingerprint) diskFree(path string) (volume string, total, free uint64, err error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", 0, 0, fmt.Errorf("failed to determine absolute path for %s", path)
	}

	// Execute statfs on the given path
	stat := syscall.Statfs_t{}
	err = syscall.Statfs(absPath, &stat)
	if err != nil {
		return "", 0, 0, fmt.Errorf("failed to perform statfs on %s\n%s", absPath, err.Error())
	}

	// Convert blocks into bytes
	total = uint64(stat.Bsize) * stat.Blocks
	free = uint64(stat.Bsize) * stat.Bavail

	// Fetch the mount point for the path
	filter := mountinfo.ParentsFilter(absPath)
	info, err := mountinfo.GetMounts(filter)
	if err != nil {
		return "", 0, 0, fmt.Errorf("failed to determine mount point for %s", absPath)
	}

	// Get the deepest mount point for the path
	volume = ""
	for _, entry := range info {
		if info == nil || len(volume) < len(entry.Root) {
			volume = entry.Root
		}
	}

	return volume, total, free, nil
}
