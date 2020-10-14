package helpers

import "math/rand"

const letterBytes = "abcdefghijklmnopqrstuvwxyz"

// RandStringBytes generates random string with defined length
func RandStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}