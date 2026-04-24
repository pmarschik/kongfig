module github.com/pmarschik/kongfig/parsers/toml

go 1.26.2

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/pmarschik/kongfig v0.0.0-00010101000000-000000000000
)

require github.com/go-viper/mapstructure/v2 v2.5.0 // indirect

replace github.com/pmarschik/kongfig => ../../
