package unifi

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	openAPIContractVersion = "10.4.57"
	openAPIContractSHA256  = "3773947b4572b1bdc84272d3d2b9ebb5cad0d9aaa2640c3418568df97c9df59d"
	componentSchemaPrefix  = "#/components/schemas/"
	definitionSchemaPrefix = "#/$defs/"
)

// The formal 10.4.57 contract is embedded because it contains the complete
// request-body schemas. Its 73 method/path/operationId tuples match the
// supplied Network 10.5.67 controller documentation capture.
//
//go:embed network_v10.4.57_openapi.json
var embeddedOpenAPIContract []byte

type contractDocument struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]any `json:"schemas"`
	} `json:"components"`
}

type contractOperation struct {
	OperationID string              `json:"operationId"`
	Summary     string              `json:"summary"`
	Description string              `json:"description"`
	Parameters  []contractParameter `json:"parameters"`
	RequestBody *struct {
		Description string `json:"description"`
		Required    bool   `json:"required"`
		Content     map[string]struct {
			Schema map[string]any `json:"schema"`
		} `json:"content"`
	} `json:"requestBody"`
}

type contractParameter struct {
	Name        string                  `json:"name"`
	In          string                  `json:"in"`
	Description string                  `json:"description"`
	Required    bool                    `json:"required"`
	Schema      contractParameterSchema `json:"schema"`
}

type contractParameterSchema struct {
	Type        string   `json:"type"`
	Format      string   `json:"format"`
	Description string   `json:"description"`
	Default     any      `json:"default"`
	Minimum     *float64 `json:"minimum"`
	Maximum     *float64 `json:"maximum"`
}

func mustBuildEndpoints(readEndpoints []Endpoint) []Endpoint {
	checksum := fmt.Sprintf("%x", sha256.Sum256(embeddedOpenAPIContract))
	if checksum != openAPIContractSHA256 {
		panic(fmt.Sprintf("embedded UniFi OpenAPI checksum = %s, want %s", checksum, openAPIContractSHA256))
	}

	var document contractDocument
	if err := json.Unmarshal(embeddedOpenAPIContract, &document); err != nil {
		panic(fmt.Sprintf("decode embedded UniFi OpenAPI contract: %v", err))
	}
	if document.Info.Version != openAPIContractVersion {
		panic(fmt.Sprintf("embedded UniFi OpenAPI version = %q, want %q", document.Info.Version, openAPIContractVersion))
	}

	result := make([]Endpoint, len(readEndpoints))
	for index, endpoint := range readEndpoints {
		result[index] = enrichReadEndpointFromContract(document, endpoint)
	}

	seenNames := make(map[string]struct{}, len(readEndpoints))
	for _, endpoint := range result {
		if _, exists := seenNames[endpoint.Name]; exists {
			panic(fmt.Sprintf("duplicate UniFi MCP tool name %q", endpoint.Name))
		}
		seenNames[endpoint.Name] = struct{}{}
	}

	mutations := make([]Endpoint, 0, 32)
	for path, pathItem := range document.Paths {
		for method, rawOperation := range pathItem {
			method = strings.ToUpper(method)
			if method == methodGET || !isSupportedMethod(method) {
				continue
			}

			var operation contractOperation
			if err := json.Unmarshal(rawOperation, &operation); err != nil {
				panic(fmt.Sprintf("decode UniFi OpenAPI %s %s: %v", method, path, err))
			}
			endpoint := mutationEndpointFromContract(document, method, path, operation)
			if _, exists := seenNames[endpoint.Name]; exists {
				panic(fmt.Sprintf("duplicate UniFi MCP tool name %q", endpoint.Name))
			}
			seenNames[endpoint.Name] = struct{}{}
			mutations = append(mutations, endpoint)
		}
	}

	sort.Slice(mutations, func(i, j int) bool {
		return mutations[i].Name < mutations[j].Name
	})
	return append(result, mutations...)
}

func enrichReadEndpointFromContract(document contractDocument, endpoint Endpoint) Endpoint {
	pathItem, ok := document.Paths[endpoint.Path]
	if !ok {
		panic(fmt.Sprintf("read-only UniFi MCP tool %s references undocumented path %s", endpoint.Name, endpoint.Path))
	}
	rawOperation, ok := pathItem["get"]
	if !ok {
		panic(fmt.Sprintf("read-only UniFi MCP tool %s references path without GET %s", endpoint.Name, endpoint.Path))
	}
	var operation contractOperation
	if err := json.Unmarshal(rawOperation, &operation); err != nil {
		panic(fmt.Sprintf("decode UniFi OpenAPI GET %s: %v", endpoint.Path, err))
	}

	endpoint.PathParamSchemas = map[string]ParameterSchema{}
	endpoint.QueryParams = append([]QueryParameter(nil), endpoint.QueryParams...)
	for _, parameter := range operation.Parameters {
		schema := ParameterSchema{
			Type:        parameter.Schema.Type,
			Format:      parameter.Schema.Format,
			Description: firstNonEmpty(parameter.Description, parameter.Schema.Description),
			Default:     parameter.Schema.Default,
			Minimum:     parameter.Schema.Minimum,
			Maximum:     parameter.Schema.Maximum,
		}.withDefaults()
		switch parameter.In {
		case "path":
			endpoint.PathParamSchemas[parameter.Name] = schema
		case "query":
			for index := range endpoint.QueryParams {
				if endpoint.QueryParams[index].Name == parameter.Name {
					endpoint.QueryParams[index].Required = parameter.Required
					endpoint.QueryParams[index].Schema = schema
				}
			}
		}
	}
	return endpoint
}

func isSupportedMethod(method string) bool {
	switch method {
	case methodGET, methodPOST, methodPUT, methodPATCH, methodDELETE:
		return true
	default:
		return false
	}
}

func mutationEndpointFromContract(document contractDocument, method, path string, operation contractOperation) Endpoint {
	name := strings.TrimSpace(operation.OperationID)
	if name == "" {
		panic(fmt.Sprintf("UniFi OpenAPI %s %s has no operationId", method, path))
	}

	endpoint := Endpoint{
		Name:             name,
		Description:      operationDescription(operation),
		Method:           method,
		Path:             path,
		PathParamSchemas: map[string]ParameterSchema{},
		Destructive:      mutationIsDestructive(name, method),
	}

	for _, parameter := range operation.Parameters {
		schema := ParameterSchema{
			Type:        parameter.Schema.Type,
			Format:      parameter.Schema.Format,
			Description: firstNonEmpty(parameter.Description, parameter.Schema.Description),
			Default:     parameter.Schema.Default,
			Minimum:     parameter.Schema.Minimum,
			Maximum:     parameter.Schema.Maximum,
		}.withDefaults()
		switch parameter.In {
		case "path":
			if !parameter.Required {
				panic(fmt.Sprintf("UniFi OpenAPI %s %s path parameter %s is optional", method, path, parameter.Name))
			}
			endpoint.PathParams = append(endpoint.PathParams, parameter.Name)
			endpoint.PathParamSchemas[parameter.Name] = schema
		case "query":
			endpoint.QueryParams = append(endpoint.QueryParams, QueryParameter{
				Name:        parameter.Name,
				Description: schema.Description,
				Required:    parameter.Required,
				Schema:      schema,
			})
		}
	}

	sort.Strings(endpoint.PathParams)
	sort.Slice(endpoint.QueryParams, func(i, j int) bool {
		return endpoint.QueryParams[i].Name < endpoint.QueryParams[j].Name
	})
	endpoint.RequestBody = requestBodyFromContract(document, operation)
	return endpoint
}

func operationDescription(operation contractOperation) string {
	description := strings.TrimSpace(operation.Description)
	if index := strings.Index(description, "\n\n"); index >= 0 {
		description = description[:index]
	}
	if description != "" {
		return strings.Join(strings.Fields(description), " ")
	}
	return strings.TrimSpace(operation.Summary)
}

func mutationIsDestructive(operationID, method string) bool {
	switch method {
	case methodDELETE, methodPUT, methodPATCH:
		return true
	case methodPOST:
		return operationID == "adoptDevice" || strings.HasPrefix(operationID, "execute")
	default:
		return false
	}
}

func requestBodyFromContract(document contractDocument, operation contractOperation) *RequestBody {
	if operation.RequestBody == nil {
		return nil
	}
	mediaType, ok := operation.RequestBody.Content["application/json"]
	if !ok || len(mediaType.Schema) == 0 {
		panic(fmt.Sprintf("UniFi OpenAPI operation %s has no application/json request schema", operation.OperationID))
	}

	definitions := referencedDefinitions(mediaType.Schema, document.Components.Schemas)
	schema := rewriteSchemaReferences(mediaType.Schema).(map[string]any)
	if discriminatorSchema := discriminatorRequestSchema(mediaType.Schema, document.Components.Schemas); discriminatorSchema != nil {
		schema = discriminatorSchema
	}

	return &RequestBody{
		Required:    operation.RequestBody.Required,
		Description: firstNonEmpty(strings.TrimSpace(operation.RequestBody.Description), "JSON request body defined by the UniFi Network OpenAPI contract."),
		Schema:      schema,
		Definitions: definitions,
	}
}

func referencedDefinitions(root map[string]any, components map[string]any) map[string]any {
	definitions := map[string]any{}
	queued := schemaReferences(root)
	for len(queued) > 0 {
		name := queued[0]
		queued = queued[1:]
		if _, exists := definitions[name]; exists {
			continue
		}
		raw, exists := components[name]
		if !exists {
			panic(fmt.Sprintf("UniFi OpenAPI request schema references missing component %q", name))
		}
		definitions[name] = rewriteSchemaReferences(raw)
		for _, reference := range schemaReferences(raw) {
			if _, exists := definitions[reference]; !exists {
				queued = append(queued, reference)
			}
		}
	}
	return definitions
}

func schemaReferences(value any) []string {
	seen := map[string]struct{}{}
	var visit func(any)
	visit = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				if key == "$ref" {
					if reference, ok := child.(string); ok {
						if name, ok := componentName(reference); ok {
							seen[name] = struct{}{}
						}
					}
				}
				if key == "mapping" {
					if mapping, ok := child.(map[string]any); ok {
						for _, mapped := range mapping {
							if reference, ok := mapped.(string); ok {
								if name, ok := componentName(reference); ok {
									seen[name] = struct{}{}
								}
							}
						}
					}
				}
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		}
	}
	visit(value)

	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func rewriteSchemaReferences(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = rewriteSchemaReferences(child)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = rewriteSchemaReferences(child)
		}
		return result
	case string:
		if name, ok := componentName(typed); ok {
			return definitionSchemaPrefix + name
		}
		return typed
	default:
		return value
	}
}

func discriminatorRequestSchema(root map[string]any, components map[string]any) map[string]any {
	reference, ok := root["$ref"].(string)
	if !ok {
		return nil
	}
	rootName, ok := componentName(reference)
	if !ok {
		return nil
	}
	component, ok := components[rootName].(map[string]any)
	if !ok {
		return nil
	}
	discriminator, ok := component["discriminator"].(map[string]any)
	if !ok {
		return nil
	}
	propertyName, ok := discriminator["propertyName"].(string)
	if !ok || propertyName == "" {
		return nil
	}
	mapping, ok := discriminator["mapping"].(map[string]any)
	if !ok || len(mapping) == 0 {
		return nil
	}

	values := make([]string, 0, len(mapping))
	for value := range mapping {
		values = append(values, value)
	}
	sort.Strings(values)

	variants := make([]any, 0, len(values))
	for _, value := range values {
		mappedReference, ok := mapping[value].(string)
		if !ok {
			continue
		}
		variants = append(variants, map[string]any{
			"title": value,
			"allOf": []any{
				map[string]any{"$ref": rewriteSchemaReferences(mappedReference)},
				map[string]any{
					"type": "object",
					"properties": map[string]any{
						propertyName: map[string]any{"const": value},
					},
					"required": []any{propertyName},
				},
			},
		})
	}
	if len(variants) == 0 {
		return nil
	}
	return map[string]any{
		"description": fmt.Sprintf("Select a request variant using the %q discriminator.", propertyName),
		"oneOf":       variants,
	}
}

func componentName(reference string) (string, bool) {
	if !strings.HasPrefix(reference, componentSchemaPrefix) {
		return "", false
	}
	name := strings.TrimPrefix(reference, componentSchemaPrefix)
	return name, name != ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
