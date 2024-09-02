module github.com/kothawoc/kothawoc

go 1.23.0

require (
	//github.com/cretz/bine v0.2.0
	github.com/kothawoc/go-nntp v0.0.0-20240830121046-7682f02055ea
	github.com/mattn/go-sqlite3 v1.14.22
	golang.org/x/crypto v0.26.0
)

require (
	github.com/cretz/bine v0.2.0
	github.com/emersion/go-vcard v0.0.0-20230815062825-8fda7d206ec9
)

require (
	golang.org/x/net v0.21.0 // indirect
	golang.org/x/sys v0.24.0 // indirect
)

replace github.com/kothawoc/go-nntp => ../go-nntp

//replace github.com/cretz/bine => github.com/mibmo/bine v0.2.1-0.20220611130251-c08c905da086
