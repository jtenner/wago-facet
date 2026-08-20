package facet

const (
	ErrOK                 int32 = 0
	ErrPermission         int32 = 1
	ErrNoEntry            int32 = 2
	ErrIO                 int32 = 3
	ErrBadHandle          int32 = 4
	ErrAgain              int32 = 5
	ErrNoMemory           int32 = 6
	ErrAccess             int32 = 7
	ErrBusy               int32 = 8
	ErrExists             int32 = 9
	ErrNotDirectory       int32 = 10
	ErrIsDirectory        int32 = 11
	ErrInvalid            int32 = 12
	ErrFileTooLarge       int32 = 13
	ErrNoSpace            int32 = 14
	ErrReadOnly           int32 = 15
	ErrPipe               int32 = 16
	ErrRange              int32 = 17
	ErrNotEmpty           int32 = 18
	ErrLoop               int32 = 19
	ErrNameTooLong        int32 = 20
	ErrNotSupported       int32 = 21
	ErrOverflow           int32 = 22
	ErrIllegalSequence    int32 = 23
	ErrFault              int32 = 24
	ErrType               int32 = 25
	ErrQuota              int32 = 26
	ErrCanceled           int32 = 27
	ErrAddressInUse       int32 = 28
	ErrAddressInvalid     int32 = 29
	ErrConnectionRefused  int32 = 30
	ErrConnectionReset    int32 = 31
	ErrNotConnected       int32 = 32
	ErrTimedOut           int32 = 33
	ErrHostUnreachable    int32 = 34
	ErrNetworkUnreachable int32 = 35
	ErrProtocol           int32 = 36
	ErrCapability         int32 = 37
	ErrEnd                int32 = 38
	ErrOther              int32 = 255
)

const (
	RightRead uint64 = 1 << iota
	RightWrite
	RightSeek
	RightTell
	RightStat
	RightSetSize
	RightSync
)

const (
	RightPathOpen uint64 = 1 << (16 + iota)
	RightPathCreate
	RightPathRemove
	RightPathRename
	RightPathLink
	RightPathSymlink
	RightPathReadlink
	RightDirIterate
)

const (
	FileTypeUnknown int32 = iota
	FileTypeRegular
	FileTypeDirectory
	FileTypeSymlink
	FileTypeChar
	FileTypeBlock
	FileTypeSocket
	FileTypeFIFO
)

const (
	FDAppend   uint32 = 1 << 0
	FDNonblock uint32 = 1 << 1
)

const (
	OpenCreate uint32 = 1 << iota
	OpenExclusive
	OpenTruncate
	OpenDirectory
	OpenNoFollow
	OpenAppend
	OpenNonblock
)

const PathFollowSymlink uint32 = 1 << 0

const (
	RemoveFile uint32 = 1 << iota
	RemoveDirectory
)

const (
	RenameReplace uint32 = 1 << iota
	RenameNoReplace
	RenameExchange
)

const (
	SeekSet int32 = iota
	SeekCur
	SeekEnd
)

const (
	StatHasATime uint32 = 1 << iota
	StatHasMTime
	StatHasCTime
)

const (
	AFUnspec int32 = iota
	AFInet4
	AFInet6
)

const (
	SockStream int32 = 1
	SockDgram  int32 = 2
)

const (
	ProtoDefault int32 = iota
	ProtoTCP
	ProtoUDP
)

const SockNonblock uint32 = 1 << 0

const (
	ShutRD int32 = 1 + iota
	ShutWR
	ShutRDWR
)

const (
	PollReadable uint32 = 1 << iota
	PollWritable
	PollHangup
	PollError
	PollTimer
)

const (
	PollSourceFD int32 = 1 + iota
	PollSourceTimer
)

const (
	EnvName int32 = iota
	EnvValue
)

const defaultPreopenRights = RightStat | RightPathOpen | RightDirIterate
