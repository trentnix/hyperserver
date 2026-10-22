module github.com/trentnix/hyperserver/examples/blog

go 1.27.3

require (
	github.com/mattn/go-sqlite3 v1.14.52
	github.com/trentnix/hyperserver v0.0.0
	github.com/yuin/goldmark v1.8.6
)

require (
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/trentnix/hyperserver => ../..
