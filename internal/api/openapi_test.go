package api_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Mmd4LIFE/pivot/internal/api"
)

// specPath is the OpenAPI document, relative to this package.
const specPath = "../../api/openapi.yaml"

// The spec is the source of truth for the HTTP surface, which only means
// something if it stays true. These tests are what stop it drifting into
// documentation of an API we used to have.

func loadSpec(t *testing.T) map[string]any {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(specPath))
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}

	var spec map[string]any
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}

	return spec
}

func TestOpenAPISpecParses(t *testing.T) {
	t.Parallel()

	spec := loadSpec(t)

	version, _ := spec["openapi"].(string)
	if !strings.HasPrefix(version, "3.1") {
		t.Errorf("openapi = %q, want 3.1.x", version)
	}

	for _, key := range []string{"info", "paths", "components"} {
		if _, ok := spec[key]; !ok {
			t.Errorf("spec is missing the %q section", key)
		}
	}
}

// Every documented endpoint must actually exist. A spec describing a route
// that 404s is worse than no spec: a client trusts it.
func TestOpenAPIPathsExist(t *testing.T) {
	t.Parallel()

	spec := loadSpec(t)

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths is not a mapping")
	}

	if len(paths) == 0 {
		t.Fatal("the spec documents no paths")
	}

	handler := testRouter(t)

	for path, item := range paths {
		methods, ok := item.(map[string]any)
		if !ok {
			continue
		}

		for method := range methods {
			switch strings.ToLower(method) {
			case "get", "post", "put", "patch", "delete":
			default:
				continue // not an operation
			}

			t.Run(strings.ToUpper(method)+" "+path, func(t *testing.T) {
				rec := do(t, handler, strings.ToUpper(method), path, nil)

				if rec.Code == 404 {
					t.Errorf("the spec documents %s %s but the router returns 404",
						strings.ToUpper(method), path)
				}
			})
		}
	}
}

// The error-code pattern in the spec must match what the code actually emits.
// If they drift, a client that validates against the spec rejects real errors.
func TestOpenAPIErrorCodePatternMatchesRegistry(t *testing.T) {
	t.Parallel()

	spec := loadSpec(t)

	components, _ := spec["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	errorBody, _ := schemas["ErrorBody"].(map[string]any)
	props, _ := errorBody["properties"].(map[string]any)
	codeProp, _ := props["code"].(map[string]any)

	pattern, _ := codeProp["pattern"].(string)
	if pattern == "" {
		t.Fatal("ErrorBody.code has no pattern; clients cannot validate codes")
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("the spec's code pattern does not compile: %v", err)
	}

	for _, code := range api.RegisteredCodes() {
		if !re.MatchString(string(code)) {
			t.Errorf("registered code %q does not match the spec's pattern %q", code, pattern)
		}
	}
}

// Every error response in the spec must reference the shared envelope, so a
// client really does need only one error path.
func TestOpenAPIErrorResponsesUseTheEnvelope(t *testing.T) {
	t.Parallel()

	spec := loadSpec(t)

	components, _ := spec["components"].(map[string]any)

	responses, ok := components["responses"].(map[string]any)
	if !ok {
		t.Fatal("components.responses is missing")
	}

	if len(responses) == 0 {
		t.Fatal("no reusable error responses are defined")
	}

	for name, resp := range responses {
		r, _ := resp.(map[string]any)
		content, _ := r["content"].(map[string]any)
		json, _ := content["application/json"].(map[string]any)
		schema, _ := json["schema"].(map[string]any)
		ref, _ := schema["$ref"].(string)

		if ref != "#/components/schemas/ErrorResponse" {
			t.Errorf("response %q uses schema %q, want the shared ErrorResponse", name, ref)
		}
	}
}
