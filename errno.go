package facet

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"syscall"
)

func errorCode(err error) int32 {
	if err == nil {
		return ErrOK
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ErrNoEntry
	case errors.Is(err, fs.ErrExist):
		return ErrExists
	case errors.Is(err, fs.ErrPermission):
		return ErrAccess
	case errors.Is(err, os.ErrDeadlineExceeded):
		return ErrTimedOut
	case errors.Is(err, syscall.EAGAIN), errors.Is(err, syscall.EWOULDBLOCK), errors.Is(err, syscall.EINPROGRESS), errors.Is(err, syscall.EALREADY):
		return ErrAgain
	case errors.Is(err, syscall.EBADF):
		return ErrBadHandle
	case errors.Is(err, syscall.EBUSY):
		return ErrBusy
	case errors.Is(err, syscall.EISDIR):
		return ErrIsDirectory
	case errors.Is(err, syscall.ENOTDIR):
		return ErrNotDirectory
	case errors.Is(err, syscall.EINVAL):
		return ErrInvalid
	case errors.Is(err, syscall.EFBIG):
		return ErrFileTooLarge
	case errors.Is(err, syscall.ENOSPC):
		return ErrNoSpace
	case errors.Is(err, syscall.EROFS):
		return ErrReadOnly
	case errors.Is(err, syscall.EPIPE):
		return ErrPipe
	case errors.Is(err, syscall.ENOTEMPTY):
		return ErrNotEmpty
	case errors.Is(err, syscall.ELOOP):
		return ErrLoop
	case errors.Is(err, syscall.ENAMETOOLONG):
		return ErrNameTooLong
	case errors.Is(err, syscall.ENOTSUP), errors.Is(err, syscall.EOPNOTSUPP):
		return ErrNotSupported
	case errors.Is(err, syscall.EADDRINUSE):
		return ErrAddressInUse
	case errors.Is(err, syscall.EADDRNOTAVAIL), errors.Is(err, syscall.EAFNOSUPPORT):
		return ErrAddressInvalid
	case errors.Is(err, syscall.ECONNREFUSED):
		return ErrConnectionRefused
	case errors.Is(err, syscall.ECONNRESET):
		return ErrConnectionReset
	case errors.Is(err, syscall.ENOTCONN):
		return ErrNotConnected
	case errors.Is(err, syscall.ETIMEDOUT):
		return ErrTimedOut
	case errors.Is(err, syscall.EHOSTUNREACH):
		return ErrHostUnreachable
	case errors.Is(err, syscall.ENETUNREACH):
		return ErrNetworkUnreachable
	case errors.Is(err, syscall.EPROTONOSUPPORT), errors.Is(err, syscall.EPROTOTYPE):
		return ErrProtocol
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		if dns.IsTimeout {
			return ErrTimedOut
		}
		if dns.IsTemporary {
			return ErrAgain
		}
		if dns.IsNotFound {
			return ErrNoEntry
		}
		return ErrProtocol
	}
	return ErrIO
}

func zeroResults(results []uint64) {
	for i := range results {
		results[i] = 0
	}
}
