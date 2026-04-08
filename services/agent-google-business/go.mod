module github.com/f1xgun/onevoice/services/agent-google-business

go 1.24

require (
	github.com/f1xgun/onevoice/pkg v0.0.0
	github.com/nats-io/nats.go v1.41.1
)

require (
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/nats-io/nkeys v0.4.9 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.40.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
)

replace github.com/f1xgun/onevoice/pkg => ../../pkg
