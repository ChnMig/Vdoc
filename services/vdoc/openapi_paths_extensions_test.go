package vdoc

import (
	"strings"
	"testing"
)

func TestOpenAPIPathsExtensionsAreNotOperations(t *testing.T) {
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, extension := range []string{`"annotation"`, `{"$ref":"literal-data","get":{"responses":{"200":{"description":"not an operation"}}}}`} {
			t.Run(version+"/"+extension, func(t *testing.T) {
				content := `{"openapi":"` + version + `","info":{"title":"Extensions","version":"1"},"paths":{"x-notes":` + extension + `,"/values":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
				parsed, err := ParseOpenAPI(content)
				if err != nil {
					t.Fatalf("valid Paths Object extension was rejected: %v", err)
				}
				if len(parsed.Endpoints) != 1 || parsed.Endpoints[0].Path != "/values" || !strings.Contains(parsed.Normalized, `"x-notes":`) {
					t.Fatalf("extension must remain data without becoming an endpoint: %+v", parsed)
				}
			})
		}
	}
}

func TestOpenAPIPathsExtensionsKeepPathValidation(t *testing.T) {
	for _, paths := range []string{
		`{"x-notes":{"get":{"responses":{"200":{"description":"no real operation"}}}}}`,
		`{"invalid":{"get":{"responses":{"200":{"description":"missing slash"}}}}}`,
		`{"x-notes":{},"/invalid":"not a path item"}`,
	} {
		t.Run(paths, func(t *testing.T) {
			content := `{"openapi":"3.1.0","info":{"title":"Paths","version":"1"},"paths":` + paths + `}`
			if _, err := ParseOpenAPI(content); !Is(err, ErrInvalidArgument) {
				t.Fatalf("invalid paths accepted: %v", err)
			}
		})
	}
}
