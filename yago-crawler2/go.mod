module github.com/D4rk4/yago/yago-crawler2

go 1.26.10

require (
	github.com/D4rk4/yago/yagocrawlcontract v0.0.0
	github.com/D4rk4/yago/yagoegress v0.0.0
	github.com/D4rk4/yago/yagomodel v0.0.0
	github.com/temoto/robotstxt v1.1.2
	go.etcd.io/bbolt v1.4.3
	golang.org/x/net v0.62.0
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

replace github.com/D4rk4/yago/yagocrawlcontract => ../yagocrawlcontract

replace github.com/D4rk4/yago/yagoegress => ../yagoegress

replace github.com/D4rk4/yago/yagomodel => ../yagomodel
