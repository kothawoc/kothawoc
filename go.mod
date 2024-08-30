module github.com/kothawoc/kothawoc

go 1.23.0

require (
	github.com/cretz/bine v0.2.0
	github.com/kothawoc/go-nntp v0.0.0-20240822123350-30744ff03402
	github.com/mattn/go-sqlite3 v1.14.22
	golang.org/x/crypto v0.26.0
)

require (
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.23.0 // indirect
)

replace github.com/kothawoc/go-nntp => ../go-nntp
