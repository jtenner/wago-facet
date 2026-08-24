package facet

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func pinPreopens(preopens []Preopen) ([]int, error) {
	fds := make([]int, 0, len(preopens))
	for _, preopen := range preopens {
		fd, err := unix.Open(preopen.Host, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			closePinnedPreopens(fds)
			return nil, fmt.Errorf("facet preopen %q: %w", preopen.Guest, err)
		}
		fds = append(fds, fd)
	}
	return fds, nil
}

func duplicatePinnedPreopen(fd int) (int, error) {
	dup, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	return dup, nil
}

func closePinnedPreopens(fds []int) {
	for _, fd := range fds {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
	}
}
