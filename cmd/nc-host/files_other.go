//go:build !linux && !darwin

package main

import "errors"

func freeBytes(string) (int64, error) { return 0, errors.New("unknown") }
func chownLike(string, string)        {}
