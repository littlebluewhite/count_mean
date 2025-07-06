w-build:
	GOOS=windows GOARCH=amd64 go build -o main.exe main.go

u-build:
	go build -o app main.go