package apidocs

import _ "embed"

//go:embed openapi.yaml
var openAPIYAML []byte

func OpenAPIYAML() []byte {
	return append([]byte(nil), openAPIYAML...)
}
