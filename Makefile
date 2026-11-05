.PHONY: run install packageAppImage

run:
	go1.27 run .

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go1.27 build 

install:
	go1.27 install