//go:build windows

package main

import "syscall"

const utf8CodePage = 65001

func init() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleCP := kernel32.NewProc("SetConsoleCP")
	setConsoleOutputCP := kernel32.NewProc("SetConsoleOutputCP")

	_, _, _ = setConsoleCP.Call(utf8CodePage)
	_, _, _ = setConsoleOutputCP.Call(utf8CodePage)
}
