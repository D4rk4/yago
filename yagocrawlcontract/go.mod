module github.com/D4rk4/yago/yagocrawlcontract

go 1.26.11

require (
	github.com/D4rk4/yago/yagomodel v0.0.0
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

replace github.com/D4rk4/yago/yagomodel => ../yagomodel
