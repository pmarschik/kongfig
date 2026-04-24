module github.com/pmarschik/kongfig/providers/file

go 1.26.2

require (
	github.com/fsnotify/fsnotify v1.9.0
	github.com/pmarschik/kongfig v0.0.0-00010101000000-000000000000
)

require (
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

replace github.com/pmarschik/kongfig => ../../
