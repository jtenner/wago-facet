package facet

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func pinPreopens(preopens []Preopen) ([]int, error) {
	fds := make([]int, 0, len(preopens))
	for _, preopen := range preopens {
		flags := unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC
		if preopen.Rights&RightSync != 0 {
			// Keep the root pinned with a descriptor that can actually be synced.
			// An O_PATH descriptor is sufficient for resolution but fsync rejects it.
			flags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC
		}
		fd, err := unix.Open(preopen.Host, flags, 0)
		if err != nil {
			closePinnedPreopens(fds)
			return nil, fmt.Errorf("facet preopen %q: %w", preopen.Guest, err)
		}
		fds = append(fds, fd)
	}
	return fds, nil
}

func duplicatePinnedPreopen(fd int) (int, error) {
	return unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
}

func closePinnedPreopens(fds []int) {
	for _, fd := range fds {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
	}
}
