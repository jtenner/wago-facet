package facet

// Host resource budgets cap allocations selected directly by guest-controlled
// counts or lengths. These are implementation limits, not Facet ABI constants.
// Exceeding one returns ERR_QUOTA before the corresponding host allocation.
const (
	maxIOVecs            uint32 = 1024
	maxVectorBytes       uint64 = 16 << 20
	maxTextUnits         uint64 = 1 << 20
	maxPollSubscriptions        = 4096
)
