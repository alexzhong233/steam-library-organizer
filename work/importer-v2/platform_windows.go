//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

func regValue(path, name string) ([]byte, uint32, error) {
	p, _ := syscall.UTF16PtrFromString(path)
	var h syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, p, 0, syscall.KEY_READ, &h); err != nil {
		return nil, 0, err
	}
	defer syscall.RegCloseKey(h)
	n, _ := syscall.UTF16PtrFromString(name)
	var typ, size uint32
	if err := syscall.RegQueryValueEx(h, n, nil, &typ, nil, &size); err != nil {
		return nil, typ, err
	}
	if size == 0 || size > 65536 {
		return nil, typ, fmt.Errorf("无效注册表长度")
	}
	b := make([]byte, size)
	err := syscall.RegQueryValueEx(h, n, nil, &typ, &b[0], &size)
	return b[:size], typ, err
}
func currentAccount() (string, error) {
	b, t, e := regValue(`Software\Valve\Steam\ActiveProcess`, "ActiveUser")
	if e != nil {
		return "", fmt.Errorf("请先登录 Steam：%w", e)
	}
	if t != syscall.REG_DWORD || len(b) != 4 {
		return "", fmt.Errorf("Steam ActiveUser 格式不支持")
	}
	n := binary.LittleEndian.Uint32(b)
	if n == 0 {
		return "", fmt.Errorf("Steam 尚未登录")
	}
	return strconv.FormatUint(uint64(n), 10), nil
}
func steamRoot() (string, error) {
	b, t, e := regValue(`Software\Valve\Steam`, "SteamPath")
	if e != nil {
		return "", e
	}
	if t != syscall.REG_SZ || len(b)%2 != 0 {
		return "", fmt.Errorf("SteamPath 格式不支持")
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return filepath.FromSlash(syscall.UTF16ToString(u)), nil
}
func initConsole() {
	k := syscall.NewLazyDLL("kernel32.dll")
	k.NewProc("SetConsoleOutputCP").Call(65001)
	k.NewProc("SetConsoleCP").Call(65001)
}
func acquireLock() (func(), error) {
	k := syscall.NewLazyDLL("kernel32.dll")
	p, _ := syscall.UTF16PtrFromString(`Local\SteamCollectionImporter-5952-v1`)
	h, _, e := k.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(p)))
	if h == 0 {
		return nil, e
	}
	if e == syscall.ERROR_ALREADY_EXISTS {
		syscall.CloseHandle(syscall.Handle(h))
		return nil, fmt.Errorf("另一个整理工具正在运行，请先关闭它")
	}
	return func() { syscall.CloseHandle(syscall.Handle(h)) }, nil
}
func steamRunning() (bool, error) {
	h, e := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if e != nil {
		return false, e
	}
	defer syscall.CloseHandle(h)
	var p syscall.ProcessEntry32
	p.Size = uint32(unsafe.Sizeof(p))
	e = syscall.Process32First(h, &p)
	for e == nil {
		if strings.EqualFold(syscall.UTF16ToString(p.ExeFile[:]), "steam.exe") {
			return true, nil
		}
		e = syscall.Process32Next(h, &p)
	}
	if e != syscall.ERROR_NO_MORE_FILES {
		return false, e
	}
	return false, nil
}
func launchSteam(root string, debug bool, port int) error {
	args := []string{}
	if debug {
		args = []string{"-cef-enable-debugging", fmt.Sprintf("-devtools-port=%d", port)}
	}
	c := exec.Command(filepath.Join(root, "steam.exe"), args...)
	c.Dir = root
	if e := c.Start(); e != nil {
		return e
	}
	return c.Process.Release()
}
func requestShutdown(root string) error {
	c := exec.Command(filepath.Join(root, "steam.exe"), "-shutdown")
	if e := c.Start(); e != nil {
		return e
	}
	return c.Process.Release()
}
func openFolder(p string) error { return exec.Command("explorer.exe", p).Start() }
func platformCheck() error      { return nil }
