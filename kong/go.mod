module github.com/pmarschik/kongfig/kong

go 1.26.2

require (
	github.com/alecthomas/kong v1.15.0
	github.com/pmarschik/kongfig v0.0.0-00010101000000-000000000000
	github.com/pmarschik/kongfig/providers/file v0.0.0-00010101000000-000000000000
)

require (
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

replace (
	github.com/pmarschik/kongfig => ../
	github.com/pmarschik/kongfig/providers/file => ../providers/file
)
