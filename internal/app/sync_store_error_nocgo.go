//go:build !cgo

package app

func isRetryableStoreError(error) bool { return false }
