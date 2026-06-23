module github.com/nikitakarpei/yacy-rwi-node/yacynode

go 1.26

require (
	github.com/nikitakarpei/yacy-rwi-node/yacymodel v0.0.0
	github.com/nikitakarpei/yacy-rwi-node/yacyproto v0.0.0
	github.com/prometheus/client_golang v1.20.5
	go.etcd.io/bbolt v1.4.3
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	golang.org/x/sys v0.43.0 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)

replace github.com/nikitakarpei/yacy-rwi-node/yacymodel => ../yacymodel

replace github.com/nikitakarpei/yacy-rwi-node/yacyproto => ../yacyproto
