package backtest

import (
	"os"
	"testing"
)

// The guard has to answer for a real filesystem, or it would suspend
// downloading on every case and quietly degrade a whole run.
func TestFreeBytesReadsARealFilesystem(t *testing.T) {
	got, ok := freeBytes(os.TempDir())
	if !ok {
		t.Skip("cannot stat the temp filesystem")
	}
	if got == 0 {
		t.Error("free space reported as exactly zero on a working filesystem")
	}
}

// An unstattable path must read as "carry on". A backtest that refused to run
// because it could not stat a filesystem would be worse than one that
// occasionally fills a disk.
func TestUnknownFilesystemDoesNotSuspendDownloads(t *testing.T) {
	if _, ok := freeBytes("/definitely/not/a/path/anywhere"); ok {
		t.Fatal("stat succeeded on a nonexistent path")
	}
	if diskIsTight("/definitely/not/a/path/anywhere") {
		t.Error("an unstattable path was treated as a full disk")
	}
}

// On a machine with room, downloading must not be suspended.
func TestRoomyDiskIsNotTight(t *testing.T) {
	free, ok := freeBytes(os.TempDir())
	if !ok {
		t.Skip("cannot stat the temp filesystem")
	}
	if free < MinFreeBytes {
		t.Skipf("this machine has %d bytes free, below the %d threshold", free, MinFreeBytes)
	}
	if diskIsTight(os.TempDir()) {
		t.Error("a filesystem above the threshold was reported as tight")
	}
}

// The threshold has to leave room for what a case still needs once downloading
// stops: a worktree, an index database and a type-check.
func TestThresholdLeavesRoomForACase(t *testing.T) {
	if MinFreeBytes < 1<<30 {
		t.Errorf("MinFreeBytes = %d, too small to hold a worktree and an index", MinFreeBytes)
	}
	if MinFreeBytes > 64<<30 {
		t.Errorf("MinFreeBytes = %d, large enough to suspend downloads on a healthy machine", MinFreeBytes)
	}
}
