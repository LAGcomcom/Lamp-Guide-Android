//go:build windows

package agents

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

// AutostartEnabled reports whether the HKCU Run entry exists.
func AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	for _, name := range []string{"Lamp Guide", "Zen Gate"} {
		if v, _, err := k.GetStringValue(name); err == nil && v != "" {
			return true
		}
	}
	return false
}

// AutostartSet writes or removes the HKCU Run entry pointing at the current
// executable.
func AutostartSet(enable bool) error {
	if enable {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		k, err := registry.OpenKey(registry.CURRENT_USER,
			`Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		if err := k.SetStringValue("Lamp Guide", `"`+exe+`"`); err != nil {
			return err
		}
		if err := k.DeleteValue("Zen Gate"); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	// 关掉一个从来没开过的开关不是错误：Run 项不存在时 DeleteValue 返回
	// "The system cannot find the file specified"，把它当失败会让界面在
	// 本该安静的地方报一条红条，而这个动作的实际结果（未启用）已经达成。
	for _, name := range []string{"Lamp Guide", "Zen Gate"} {
		if err := k.DeleteValue(name); err != nil && err != registry.ErrNotExist {
			return err
		}
	}
	return nil
}
