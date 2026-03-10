package utils

import "log"

// SafeGo 拦截 panic 的 goroutine
func SafeGo(fn func()) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[SafeGo] panic recovered: %v", err)
			}
		}()
		fn()
	}()
}
