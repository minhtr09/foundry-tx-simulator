package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"

	"foundry-tx-simulator/backend/internal/model"
)

const (
	addressPattern         = `^0x[0-9a-fA-F]{40}$`
	bytesPattern           = `^0x([0-9a-fA-F]{2})*$`
	txHashPattern          = `^0x[0-9a-fA-F]{64}$`
	uint256Pattern         = `^(0x[0-9a-fA-F]+|[0-9]+)$`
	optionalUint256Pattern = `^(|0x[0-9a-fA-F]+|[0-9]+)$`
)

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	spec, err := openAPISpec(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.ErrorResponse{Error: "generate openapi spec: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, spec)
}

func (s *Server) handleSwaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swaggerHTML))
}

const swaggerHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Foundry Tx Simulator API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    body { margin: 0; background: #f7f7f7; }
    .swagger-ui .topbar { display: none; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function () {
      window.ui = SwaggerUIBundle({
        url: "/openapi.json",
        dom_id: "#swagger-ui",
        deepLinking: true,
        displayRequestDuration: true
      });
    };
  </script>
</body>
</html>`

func openAPISpec(ctx context.Context) (*openapi3.T, error) {
	components := openapi3.NewComponents()
	components.Schemas = openapi3.Schemas{}
	spec := &openapi3.T{
		OpenAPI: "3.0.3",
		Info: &openapi3.Info{
			Title:   "Foundry Tx Simulator API",
			Version: "0.1.0",
		},
		Paths:      openapi3.NewPaths(),
		Components: &components,
	}

	if err := registerOpenAPISchemas(spec.Components.Schemas); err != nil {
		return nil, err
	}
	enrichOpenAPISchemas(spec.Components.Schemas)
	addOpenAPIOperations(spec)

	if err := validateOpenAPISpec(ctx, spec); err != nil {
		return nil, err
	}
	return spec, nil
}

func validateOpenAPISpec(ctx context.Context, spec *openapi3.T) error {
	value, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	loader := openapi3.NewLoader()
	loader.Context = ctx
	loaded, err := loader.LoadFromData(value)
	if err != nil {
		return err
	}
	return loaded.Validate(ctx)
}

func registerOpenAPISchemas(schemas openapi3.Schemas) error {
	for _, item := range []struct {
		name  string
		value any
	}{
		{"HealthResponse", model.HealthResponse{}},
		{"ChainsResponse", model.ChainsResponse{}},
		{"ProjectsResponse", model.ProjectsResponse{}},
		{"BrowseProjectResponse", model.BrowseProjectResponse{}},
		{"SimulationRecord", model.SimulationRecord{}},
		{"SimulateRequest", model.SimulateRequest{}},
		{"TxRequest", model.TxRequest{}},
		{"LabelOverride", model.LabelOverride{}},
		{"ERC20BalanceOverride", model.ERC20BalanceOverride{}},
		{"ERC20ApprovalOverride", model.ERC20ApprovalOverride{}},
		{"ERC721ApprovalOverride", model.ERC721ApprovalOverride{}},
		{"StateOverride", model.StateOverride{}},
		{"CompilerConfig", model.CompilerConfig{}},
		{"SimulateResponse", model.SimulateResponse{}},
		{"ERC20Transfer", model.ERC20Transfer{}},
		{"BalanceAnalysis", model.BalanceAnalysis{}},
		{"TokenBalanceChange", model.TokenBalanceChange{}},
		{"UserUSDChange", model.UserUSDChange{}},
		{"ErrorResponse", model.ErrorResponse{}},
	} {
		if err := registerOpenAPISchema(schemas, item.name, item.value); err != nil {
			return err
		}
	}
	return nil
}

func registerOpenAPISchema(schemas openapi3.Schemas, name string, value any) error {
	ref, err := openapi3gen.NewSchemaRefForValue(value, schemas, openapi3gen.CreateComponentSchemas(openapi3gen.ExportComponentSchemasOptions{
		ExportComponentSchemas: true,
		ExportTopLevelSchema:   true,
	}), openapi3gen.CreateTypeNameGenerator(openAPITypeName), openapi3gen.SchemaCustomizer(openAPISchemaCustomizer))
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if generated := concreteSchemaRef(schemas[name]); generated != nil {
		schemas[name] = generated
		return nil
	}
	if generated := concreteSchemaRef(ref); generated != nil {
		schemas[name] = generated
		return nil
	}
	refName := strings.TrimPrefix(ref.Ref, "#/components/schemas/")
	if refName != ref.Ref {
		if generated := concreteSchemaRef(schemas[refName]); generated != nil {
			schemas[name] = generated
			return nil
		}
	}

	inlineRef, err := openapi3gen.NewSchemaRefForValue(value, schemas, openapi3gen.CreateTypeNameGenerator(openAPITypeName), openapi3gen.SchemaCustomizer(openAPISchemaCustomizer))
	if err != nil {
		return fmt.Errorf("%s inline schema: %w", name, err)
	}
	if generated := concreteSchemaRef(inlineRef); generated != nil {
		schemas[name] = generated
		return nil
	}
	return fmt.Errorf("%s schema was not generated", name)
}

func concreteSchemaRef(ref *openapi3.SchemaRef) *openapi3.SchemaRef {
	if ref == nil || ref.Value == nil {
		return nil
	}
	return &openapi3.SchemaRef{Value: ref.Value}
}

func openAPITypeName(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Name() != "" {
		return t.Name()
	}
	return strings.NewReplacer(".", "_", "[]", "Array").Replace(t.String())
}

func openAPISchemaCustomizer(_ string, t reflect.Type, tag reflect.StructTag, schema *openapi3.Schema) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == reflect.TypeOf(model.Uint256("")) {
		schema.Type = &openapi3.Types{"string"}
		schema.Pattern = uint256Pattern
		schema.Example = "1000000000000000000"
		return nil
	}

	validateTag := tag.Get("validate")
	if strings.Contains(validateTag, "eth_address") {
		schema.Type = &openapi3.Types{"string"}
		schema.Pattern = addressPattern
	}
	if strings.Contains(validateTag, "hex_bytes") {
		schema.Type = &openapi3.Types{"string"}
		schema.Pattern = bytesPattern
	}
	if strings.Contains(validateTag, "tx_hash") {
		schema.Type = &openapi3.Types{"string"}
		schema.Pattern = txHashPattern
	}
	return nil
}

func enrichOpenAPISchemas(schemas openapi3.Schemas) {
	setPropertyExample(schemas, "SimulateRequest", "chain", "mainnet")
	setPropertyDescription(schemas, "SimulateRequest", "blockNumber", "Optional fork block number. Omit or send an empty string to use the latest block from the configured chain RPC.")
	setPropertyPattern(schemas, "SimulateRequest", "blockNumber", optionalUint256Pattern)
	setPropertyExample(schemas, "SimulateRequest", "blockNumber", "23000000")
	setPropertyDescription(schemas, "SimulateRequest", "projectPath", "Optional Foundry project root. When set, the backend runs `forge-kyber build src`, copies the simulation test harness under this project's test folder, and runs forge-kyber test with this root.")
	setPropertyExample(schemas, "SimulateRequest", "projectPath", "~/project")
	setPropertyDescription(schemas, "SimulateRequest", "stateOverrideBytecode", "Backend-generated compiled state override bytecode for the simulation test input. Client requests should omit this field.")
	setPropertyDescription(schemas, "SimulateRequest", "decodeInternal", "When true, passes --decode-internal to forge-kyber test. Defaults to false.")
	setPropertyDefault(schemas, "SimulateRequest", "decodeInternal", false)
	setPropertyExample(schemas, "SimulateRequest", "sender", "0x0000000000000000000000000000000000000001")
	setPropertyExample(schemas, "SimulateRequest", "target", "0x0000000000000000000000000000000000000002")
	setPropertyExample(schemas, "SimulateRequest", "data", "0x")
	setPropertyDescription(schemas, "SimulateRequest", "value", "Native token call value in wei. Defaults to 0; the sender must already have enough balance in fork state or overrides.")
	setPropertyDefault(schemas, "SimulateRequest", "value", "0")
	setPropertyExample(schemas, "SimulateRequest", "value", "0")
	setPropertyExample(schemas, "TxRequest", "chain", "mainnet")
	setPropertyExample(schemas, "TxRequest", "txHash", "0x0000000000000000000000000000000000000000000000000000000000000000")
	setPropertyDescription(schemas, "TxRequest", "decodeInternal", "When true, passes --decode-internal to cast-kyber run. Defaults to false.")
	setPropertyDefault(schemas, "TxRequest", "decodeInternal", false)
	setPropertyDescription(schemas, "TxRequest", "quick", "When true, passes --quick to cast-kyber run. Defaults to false.")
	setPropertyDefault(schemas, "TxRequest", "quick", false)

	setPropertyDescription(schemas, "ChainsResponse", "explorerUrls", "Map of configured chain name to block explorer base URL.")
	setPropertyDescription(schemas, "ProjectsResponse", "projects", "Most recently used Foundry project paths.")
	setPropertyDescription(schemas, "BrowseProjectResponse", "path", "Absolute path selected by the local backend folder picker.")
	setPropertyExample(schemas, "BrowseProjectResponse", "path", "~/foundry-project")

	setPropertyExample(schemas, "LabelOverride", "account", "0x0000000000000000000000000000000000000001")
	setPropertyExample(schemas, "LabelOverride", "label", "WETHOwner")
	setPropertyExample(schemas, "ERC20BalanceOverride", "token", "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	setPropertyExample(schemas, "ERC20BalanceOverride", "account", "0x0000000000000000000000000000000000000001")
	setPropertyExample(schemas, "ERC20ApprovalOverride", "token", "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	setPropertyExample(schemas, "ERC20ApprovalOverride", "owner", "0x0000000000000000000000000000000000000001")
	setPropertyExample(schemas, "ERC20ApprovalOverride", "spender", "0x0000000000000000000000000000000000000002")
	setPropertyExample(schemas, "ERC721ApprovalOverride", "token", "0xBC4CA0EdA7647A8aB7C2061c2E118A18a936f13D")
	setPropertyExample(schemas, "ERC721ApprovalOverride", "owner", "0x0000000000000000000000000000000000000001")
	setPropertyExample(schemas, "ERC721ApprovalOverride", "spender", "0x0000000000000000000000000000000000000002")
	setPropertyExample(schemas, "StateOverride", "contractName", "MyStateOverride")

	setPropertyDescription(schemas, "CompilerConfig", "use", "Maps to forge-kyber --use <SOLC_VERSION>. Omitted unless explicitly provided.")
	setPropertyDescription(schemas, "CompilerConfig", "offline", "Maps to --offline.")
	setPropertyDescription(schemas, "CompilerConfig", "noAutoDetect", "Maps to --no-auto-detect.")
	setPropertyDescription(schemas, "CompilerConfig", "viaIR", "Maps to --via-ir. Defaults to true for this backend.")
	setPropertyDefault(schemas, "CompilerConfig", "viaIR", true)
	setPropertyDescription(schemas, "CompilerConfig", "useLiteralContent", "Maps to --use-literal-content.")
	setPropertyDescription(schemas, "CompilerConfig", "noMetadata", "Maps to --no-metadata.")
	setPropertyDescription(schemas, "CompilerConfig", "evmVersion", "Maps to --evm-version <VERSION>. Omitted unless explicitly provided.")
	setPropertyDescription(schemas, "CompilerConfig", "optimize", "Maps to --optimize. Defaults to true for this backend.")
	setPropertyDefault(schemas, "CompilerConfig", "optimize", true)
	setPropertyDescription(schemas, "CompilerConfig", "optimizerRuns", "Maps to --optimizer-runs <RUNS>.")
	setPropertyMinMax(schemas, "CompilerConfig", "optimizerRuns", 0, 4294967295)
	setPropertyExample(schemas, "CompilerConfig", "optimizerRuns", 200)
	setPropertyDescription(schemas, "CompilerConfig", "revertStrings", "Maps to --revert-strings <REVERT>.")
	setPropertyEnum(schemas, "CompilerConfig", "revertStrings", "default", "strip", "debug", "verboseDebug")
	setPropertyExample(schemas, "CompilerConfig", "revertStrings", "default")

	if schema := schemaValue(schemas, "Uint256"); schema != nil {
		schema.Type = &openapi3.Types{"string"}
		schema.Pattern = uint256Pattern
		schema.Example = "1000000000000000000"
	}
}

func addOpenAPIOperations(spec *openapi3.T) {
	spec.AddOperation("/health", http.MethodGet, getOperation(
		"Backend health and configured chain count",
		"HealthResponse",
	))
	spec.AddOperation("/chains", http.MethodGet, getOperation(
		"List configured chain names",
		"ChainsResponse",
	))
	spec.AddOperation("/projects", http.MethodGet, getOperation(
		"List recently used Foundry project paths",
		"ProjectsResponse",
	))
	spec.AddOperation("/browse/project", http.MethodGet, getOperation(
		"Open a local folder picker and return a Foundry project path",
		"BrowseProjectResponse",
		withErrorResponse(http.StatusBadRequest, "ErrorResponse"),
	))
	spec.AddOperation("/requests/{id}", http.MethodGet, getOperation(
		"Load a saved request and response by request ID",
		"SimulationRecord",
		withPathParameter("id", "Request ID returned by /simulation or /tx"),
		withErrorResponse(http.StatusBadRequest, "ErrorResponse"),
		withErrorResponse(http.StatusNotFound, "ErrorResponse"),
	))

	simulationOp := postOperation(
		"Run a forge-kyber test simulation and return the execution trace with fund-flow analysis",
		"SimulateRequest",
		"SimulateResponse",
	)
	for _, status := range []int{http.StatusBadRequest} {
		simulationOp.Responses.Set(fmt.Sprintf("%d", status), jsonResponse("ErrorResponse", http.StatusText(status)))
	}
	for _, status := range []int{http.StatusTooManyRequests, http.StatusBadGateway, http.StatusInternalServerError, http.StatusGatewayTimeout} {
		simulationOp.Responses.Set(fmt.Sprintf("%d", status), jsonResponse("SimulateResponse", http.StatusText(status)))
	}
	spec.AddOperation("/simulation", http.MethodPost, simulationOp)

	txOp := postOperation(
		"Replay a published transaction with cast-kyber run and return the execution trace with fund-flow analysis",
		"TxRequest",
		"SimulateResponse",
	)
	txOp.Responses.Set("400", jsonResponse("ErrorResponse", http.StatusText(http.StatusBadRequest)))
	for _, status := range []int{http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusInternalServerError} {
		txOp.Responses.Set(fmt.Sprintf("%d", status), jsonResponse("SimulateResponse", http.StatusText(status)))
	}
	spec.AddOperation("/tx", http.MethodPost, txOp)
}

type operationOption func(*openapi3.Operation)

func withErrorResponse(status int, schemaName string) operationOption {
	return func(op *openapi3.Operation) {
		op.Responses.Set(fmt.Sprintf("%d", status), jsonResponse(schemaName, http.StatusText(status)))
	}
}

func withPathParameter(name string, description string) operationOption {
	return func(op *openapi3.Operation) {
		op.Parameters = append(op.Parameters, &openapi3.ParameterRef{
			Value: &openapi3.Parameter{
				Name:        name,
				In:          "path",
				Description: description,
				Required:    true,
				Schema: &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
			},
		})
	}
}

func getOperation(summary string, responseSchema string, options ...operationOption) *openapi3.Operation {
	op := openapi3.NewOperation()
	op.Summary = summary
	op.Responses = openapi3.NewResponses()
	op.Responses.Set("200", jsonResponse(responseSchema, "OK"))
	for _, option := range options {
		option(op)
	}
	return op
}

func postOperation(summary string, requestSchema string, responseSchema string) *openapi3.Operation {
	op := getOperation(summary, responseSchema)
	op.RequestBody = &openapi3.RequestBodyRef{
		Value: openapi3.NewRequestBody().
			WithRequired(true).
			WithJSONSchemaRef(schemaRef(requestSchema)),
	}
	return op
}

func jsonResponse(schemaName string, description string) *openapi3.ResponseRef {
	return &openapi3.ResponseRef{
		Value: openapi3.NewResponse().
			WithDescription(description).
			WithJSONSchemaRef(schemaRef(schemaName)),
	}
}

func schemaRef(name string) *openapi3.SchemaRef {
	return openapi3.NewSchemaRef("#/components/schemas/"+name, nil)
}

func schemaValue(schemas openapi3.Schemas, schemaName string) *openapi3.Schema {
	ref := schemas[schemaName]
	if ref == nil {
		return nil
	}
	return ref.Value
}

func propertyValue(schemas openapi3.Schemas, schemaName string, propertyName string) *openapi3.Schema {
	schema := schemaValue(schemas, schemaName)
	if schema == nil {
		return nil
	}
	ref := schema.Properties[propertyName]
	if ref == nil {
		return nil
	}
	return ref.Value
}

func setPropertyDescription(schemas openapi3.Schemas, schemaName string, propertyName string, description string) {
	if property := propertyValue(schemas, schemaName, propertyName); property != nil {
		property.Description = description
	}
}

func setPropertyExample(schemas openapi3.Schemas, schemaName string, propertyName string, example any) {
	if property := propertyValue(schemas, schemaName, propertyName); property != nil {
		property.Example = example
	}
}

func setPropertyDefault(schemas openapi3.Schemas, schemaName string, propertyName string, value any) {
	if property := propertyValue(schemas, schemaName, propertyName); property != nil {
		property.Default = value
	}
}

func setPropertyPattern(schemas openapi3.Schemas, schemaName string, propertyName string, pattern string) {
	if property := propertyValue(schemas, schemaName, propertyName); property != nil {
		property.Pattern = pattern
	}
}

func setPropertyEnum(schemas openapi3.Schemas, schemaName string, propertyName string, values ...any) {
	if property := propertyValue(schemas, schemaName, propertyName); property != nil {
		property.Enum = values
	}
}

func setPropertyMinMax(schemas openapi3.Schemas, schemaName string, propertyName string, min float64, max float64) {
	if property := propertyValue(schemas, schemaName, propertyName); property != nil {
		property.Min = &min
		property.Max = &max
	}
}
