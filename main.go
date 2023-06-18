package main

import (
	"count_mean/util"
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"os"
	"time"
)

func main() {
	var file string
	fmt.Print("請輸入載入檔名: ")
	fmt.Scanln(&file)
	f, err := os.Open(file + ".csv")
	defer func(f *os.File) {
		e := f.Close()
		if e != nil {

		}
	}(f)
	if err != nil {
		panic(err)
	}
	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		panic(err)
	}
	var fn int
	fmt.Print("1. 某幾筆數平均最大值\n2. 每一行同除一個值\n3. 分期處理\n選擇功能(輸入數字): ")
	fmt.Scanln(&fn)
	switch fn {
	case 1:
		fn1(records)
	case 2:
		fn2(records)
	case 3:
		fn3(records)
	}
}

func fn1(r [][]string) {
	l := len(r)
	columnMax := len(r[0])
	var n int
	fmt.Print("多少資料的平均(輸入數字): ")
	fmt.Scanln(&n)
	if l-1 < n || n < 1 {
		fmt.Println("不要亂輸入! ^^")
		time.Sleep(5 * time.Second)
	}
	result := make([][]string, 0, 4)
	result = append(result, r[0])
	count := make(map[int][]string)
	for i := 1; i < columnMax; i++ {
		maxMean := 0
		from := 0
		move := 10
		for j := 1; j <= l-n; j++ {
			numbers := make([]float64, 0, n)
			for k := j; k < j+n; k++ {
				numbers = append(numbers, util.Str2Number[float64, int](r[k][i], move))
			}
			m := int(util.ArrayMean(numbers))
			if m > maxMean {
				maxMean = m
				from = j
			}
		}
		count[i] = []string{r[from][0], r[from+n-1][0], fmt.Sprintf("%.10f", float64(maxMean)/math.Pow10(move))}
	}
	for i := 0; i < 3; i++ {
		row := make([]string, 0, l)
		switch i {
		case 0:
			row = append(row, "開始秒數")
		case 1:
			row = append(row, "結束秒數")
		case 2:
			row = append(row, "最大平均值")
		}
		for j := 1; j < columnMax; j++ {
			row = append(row, count[j][i])
		}
		result = append(result, row)
	}
	file, err := os.Create("fn1_result.csv")
	defer func(file *os.File) {
		e := file.Close()
		if e != nil {

		}
	}(file)
	if err != nil {
		log.Fatalln("failed to open file", err)
	}
	w := csv.NewWriter(file)
	err = w.WriteAll(result)
	if err != nil {
		log.Fatalln("failed to write result", err)
	}
}

func fn2(r [][]string) {
	columnMax := len(r[0])
	move := 10

	var file string
	result := make([][]string, 0, len(r))
	result = append(result, r[0])
	fmt.Print("請輸入要相除的csv檔名: ")
	fmt.Scanln(&file)
	f, err := os.Open(file + ".csv")
	defer func(f *os.File) {
		e := f.Close()
		if e != nil {

		}
	}(f)
	if err != nil {
		panic(err)
	}
	o := csv.NewReader(f)
	oValue, err := o.ReadAll()
	if err != nil {
		panic(err)
	}
	for i := 1; i < len(r); i++ {
		row := make([]string, 0, columnMax)
		row = append(row, r[i][0])
		for j := 1; j < columnMax; j++ {
			value := util.Str2Number[float64, int](r[i][j], move) / util.Str2Number[float64, int](oValue[1][j], move)
			row = append(row, fmt.Sprintf("%.10f", value))
		}
		result = append(result, row)
	}
	resultFile, err := os.Create("fn2_result.csv")
	defer func(file *os.File) {
		e := file.Close()
		if e != nil {

		}
	}(resultFile)
	if err != nil {
		log.Fatalln("failed to open file", err)
	}
	w := csv.NewWriter(resultFile)
	err = w.WriteAll(result)
	if err != nil {
		log.Fatalln("failed to write result", err)
	}
}

func fn3(r [][]string) {
	l := len(r)
	columnMax := len(r[0])
	move := 10

	var file string
	result := make([][]string, 0, len(r))
	result = append(result, r[0])
	fmt.Print("請輸入分期的csv檔名: ")
	fmt.Scanln(&file)
	f, err := os.Open(file + ".csv")
	defer func(f *os.File) {
		e := f.Close()
		if e != nil {

		}
	}(f)
	if err != nil {
		panic(err)
	}
	o := csv.NewReader(f)
	oValue, err := o.ReadAll()
	if err != nil {
		panic(err)
	}
	operate := make([]string, 0, 5)
	for i := 1; i < len(oValue); i++ {
		operate = append(operate, oValue[i][1])
	}
	//fmt.Println(operate)
	count1 := make(map[int][]float64)
	count2 := make(map[int][]float64)
	count3 := make(map[int][]float64)
	count4 := make(map[int][]float64)
	countAllMax := make(map[int][]float64)
	for i := 1; i < l; i++ {
		row := r[i]
		t := util.Str2Number[float64, int](row[0], move)
		switch {
		case t > util.Str2Number[float64, int](operate[0], move) && t < util.Str2Number[float64, int](operate[1], move):
			for j := 1; j < columnMax; j++ {
				count1[j] = append(count1[j], util.Str2Number[float64, int](row[j], 10))
			}
		case t > util.Str2Number[float64, int](operate[1], move) && t < util.Str2Number[float64, int](operate[2], move):
			for j := 1; j < columnMax; j++ {
				count2[j] = append(count2[j], util.Str2Number[float64, int](row[j], 10))
			}
		case t > util.Str2Number[float64, int](operate[2], move) && t < util.Str2Number[float64, int](operate[3], move):
			for j := 1; j < columnMax; j++ {
				count3[j] = append(count3[j], util.Str2Number[float64, int](row[j], 10))
			}
		case t > util.Str2Number[float64, int](operate[3], move) && t < util.Str2Number[float64, int](operate[4], move):
			for j := 1; j < columnMax; j++ {
				count4[j] = append(count4[j], util.Str2Number[float64, int](row[j], 10))
			}
		}
		for j := 1; j < columnMax; j++ {
			countAllMax[j] = append(countAllMax[j], util.Str2Number[float64, int](row[j], 10))
		}
	}
	for i := 0; i < 9; i++ {
		row := make([]string, 0, columnMax)
		switch i {
		case 0:
			row = append(row, "啟跳下蹲階段 最大值")
			for j := 1; j < columnMax; j++ {
				max, _ := util.ArrayMax[float64](count1[j])
				row = append(row, fmt.Sprintf("%.10f", max/math.Pow10(10)))
			}
		case 1:
			row = append(row, "啟跳上升階段 最大值")
			for j := 1; j < columnMax; j++ {
				max, _ := util.ArrayMax[float64](count2[j])
				row = append(row, fmt.Sprintf("%.10f", max/math.Pow10(10)))
			}
		case 2:
			row = append(row, "團身階段 最大值")
			for j := 1; j < columnMax; j++ {
				max, _ := util.ArrayMax[float64](count3[j])
				row = append(row, fmt.Sprintf("%.10f", max/math.Pow10(10)))
			}
		case 3:
			row = append(row, "下降階段 最大值")
			for j := 1; j < columnMax; j++ {
				max, _ := util.ArrayMax[float64](count4[j])
				row = append(row, fmt.Sprintf("%.10f", max/math.Pow10(10)))
			}
		case 4:
			row = append(row, "啟跳下蹲階段 平均值")
			for j := 1; j < columnMax; j++ {
				mean := util.ArrayMean[float64](count1[j])
				row = append(row, fmt.Sprintf("%.10f", mean/math.Pow10(10)))
			}
		case 5:
			row = append(row, "啟跳上升階段 平均值")
			for j := 1; j < columnMax; j++ {
				mean := util.ArrayMean[float64](count2[j])
				row = append(row, fmt.Sprintf("%.10f", mean/math.Pow10(10)))
			}
		case 6:
			row = append(row, "團身階段 平均值")
			for j := 1; j < columnMax; j++ {
				mean := util.ArrayMean[float64](count3[j])
				row = append(row, fmt.Sprintf("%.10f", mean/math.Pow10(10)))
			}
		case 7:
			row = append(row, "下降階段 平均值")
			for j := 1; j < columnMax; j++ {
				mean := util.ArrayMean[float64](count4[j])
				row = append(row, fmt.Sprintf("%.10f", mean/math.Pow10(10)))
			}
		case 8:
			row = append(row, "整個階段最大值出現在_秒")
			for j := 1; j < columnMax; j++ {
				_, index := util.ArrayMax[float64](countAllMax[j])
				row = append(row, fmt.Sprintf("%.2f", util.Str2Number[float64](r[index+1][0], 0)))
			}
		}
		result = append(result, row)
	}

	resultFile, err := os.Create("fn3_result.csv")
	defer func(file *os.File) {
		e := file.Close()
		if e != nil {

		}
	}(resultFile)
	if err != nil {
		log.Fatalln("failed to open file", err)
	}
	w := csv.NewWriter(resultFile)
	err = w.WriteAll(result)
	if err != nil {
		log.Fatalln("failed to write result", err)
	}
}
