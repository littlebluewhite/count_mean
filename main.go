package main

import (
	"count_mean/util"
	"encoding/csv"
	"fmt"
	"math"
	"os"
)

func main() {
	var file string
	fmt.Print("請輸入檔名: ")
	fmt.Scanln(&file)
	f, err := os.Open(file + ".csv")
	if err != nil {
		panic(err)
	}
	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		panic(err)
	}
	l := len(records)
	maxMean := 0
	from := 0
	move := 10
	for {
		fmt.Print("選取的欄位(輸入數字): ")
		var columns int
		fmt.Scanln(&columns)
		if columns < 2 {
			panic("輸入錯誤")
		}
		if columns > len(records[0]) {
			panic("超出範圍！")
		}
		columns -= 1
		fmt.Printf("你選取的欄位是: %s\n", records[0][columns])

		fmt.Print("多少資料的平均(輸入數字): ")
		var n int
		fmt.Scanln(&n)
		if l-1 < n || n < 1 {
			panic("不要亂輸入! ^^")
		}
		for i := 1; i <= l-n; i++ {
			numbers := make([]float64, 0, n)
			for j := i; j < i+n; j++ {
				numbers = append(numbers, float64(util.Str2int(records[j][columns], int64(move))))
			}
			m := int(util.ArrayMean(numbers))
			if m > maxMean {
				maxMean = m
				from = i
			}
		}
		fmt.Printf("從 %v 到 %v 有最大平均值: %.10f\n", records[from][0], records[from+n][0], float64(maxMean)/math.Pow10(move))
	}
}
