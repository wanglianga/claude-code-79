package main

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

func timeNowUnix() int64 {
	return time.Now().Unix()
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
