module github.com/nikitakarpei/yacy-rwi-node/yacynode

go 1.26

require (
	github.com/google/uuid v1.6.0
	github.com/nats-io/nats-server/v2 v2.14.2
	github.com/nats-io/nats.go v1.52.0
	github.com/nikitakarpei/yacy-rwi-node/bytesize v0.0.0
	github.com/nikitakarpei/yacy-rwi-node/serviceruntime v0.0.0
	github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract v0.0.0
	github.com/nikitakarpei/yacy-rwi-node/yacymodel v0.0.0
	github.com/nikitakarpei/yacy-rwi-node/yacyproto v0.0.0
	github.com/prometheus/client_golang v1.23.2
	go.etcd.io/bbolt v1.4.3
)

require (
	github.com/antithesishq/antithesis-sdk-go v0.7.0-default-no-op // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/jwt/v2 v2.8.2 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/crypto v0.52.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
)

replace github.com/nikitakarpei/yacy-rwi-node/bytesize => ../../libraries/bytesize

replace github.com/nikitakarpei/yacy-rwi-node/searchdocument => ../../libraries/searchdocument

replace github.com/nikitakarpei/yacy-rwi-node/serviceruntime => ../../libraries/serviceruntime

replace github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract => ../../libraries/yacycrawlcontract

replace github.com/nikitakarpei/yacy-rwi-node/yacymodel => ../../libraries/yacymodel

replace github.com/nikitakarpei/yacy-rwi-node/yacyproto => ../../libraries/yacyproto
