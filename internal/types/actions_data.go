package types

import "fmt"

// MCPCallAction represents calling an MCP (Model Context Protocol) server.
type MCPCallAction struct {
	// Server is the MCP server identifier.
	Server string `json:"server"`
	// Method is the method to call on the server.
	Method string `json:"method"`
	// Params are the parameters for the method call.
	Params map[string]any `json:"params,omitempty"`
}

func (a MCPCallAction) GetActionType() ActionType { return ActionMCPCall }
func (a MCPCallAction) Target() string            { return a.Server + "/" + a.Method }
func (a MCPCallAction) Validate() error {
	if a.Server == "" {
		return fmt.Errorf("server is required")
	}
	if a.Method == "" {
		return fmt.Errorf("method is required")
	}
	return nil
}

// DBQueryAction represents executing a database query.
type DBQueryAction struct {
	// Driver is the database driver ("sqlite", "postgres", etc.).
	Driver string `json:"driver"`
	// DSN is the data source name (connection string).
	DSN string `json:"dsn,omitempty"`
	// ConnectionString is the database connection string (alternative to Driver).
	ConnectionString string `json:"connection_string,omitempty"`
	// Query is the SQL query to execute.
	Query string `json:"query"`
	// Args are query parameters (alias for Params).
	Args []any `json:"args,omitempty"`
	// Params are query parameters.
	Params []any `json:"params,omitempty"`
}

func (a DBQueryAction) GetActionType() ActionType { return ActionDBQuery }
func (a DBQueryAction) Target() string            { return "<db>" }
func (a DBQueryAction) Validate() error {
	if a.Driver == "" {
		return fmt.Errorf("driver is required")
	}
	if a.Query == "" {
		return fmt.Errorf("query is required")
	}
	return nil
}

// DBAssertAction represents asserting a database state.
type DBAssertAction struct {
	// Driver is the database driver ("sqlite", "postgres", etc.).
	Driver string `json:"driver"`
	// DSN is the data source name (connection string).
	DSN string `json:"dsn,omitempty"`
	// ConnectionString is the database connection string (alternative to Driver).
	ConnectionString string `json:"connection_string,omitempty"`
	// Query is the SQL query to execute for assertion.
	Query string `json:"query"`
	// Assertion is the assertion expression (e.g., "count == 1").
	Assertion string `json:"assertion"`
	// Params are query parameters.
	Params []any `json:"params,omitempty"`
}

func (a DBAssertAction) GetActionType() ActionType { return ActionDBAssert }
func (a DBAssertAction) Target() string            { return a.Query }
func (a DBAssertAction) Validate() error {
	if a.Driver == "" {
		return fmt.Errorf("driver is required")
	}
	if a.Query == "" {
		return fmt.Errorf("query is required")
	}
	if a.Assertion == "" {
		return fmt.Errorf("assertion is required")
	}
	return nil
}

// GraphQLQueryAction represents executing a GraphQL query.
type GraphQLQueryAction struct {
	// URL is the GraphQL endpoint URL.
	URL string `json:"url"`
	// Query is the GraphQL query string.
	Query string `json:"query"`
	// Variables are query variables.
	Variables map[string]any `json:"variables,omitempty"`
	// Headers are optional request headers.
	Headers map[string]string `json:"headers,omitempty"`
	// OperationName is the GraphQL operation name.
	OperationName string `json:"operation_name,omitempty"`
}

func (a GraphQLQueryAction) GetActionType() ActionType { return ActionGraphQLQuery }
func (a GraphQLQueryAction) Target() string            { return a.URL }
func (a GraphQLQueryAction) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	if a.Query == "" {
		return fmt.Errorf("query is required")
	}
	return nil
}
