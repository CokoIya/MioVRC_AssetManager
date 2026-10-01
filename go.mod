module vrclib

go 1.24

require (
	github.com/jchv/go-webview2 v0.0.0
	github.com/jchv/go-winloader v0.0.0
	golang.org/x/sys v0.0.0
)

// vendored copies (proxy.golang.org is not reachable from every network)
replace (
	github.com/jchv/go-webview2 => ./third_party/go-webview2
	github.com/jchv/go-winloader => ./third_party/go-winloader
	golang.org/x/sys => ./third_party/sys
)
