module github.com/Mmd4LIFE/pivot

// Minimum supported toolchain, per ADR-0001. Development currently uses a
// newer Go; keeping the floor low keeps the barrier low for contributors.
go 1.23

// Dependencies are added deliberately and kept few. Configuration precedence
// is hand-rolled rather than delegated to viper: viper pulls a large tree for
// behaviour we need exact control over, and `flags > env > file > defaults` is
// the property our tests must prove.
require (
	github.com/spf13/cobra v1.10.2
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
)
