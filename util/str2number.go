package util

import (
	"math"
	"strconv"
	"strings"
)

func Str2Number[T Number, U ~int](s string, move U) T {
	a := strings.Split(s, "E")
	// 去除空白
	b := strings.Replace(a[0], " ", "", -1)
	f, err := strconv.ParseFloat(b, 64)
	if len(a) == 1 {
		n2 := math.Pow10(int(move))
		return T(f * n2)
	}
	n, err := strconv.ParseInt(a[1], 10, 64)
	if err != nil {
		panic(err)
	}
	n2 := math.Pow10(int(int64(move) + n))
	r := f * n2
	return T(r)
}
