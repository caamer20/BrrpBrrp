package app

// FreeSpace reports the bytes a non-privileged writer can still put on the filesystem
// holding path.
//
// It is used as a guard, not as accounting: an operator's disk can be consumed by
// anything else on the host, and the only honest question an installation can ask before
// accepting an upload is "is there room for it and for what this server keeps in
// reserve". The answer is a moment old by the time it is acted on, which is why the
// write path also maps a full disk to a clear refusal rather than trusting this check
// alone.
//
// The implementation is per platform because the standard library has no portable call;
// see space_unix.go and space_windows.go.
