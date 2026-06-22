module github.com/nikitakarpei/yacy-rwi-node/yacynode

go 1.26

require (
	github.com/nikitakarpei/yacy-rwi-node/yacymodel v0.0.0
	github.com/nikitakarpei/yacy-rwi-node/yacyproto v0.0.0
	go.etcd.io/bbolt v1.4.3
)

require golang.org/x/sys v0.43.0 // indirect

replace github.com/nikitakarpei/yacy-rwi-node/yacymodel => ../yacymodel

replace github.com/nikitakarpei/yacy-rwi-node/yacyproto => ../yacyproto
