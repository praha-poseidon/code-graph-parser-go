module github.com/praha-poseidon/code-graph-parser-go

go 1.23.6

require golang.org/x/tools v0.30.0

require (
	golang.org/x/mod v0.23.0 // indirect
	golang.org/x/sync v0.11.0 // indirect
)

require github.com/praha-poseidon/static-extract-go v0.0.0

replace github.com/praha-poseidon/static-extract-go => ../static-extract-go
