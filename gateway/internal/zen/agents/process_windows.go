//go:build windows

package agents

import (
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processRunningAny reports whether any of the named applications is running.
// Windows is matched by image name, so "ZCode" is looked up as "ZCode.exe".
//
// The lookup is a Toolhelp32 snapshot taken in-process. It used to shell out to
// `tasklist /FO CSV` once per name, and that subprocess costs ~530ms on a warm
// machine: five running apps turned one dashboard poll into 2.7 seconds, which
// is exactly the wait the window showed before its cards appeared.
func processRunningAny(names []string) bool {
	if len(names) == 0 {
		return false
	}
	live := runningImages()
	for _, n := range names {
		base := strings.ToLower(strings.TrimSuffix(n, ".exe"))
		if live[base] {
			return true
		}
	}
	return false
}

// running returns every running image name, lowercased and without ".exe".
//
// A snapshot costs ~1ms but walking 200+ processes for five unrelated names
// would still be five walks; the shared map removes that, and the short TTL
// keeps a page that polls every few hundred milliseconds from re-walking.
var (
	imagesMu    sync.Mutex
	imagesAt    time.Time
	imagesCache map[string]bool
)

const imageCacheTTL = 2 * time.Second

func runningImages() map[string]bool {
	imagesMu.Lock()
	defer imagesMu.Unlock()
	if imagesCache != nil && time.Since(imagesAt) < imageCacheTTL {
		return imagesCache
	}
	out := map[string]bool{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return imagesCache
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		name := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
		name = strings.TrimSuffix(name, ".exe")
		if name != "" {
			out[name] = true
		}
	}
	imagesCache = out
	imagesAt = time.Now()
	return out
}
