//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW  = user32.NewProc("MessageBoxW")
)

func showStartupError(err error) {
	if err == nil {
		return
	}

	title, _ := syscall.UTF16PtrFromString("USBSync")
	text, _ := syscall.UTF16PtrFromString(fmt.Sprintf("程序没有成功打开。\r\n\r\n%s", err.Error()))
	procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(title)),
		0x10,
	)
}
