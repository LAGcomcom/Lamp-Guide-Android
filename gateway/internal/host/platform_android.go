//go:build android

// platform_android.go supplies the four native actions the desktop host gets
// from Win32/COM. Android has no shell32 and no IFileDialog: the app owns
// both. SAF (Storage Access Framework) can only start an Activity from the
// UI thread, so the Kotlin side does the picking and hands the resolved
// absolute path back here through the hooks below.
//
// A nil hook is a real answer, not a silent success: the page shows the same
// "无法打开" info bar it would show on a host that cannot do the thing.
package host

import "errors"

var (
	// AndroidPickFile asks the app to run ACTION_OPEN_DOCUMENT. It returns the
	// local path of the copied file, canceled=true when the user backed out.
	AndroidPickFile func(prompt string) (path string, canceled bool, err error)
	// AndroidPickSave is ACTION_CREATE_DOCUMENT: the app writes the bytes after
	// the gateway hands them over, so it returns the destination it created.
	AndroidPickSave func(prompt, defaultName string) (path string, canceled bool, err error)
	// AndroidOpenShell reveals a directory: Android has no Explorer, so the app
	// opens the folder in a file manager (or reports that none is installed).
	AndroidOpenShell func(path string) error
	// AndroidOpenURL hands a link to the system browser or the matching app.
	AndroidOpenURL func(url string) error
)

var errNoHost = errors.New("安卓宿主尚未连接")

// PickFile is the open dialog. See AndroidPickFile.
func PickFile(prompt string, extensions []string) (string, bool, error) {
	if AndroidPickFile == nil {
		return "", true, errNoHost
	}
	return AndroidPickFile(prompt)
}

// PickSaveFile is the save dialog. See AndroidPickSave.
func PickSaveFile(prompt, defaultName string) (string, bool, error) {
	if AndroidPickSave == nil {
		return "", true, errNoHost
	}
	return AndroidPickSave(prompt, defaultName)
}

// OpenInShell reveals a path the way Explorer would.
func OpenInShell(path string) error {
	if AndroidOpenShell == nil {
		return errNoHost
	}
	return AndroidOpenShell(path)
}

// OpenURL hands a link to the OS.
func OpenURL(url string) error {
	if AndroidOpenURL == nil {
		return errNoHost
	}
	return AndroidOpenURL(url)
}
