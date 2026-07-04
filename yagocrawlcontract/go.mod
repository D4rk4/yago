module github.com/D4rk4/yago/yagocrawlcontract

go 1.26

require (
	github.com/D4rk4/yago/yagomodel v0.0.0
	google.golang.org/grpc v1.79.3
	google.golang.org/protobuf v1.36.10
)

require (
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
)

replace github.com/D4rk4/yago/yagomodel => ../yagomodel
