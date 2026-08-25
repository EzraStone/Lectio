package backtest

import "syscall"

// MinFreeBytes is the free space below which module downloading is suspended.
//
// A backtest calls `go mod download all` once per case, and a corpus of thirty
// large repositories at a dozen cases each pulls tens of gigabytes into the
// shared module cache. One run filled a 252 GB disk and then failed 162 of 245
// cases — the summary reported them as discards, the strategy table underneath
// looked like a result, and nothing said the machine had run out of room.
//
// Four gigabytes is enough headroom for a worktree, an index database and a
// type-check, which is what a case needs once downloading stops.
const MinFreeBytes = 4 << 30

// freeBytes reports available space on the filesystem holding path.
//
// Returns 0 and false when it cannot tell, and every caller treats that as
// "carry on" — a backtest that refused to run because it could not stat a
// filesystem would be worse than one that occasionally fills a disk.
func freeBytes(path string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}

// diskIsTight reports whether space has run low enough to stop fetching
// modules.
func diskIsTight(path string) bool {
	free, ok := freeBytes(path)
	return ok && free < MinFreeBytes
}
