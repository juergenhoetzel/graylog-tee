 export GOPATH=$(CURDIR)/_vendor

default: dep-install graylog-tee.go
	go build graylog-tee.go

dep-install:
	go get github.com/robertkowalski/graylog-golang
