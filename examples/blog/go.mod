module github.com/trentnix/hyperserver/examples/blog

go 1.27.3

require (
	github.com/mattn/go-sqlite3 v1.14.52
	github.com/trentnix/hyperserver v0.0.0
	github.com/yuin/goldmark v1.8.6
	golang.org/x/net v0.61.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/bep/imagemeta v1.1.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/image v0.47.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace github.com/trentnix/hyperserver => ../..
