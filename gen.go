package tritter

//go:generate protoc -I=. -I=$GOPATH/src -I=$GOPATH/src/github.com/googleapis/googleapis --go_out=plugins=grpc:$GOPATH/src tritter.proto
