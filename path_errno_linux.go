package facet

// pathCode is retained for ordinary descriptor-relative filesystem operations.
// Capability-beneath openat2 resolution uses resolvePathCode instead because
// Linux reserves EXDEV there to signal a RESOLVE_BENEATH escape.
func pathCode(err error) int32 {
	return errorCode(err)
}
