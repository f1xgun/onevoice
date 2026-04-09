module github.com/f1xgun/onevoice/services/agent-vk

go 1.25.0

replace github.com/f1xgun/onevoice/pkg => ../../pkg

require (
	github.com/SevereCloud/vksdk/v3 v3.2.2
	github.com/f1xgun/onevoice/pkg v0.0.0-00010101000000-000000000000
	github.com/nats-io/nats.go v1.49.0
	github.com/stretchr/testify v1.11.1
	golang.org/x/time v0.15.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/klauspost/compress v1.18.4 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
