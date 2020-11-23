module github.com/raulk/trampoline

go 1.15

require (
	github.com/containerd/cgroups v0.0.0-20201119153540-4cbc285b3327
	github.com/opencontainers/runtime-spec v1.0.2
	golang.org/x/sys v0.0.0-20200625212154-ddb9806d33ae // indirect
)

replace github.com/testground/sdk-go/virtual/1 => github.com/testground/sdk-go v0.2.3

replace github.com/testground/sdk-go/virtual/2 => github.com/testground/sdk-go v0.2.1
