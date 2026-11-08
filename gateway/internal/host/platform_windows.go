//go:build windows

package host

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32DLL   = syscall.NewLazyDLL("ole32.dll")
	shell32DLL = syscall.NewLazyDLL("shell32.dll")

	procCoCreateInstance = ole32DLL.NewProc("CoCreateInstance")
	procOleInitialize    = ole32DLL.NewProc("OleInitialize")
	procOleUninitialize  = ole32DLL.NewProc("OleUninitialize")
	procShellExecuteW    = shell32DLL.NewProc("ShellExecuteW")
)

const (
	clsctxInprocServer = 0x1
	sigdnFileSysPath   = 0x80058000

	fosForceFileSystem = 0x00000040
	fosPathMustExist   = 0x00000800
	fosFileMustExist   = 0x00001000

	swShowNormal = 1

	// HRESULT_FROM_WIN32(ERROR_CANCELLED): the user closed the dialog.
	hrCanceled = 0x800704C7
)

// IFileDialog vtable slots, counted over IUnknown (0..2) and IModalWindow (3).
// Naming them keeps every call site free of bare integers.
const (
	vtShow             = 3
	vtSetOptions       = 9
	vtGetOptions       = 10
	vtSetFileTypes     = 4
	vtSetTitle         = 17
	vtSetOkButtonLabel = 18
	vtGetResult        = 20
)

// IShellItem::GetDisplayName.
const vtGetDisplayName = 5

var (
	// CLSID_FileOpenDialog {DC1C5A9C-E88A-4DDE-A5A1-60F82A20AEF7}
	clsidFileOpenDialog = windows.GUID{
		Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE,
		Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7},
	}
	// IID_IFileOpenDialog {D57C7288-D4AD-4768-BE02-9D969532D960}
	iidFileOpenDialog = windows.GUID{
		Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768,
		Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60},
	}
	// IID_IFileSaveDialog {C0A21332-1F76-4F4C-BE6F-D3A8B0F0AE9F}
	iidFileSaveDialog = windows.GUID{
		Data1: 0xC0A21332, Data2: 0x1F76, Data3: 0x4F4C,
		Data4: [8]byte{0xBE, 0x6F, 0xD3, 0xA8, 0xB0, 0xF0, 0xAE, 0x9F},
	}
)

// com is a raw COM interface pointer.
//
// A struct rather than a bare unsafe.Pointer: Go does not allow a method receiver
// whose underlying type is a pointer, so the pointer is wrapped and the methods
// hang off the wrapper.
type com struct {
	ptr unsafe.Pointer
}

// call invokes vtable slot idx with up to eight arguments.
func (p com) call(idx int, args ...uintptr) uintptr {
	if p.ptr == nil {
		return 0x80004003 // E_POINTER
	}
	vtbl := *(*unsafe.Pointer)(p.ptr)
	slot := unsafe.Add(vtbl, uintptr(idx)*unsafe.Sizeof(uintptr(0)))
	proc := *(*uintptr)(slot)

	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(p.ptr))
	all = append(all, args...)
	ret, _, _ := syscall.SyscallN(proc, all...)
	return ret
}

// release drops one reference; slot 2 is IUnknown::Release.
func (p com) release() {
	if p.ptr != nil {
		p.call(2)
	}
}

// isValid reports whether the interface pointer was created.
func (p com) isValid() bool { return p.ptr != nil }

// filterSpec mirrors COMDLG_FILTERSPEC.
type filterSpec struct {
	name *uint16
	spec *uint16
}

// PickFile opens the native open-file picker restricted to the given
// extensions. An empty extension list means "all files".
func PickFile(prompt string, extensions []string) (string, bool, error) {
	return openDialog(prompt, extensions, false)
}

// PickSaveFile opens the native save-file picker, starting from defaultName.
func PickSaveFile(prompt, defaultName string) (string, bool, error) {
	return openDialog(prompt, nil, true)
}

// openDialog is the shared IFileDialog path. save picks IFileSaveDialog, which
// is the same interface family with a different CLSID and IID.
//
// OleInitialize rather than CoInitializeEx: the shell dialogs want OLE's
// message filtering. It is a no-op on a thread that is already initialized,
// which is the normal case on the WebView2 UI thread; the matching
// OleUninitialize is skipped when someone else owned the apartment.
func openDialog(prompt string, extensions []string, save bool) (string, bool, error) {
	// The dialog pumps messages on this thread, so it has to stay put.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procOleInitialize.Call(0)
	ownsApartment := hr == 0 || hr == 1
	if ownsApartment {
		defer procOleUninitialize.Call()
	}

	iid := &iidFileOpenDialog
	if save {
		iid = &iidFileSaveDialog
	}

	var dialog com
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&dialog.ptr)),
	)
	if hr != 0 || !dialog.isValid() {
		return "", false, fmt.Errorf("无法创建文件对话框（HRESULT 0x%X）", hr)
	}
	defer dialog.release()

	// Only real filesystem paths, so the reply can be stored and reopened.
	var opts uint32
	dialog.call(vtGetOptions, uintptr(unsafe.Pointer(&opts)))
	opts |= fosForceFileSystem
	if save {
		opts |= fosPathMustExist
	} else {
		opts |= fosFileMustExist
	}
	dialog.call(vtSetOptions, uintptr(opts))

	if prompt != "" {
		if title, err := windows.UTF16PtrFromString(prompt); err == nil {
			dialog.call(vtSetTitle, uintptr(unsafe.Pointer(title)))
		}
	}
	if label, err := windows.UTF16PtrFromString("选择"); err == nil {
		dialog.call(vtSetOkButtonLabel, uintptr(unsafe.Pointer(label)))
	}

	if len(extensions) > 0 {
		if err := setFileTypes(dialog, extensions); err != nil {
			return "", false, err
		}
	}

	hr = dialog.call(vtShow, 0)
	if hr == hrCanceled {
		return "", true, nil
	}
	if hr != 0 {
		return "", false, fmt.Errorf("文件对话框返回错误（HRESULT 0x%X）", hr)
	}

	var item com
	hr = dialog.call(vtGetResult, uintptr(unsafe.Pointer(&item.ptr)))
	if hr != 0 || !item.isValid() {
		return "", true, nil
	}
	defer item.release()

	var name *uint16
	hr = item.call(vtGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&name)))
	if hr != 0 || name == nil {
		return "", false, fmt.Errorf("无法读取所选路径（HRESULT 0x%X）", hr)
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(name))

	return windows.UTF16PtrToString(name), false, nil
}

// setFileTypes installs the filter list ("EXE 文件" / "*.exe").
//
// The UTF-16 buffers must outlive the COM call: pinned keeps every pointer
// reachable until the KeepAlive at the end.
func setFileTypes(dialog com, extensions []string) error {
	var pinned []*uint16
	keep := func(s string) (*uint16, error) {
		p, err := windows.UTF16PtrFromString(s)
		if err != nil {
			return nil, err
		}
		pinned = append(pinned, p)
		return p, nil
	}

	specs := make([]filterSpec, 0, len(extensions)+1)
	for _, ext := range extensions {
		ext = strings.TrimSpace(strings.TrimPrefix(ext, "."))
		if ext == "" {
			continue
		}
		name, err := keep(strings.ToUpper(ext) + " 文件")
		if err != nil {
			return err
		}
		spec, err := keep("*." + ext)
		if err != nil {
			return err
		}
		specs = append(specs, filterSpec{name: name, spec: spec})
	}
	if len(specs) == 0 {
		return nil
	}
	allName, err := keep("所有文件")
	if err != nil {
		return err
	}
	allSpec, err := keep("*.*")
	if err != nil {
		return err
	}
	specs = append(specs, filterSpec{name: allName, spec: allSpec})

	dialog.call(vtSetFileTypes, uintptr(len(specs)), uintptr(unsafe.Pointer(&specs[0])))
	runtime.KeepAlive(pinned)
	return nil
}

// OpenInShell reveals a filesystem path through Explorer.
func OpenInShell(path string) error { return shellExecute(path, swShowNormal) }

// OpenURL hands an http(s) address to the default browser.
func OpenURL(url string) error { return shellExecute(url, swShowNormal) }

func shellExecute(target string, show int) error {
	t, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	// ShellExecuteW returns a value <= 32 on failure.
	ret, _, _ := procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(t)),
		0, 0, uintptr(show),
	)
	if ret <= 32 {
		return fmt.Errorf("ShellExecute 失败（返回值 %d）", int(ret))
	}
	return nil
}

var _ = syscall.SyscallN
