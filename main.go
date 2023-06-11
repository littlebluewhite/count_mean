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
	var columns int
	var n int
	for {
		maxMean := 0
		from := 0
		move := 10
		columns = 0
		n = 0

		fmt.Print("選取的欄位(輸入數字): ")
		fmt.Scanln(&columns)
		if columns < 2 {
			fmt.Println("輸入錯誤")
			continue
		}
		if columns > len(records[0]) {
			fmt.Println("超出範圍！")
			continue
		}
		columns -= 1
		fmt.Printf("你選取的欄位是: %s\n", records[0][columns])

		fmt.Print("多少資料的平均(輸入數字): ")
		fmt.Scanln(&n)
		if l-1 < n || n < 1 {
			fmt.Println("不要亂輸入! ^^")
			continue
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
		fmt.Printf("%v\n%v\n%.10f\n", records[from][0], records[from+n-1][0], float64(maxMean)/math.Pow10(move))
	}
}
