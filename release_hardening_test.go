package facet

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	wago "github.com/wago-org/wago"
	"golang.org/x/sys/unix"
)

func TestNormalizeConfigPreservesZeroPreopenRights(t *testing.T) {
	cfg := normalizeConfig(Config{Preopens: []Preopen{{Guest: "/zero", Host: t.TempDir(), Rights: 0}}})
	if got := cfg.Preopens[0].Rights; got != 0 {
		t.Fatalf("explicit zero rights normalized to %#x", got)
	}
}

func TestPluginConfigDistinguishesOmittedAndEmptyRights(t *testing.T) {
	empty := []string{}
	cfg, err := configFromPlugin(pluginConfig{Preopens: []pluginPreopen{
		{Guest: "/default", Host: t.TempDir()},
		{Guest: "/zero", Host: t.TempDir(), Rights: &empty},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Preopens[0].Rights; got != defaultPreopenRights {
		t.Fatalf("omitted rights = %#x, want defaults %#x", got, defaultPreopenRights)
	}
	if got := cfg.Preopens[1].Rights; got != 0 {
		t.Fatalf("explicit [] rights = %#x, want zero", got)
	}
}

func TestPinnedPreopenSurvivesHostPathReplacement(t *testing.T) {
	root := t.TempDir()
	configured := filepath.Join(root, "cap")
	oldPath := filepath.Join(root, "old-cap")
	if err := os.Mkdir(configured, 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := normalizeConfig(Config{Preopens: []Preopen{{Guest: "/cap", Host: configured, Rights: RightStat}}})
	pinned, err := pinPreopens(cfg.Preopens)
	if err != nil {
		t.Fatal(err)
	}
	defer closePinnedPreopens(pinned)

	var original unix.Stat_t
	if err := unix.Fstat(pinned[0], &original); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(configured, oldPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(configured, 0o700); err != nil {
		t.Fatal(err)
	}
	var replacement unix.Stat_t
	if err := unix.Stat(configured, &replacement); err != nil {
		t.Fatal(err)
	}
	if replacement.Ino == original.Ino && replacement.Dev == original.Dev {
		t.Fatal("test setup did not replace the configured directory identity")
	}

	state := newInstanceState(cfg, pinned)
	defer state.closeAll()
	id, code := state.preopen(0)
	if code != ErrOK {
		t.Fatalf("preopen = %d", code)
	}
	h, code := state.get(id)
	if code != ErrOK || h.file == nil {
		t.Fatalf("preopen handle = %#v, code %d", h, code)
	}
	var duplicated unix.Stat_t
	if err := unix.Fstat(h.file.fd, &duplicated); err != nil {
		t.Fatal(err)
	}
	if duplicated.Ino != original.Ino || duplicated.Dev != original.Dev {
		t.Fatalf("preopen moved from pinned identity dev=%d ino=%d to dev=%d ino=%d", original.Dev, original.Ino, duplicated.Dev, duplicated.Ino)
	}
}

func TestDirectorySnapshotMetadataIsDescriptorRelative(t *testing.T) {
	dir := t.TempDir()
	entryPath := filepath.Join(dir, "entry")
	if err := os.WriteFile(entryPath, []byte("facet"), 0o600); err != nil {
		t.Fatal(err)
	}
	var expected unix.Stat_t
	if err := unix.Lstat(entryPath, &expected); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	iter := &dirIterator{file: file}
	defer iter.close()

	snap, code := snapshotDirEntry(iter)
	if code != ErrOK || snap == nil {
		t.Fatalf("snapshot = %#v, code %d", snap, code)
	}
	if snap.name != "entry" {
		t.Fatalf("entry name = %q", snap.name)
	}
	if snap.inode != expected.Ino {
		t.Fatalf("entry inode = %d, want descriptor-relative inode %d", snap.inode, expected.Ino)
	}
	if snap.kind != FileTypeRegular {
		t.Fatalf("entry type = %d, want regular", snap.kind)
	}
}

func TestPathEXDEVClassification(t *testing.T) {
	if got := resolvePathCode(syscall.EXDEV); got != ErrPermission {
		t.Fatalf("openat2 EXDEV = %d, want ERR_PERMISSION", got)
	}
	if got := pathCode(syscall.EXDEV); got != ErrOther {
		t.Fatalf("ordinary path EXDEV = %d, want ERR_OTHER", got)
	}
}

func TestGuestAllocationBudgets(t *testing.T) {
	if _, code := addVectorBudget(maxVectorBytes, 1); code != ErrQuota {
		t.Fatalf("vector budget overflow = %d, want ERR_QUOTA", code)
	}
	if maxIOVecs == 0 || maxTextUnits == 0 || maxPollSubscriptions == 0 {
		t.Fatal("resource budgets must be finite and nonzero")
	}
}

func TestPollTimeoutMillisDoesNotOverflow(t *testing.T) {
	if got := pollTimeoutMillis(0, math.MaxUint64-1); got != math.MaxInt32 {
		t.Fatalf("max deadline timeout = %d, want %d", got, math.MaxInt32)
	}
	if got := pollTimeoutMillis(math.MaxUint64-2, math.MaxUint64-1); got != 1 {
		t.Fatalf("one-nanosecond deadline timeout = %d, want 1", got)
	}
}

func TestDNSNameRequiresASCIIWithoutNUL(t *testing.T) {
	for _, valid := range []string{"example.com", "xn--bcher-kva.example", "localhost"} {
		if code := validateDNSName(valid); code != ErrOK {
			t.Fatalf("valid DNS name %q = %d", valid, code)
		}
	}
	for _, invalid := range []string{"", "nul\x00name", "bücher.example"} {
		if code := validateDNSName(invalid); code != ErrInvalid {
			t.Fatalf("invalid DNS name %q = %d, want ERR_INVALID", invalid, code)
		}
	}
}

func TestDNSLookupBudgetIsFinite(t *testing.T) {
	if dnsLookupTimeout <= 0 {
		t.Fatalf("DNS lookup timeout must be finite and positive, got %v", dnsLookupTimeout)
	}
}

func TestPortableErrnoMappings(t *testing.T) {
	cases := []struct {
		err  error
		want int32
	}{
		{context.Canceled, ErrCanceled},
		{context.DeadlineExceeded, ErrTimedOut},
		{syscall.EIO, ErrIO},
		{syscall.ENOMEM, ErrNoMemory},
		{syscall.EOVERFLOW, ErrOverflow},
		{syscall.ECANCELED, ErrCanceled},
		{syscall.EDQUOT, ErrQuota},
	}
	for _, tc := range cases {
		if got := errorCode(tc.err); got != tc.want {
			t.Errorf("errorCode(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}

func TestReleaseFilesystemFlagRules(t *testing.T) {
	if _, code := openFlagsToUnix(OpenExclusive, RightRead); code != ErrInvalid {
		t.Fatalf("exclusive without create = %d", code)
	}
	if _, code := openFlagsToUnix(OpenTruncate, RightRead); code != ErrCapability {
		t.Fatalf("truncate without write = %d", code)
	}
	if _, code := openFlagsToUnix(OpenAppend, RightRead); code != ErrCapability {
		t.Fatalf("append without write = %d", code)
	}
	if _, code := openFlagsToUnix(OpenDirectory|OpenCreate, RightWrite); code != ErrInvalid {
		t.Fatalf("directory+create = %d", code)
	}
}

func TestFacetRequiresInstantiationSignatureValidation(t *testing.T) {
	for _, req := range Definition().Authorities {
		if req.Name != wago.AuthorityInstanceInstantiateIntercept {
			continue
		}
		if req.Mode != wago.AuthorityRequired {
			t.Fatalf("instantiation interceptor authority mode = %v, want required", req.Mode)
		}
		return
	}
	t.Fatal("Facet definition does not request the instantiation interceptor")
}

func TestCanonicalFacetArrayParameterShape(t *testing.T) {
	valid := wago.ValueTypeDescriptor{
		Kind: wago.ValueTypeReference,
		Ref: wago.ReferenceTypeDescriptor{
			Heap: wago.HeapTypeDescriptor{Abstract: wago.AbstractHeapArray},
		},
	}
	if !canonicalFacetArrayParameter(valid) {
		t.Fatal("canonical non-null abstract array reference was rejected")
	}
	invalid := []wago.ValueTypeDescriptor{
		{Kind: wago.ValueTypeReference, Ref: wago.ReferenceTypeDescriptor{Nullable: true, Heap: wago.HeapTypeDescriptor{Abstract: wago.AbstractHeapArray}}},
		{Kind: wago.ValueTypeReference, Ref: wago.ReferenceTypeDescriptor{Exact: true, Heap: wago.HeapTypeDescriptor{Abstract: wago.AbstractHeapArray}}},
		{Kind: wago.ValueTypeReference, Ref: wago.ReferenceTypeDescriptor{Heap: wago.HeapTypeDescriptor{Abstract: wago.AbstractHeapAny}}},
		{Kind: wago.ValueTypeReference, Ref: wago.ReferenceTypeDescriptor{Heap: wago.HeapTypeDescriptor{Defined: true, TypeIndex: 0}}},
		{Kind: wago.ValueTypeI32},
	}
	for i, typ := range invalid {
		if canonicalFacetArrayParameter(typ) {
			t.Fatalf("non-canonical array parameter shape %d was accepted: %#v", i, typ)
		}
	}
}

func TestPoisonedPositionalFDIsRemovedFromPollSets(t *testing.T) {
	fd, err := unix.Open(filepath.Join(t.TempDir(), "poisoned"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	state := newInstanceState(Config{})
	defer state.closeAll()
	fileID, code := state.alloc(&handleEntry{kind: handleFile, rights: RightWrite, flags: FDAppend, file: &fileResource{fd: fd}})
	if code != ErrOK {
		_ = unix.Close(fd)
		t.Fatalf("allocate file handle = %d", code)
	}
	poll := newPollSet()
	pollID, code := state.alloc(&handleEntry{kind: handlePoll, poll: poll})
	if code != ErrOK {
		t.Fatalf("allocate poll handle = %d", code)
	}
	_ = pollID
	poll.regs[fileID] = pollRegistration{events: PollWritable}

	p := &Plugin{raw: state}
	transferred, code := p.withPositionalFD(nil, uint64(fileID), fdIOWrite, func(h *handleEntry) (uint64, int32) {
		_ = h.file.close()
		h.kind = 0 // model pwriteExplicitOffset restore failure after a partial write
		return 1, ErrOK
	})
	if transferred != 1 || code != ErrOK {
		t.Fatalf("poisoned positional result = (%d, %d), want (1, ERR_OK)", transferred, code)
	}
	if _, ok := state.handles[fileID]; ok {
		t.Fatal("poisoned positional descriptor remained in the handle table")
	}
	if _, ok := poll.regs[fileID]; ok {
		t.Fatal("poisoned positional descriptor remained registered in a poll set")
	}
}
