//go:build !darwin || !cgo

package main

import "unsafe"

func configureMacWindow(_ unsafe.Pointer) {}
