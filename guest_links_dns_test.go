package facet

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCapabilitySafeLinkAndReadlink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	rights := RightPathOpen | RightPathLink | RightPathSymlink | RightPathReadlink | RightRead
	cfg := normalizeConfig(Config{Preopens: []Preopen{{Guest: "~", Host: root, Rights: rights}}})
	p := &Plugin{cfg: cfg, raw: newInstanceState(cfg)}
	m := testHostModule{}
	defer p.raw.closeAll()
	pre := make([]uint64, 2)
	p.preopenGetHost(m, []uint64{0}, pre)
	if pre[0] == 0 || int32(pre[1]) != ErrOK {
		t.Fatalf("preopen = %v", pre)
	}

	if code := p.symlinkDecoded(m, "target.txt", pre[0], "link.txt"); code != ErrOK {
		t.Fatalf("path_symlink code = %d", code)
	}
	if target, code := p.readlinkDecoded(m, pre[0], "link.txt"); code != ErrOK || target != "target.txt" {
		t.Fatalf("path_readlink = %q, %d", target, code)
	}
	if code := p.symlinkDecoded(m, "/etc/passwd", pre[0], "absolute"); code != ErrPermission {
		t.Fatalf("absolute symlink target code = %d, want ERR_PERMISSION", code)
	}
	if _, err := os.Lstat(filepath.Join(root, "absolute")); !os.IsNotExist(err) {
		t.Fatalf("absolute symlink unexpectedly created: %v", err)
	}

	if code := p.linkDecoded(m, pre[0], "target.txt", pre[0], "hard.txt", 0); code != ErrOK {
		t.Fatalf("path_link code = %d", code)
	}
	if got, err := os.ReadFile(filepath.Join(root, "hard.txt")); err != nil || string(got) != "target" {
		t.Fatalf("hard link contents = %q, err=%v", got, err)
	}

	// Following the final source symlink must link the securely resolved target,
	// not the symlink inode itself.
	if code := p.linkDecoded(m, pre[0], "link.txt", pre[0], "followed.txt", PathFollowSymlink); code != ErrOK {
		t.Fatalf("followed path_link code = %d", code)
	}
	if got, err := os.ReadFile(filepath.Join(root, "followed.txt")); err != nil || string(got) != "target" {
		t.Fatalf("followed hard link contents = %q, err=%v", got, err)
	}
}

func TestFollowedLinkWithoutProcFSIsNotSupported(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("facet"), 0o600); err != nil {
		t.Fatal(err)
	}
	srcFD, err := unix.Open(source, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(srcFD)
	dstParent, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dstParent)

	missingProc := filepath.Join(t.TempDir(), "missing-fd-directory")
	if code := linkFollowedFDAt(missingProc, srcFD, dstParent, "linked"); code != ErrNotSupported {
		t.Fatalf("followed link without procfs = %d, want ERR_NOT_SUPPORTED", code)
	}
	if _, err := os.Lstat(filepath.Join(root, "linked")); !os.IsNotExist(err) {
		t.Fatalf("followed link unexpectedly created: %v", err)
	}
}

func TestFollowedLinkRejectsFakeProcFS(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("facet"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	srcFD, err := unix.Open(source, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(srcFD)
	dstParent, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dstParent)

	fakeProc := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(fakeProc, strconv.Itoa(srcFD))); err != nil {
		t.Fatal(err)
	}
	if code := linkFollowedFDAt(fakeProc, srcFD, dstParent, "linked"); code != ErrNotSupported {
		t.Fatalf("followed link through fake procfs = %d, want ERR_NOT_SUPPORTED", code)
	}
	if _, err := os.Lstat(filepath.Join(root, "linked")); !os.IsNotExist(err) {
		t.Fatalf("followed link through fake procfs unexpectedly created: %v", err)
	}
}

func TestDNSLocalhostProducesFiniteResolverSequence(t *testing.T) {
	cfg := normalizeConfig(Config{})
	p := &Plugin{cfg: cfg, raw: newInstanceState(cfg)}
	m := testHostModule{}
	defer p.raw.closeAll()

	resolved := make([]uint64, 2)
	p.dnsResolveDecoded(m, "localhost", AFUnspec, 0, resolved)
	if resolved[0] == 0 || int32(resolved[1]) != ErrOK {
		t.Fatalf("dns_resolve localhost = %v", resolved)
	}
	handle := resolved[0]
	seen := 0
	for i := 0; i < 64; i++ {
		next := make([]uint64, 6)
		p.dnsNextHost(m, []uint64{handle}, next)
		if int32(next[5]) != ErrOK {
			t.Fatalf("dns_next = %v", next)
		}
		if next[4] != 0 {
			if seen == 0 {
				t.Fatal("dns resolver completed without an address")
			}
			return
		}
		family := int32(uint32(next[0]))
		if family != AFInet4 && family != AFInet6 {
			t.Fatalf("dns family = %d", family)
		}
		seen++
	}
	t.Fatal("dns resolver did not terminate within 64 addresses")
}
