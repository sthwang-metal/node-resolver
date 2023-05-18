package graphapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"go.infratographer.com/node-resolver/internal/graphapi"
)

func TestSchemaParsing(t *testing.T) {
	testCases := []struct {
		TestName string
		schema   string
		errorMsg string
	}{
		{
			TestName: "Valid Schema",
			schema: `directive @prefixedID(prefix: String!) on OBJECT

				type Server implements Node @key(fields: "id") @prefixedID(prefix: "testsrv") {
					id: ID!
				}
				type User implements Node @key(fields: "id") @prefixedID(prefix: "testusr") {
					id: ID!
				}
				interface Node @key(fields: "id") {
					id: ID!
				}
				type Query {
					"""
					Lookup a node by id.
					"""
					node(
						"""
						The ID of the node.
						"""
						id: ID!
					): Node!
				}`,
		},
		{
			TestName: "Schema with missing prefixedID on a type",
			schema: `directive @prefixedID(prefix: String!) on OBJECT
				type Server implements Node @key(fields: "id") @prefixedID(prefix: "testsrv") {
					id: ID!
				}
				interface Node @key(fields: "id") {
					id: ID!
				}`,
		},
		{
			TestName: "Schema with an invalid prefixedID",
			schema: `directive @prefixedID(prefix: String!) on OBJECT
				type Server implements Node @key(fields: "id") @prefixedID(NotmyPrefix: "testsrv") {
					id: ID!
				}
				interface Node @key(fields: "id") {
					id: ID!
				}`,
		},
		{
			TestName: "No node interface",
			schema: `type Server @key(fields: "id") @prefixedID(prefix: "testsrv") {
					id: ID!
				}`,
			errorMsg: "interface for Node missing from schema",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.TestName, func(t *testing.T) {
			r, err := graphapi.NewResolver(zap.NewNop().Sugar(), tt.schema)
			if tt.errorMsg == "" {
				assert.NoError(t, err)
				assert.NotNil(t, r)
			} else {
				assert.Error(t, err)
				assert.Nil(t, r)
				assert.ErrorContains(t, err, tt.errorMsg)
			}
		})
	}
}

func TestNodeResolving(t *testing.T) {
	testCases := []struct {
		TestName string
		schema   string
		query    string
		response string
		errorMsg string
	}{
		{
			TestName: "Happy Path - first type",
			schema: `directive @prefixedID(prefix: String!) on OBJECT

				type Server implements Node @key(fields: "id") @prefixedID(prefix: "testsrv") {
					id: ID!
				}
				type User implements Node @key(fields: "id") @prefixedID(prefix: "testusr") {
					id: ID!
				}
				interface Node @key(fields: "id") {
					id: ID!
				}
				type Query {
					"""
					Lookup a node by id.
					"""
					node(
						"""
						The ID of the node.
						"""
						id: ID!
					): Node!
				}`,
			query:    `{"query": "{ node(id: \"testsrv-123\") { __typename id } }" }`,
			response: `{"node":{"__typename":"Server","id":"testsrv-123"}}`,
		},
		{
			TestName: "Happy Path - second type",
			schema: `directive @prefixedID(prefix: String!) on OBJECT

				type Server implements Node @key(fields: "id") @prefixedID(prefix: "testsrv") {
					id: ID!
				}
				type User implements Node @key(fields: "id") @prefixedID(prefix: "testusr") {
					id: ID!
				}
				interface Node @key(fields: "id") {
					id: ID!
				}
				type Query {
					"""
					Lookup a node by id.
					"""
					node(
						"""
						The ID of the node.
						"""
						id: ID!
					): Node!
				}`,
			query:    `{"query": "{ node(id: \"testusr-123\") { __typename id } }" }`,
			response: `{"node":{"__typename":"User","id":"testusr-123"}}`,
		},
		{
			TestName: "multiple queries of different types",
			schema: `directive @prefixedID(prefix: String!) on OBJECT

				type Server implements Node @key(fields: "id") @prefixedID(prefix: "testsrv") {
					id: ID!
				}
				type User implements Node @key(fields: "id") @prefixedID(prefix: "testusr") {
					id: ID!
				}
				interface Node @key(fields: "id") {
					id: ID!
				}
				type Query {
					"""
					Lookup a node by id.
					"""
					node(
						"""
						The ID of the node.
						"""
						id: ID!
					): Node!
				}`,
			query:    `{"query": "{ nodeA: node(id: \"testsrv-987\") { __typename id } nodeB: node(id: \"testusr-654\") { __typename id } }" }`,
			response: `{"nodeA":{"__typename":"Server","id":"testsrv-987"},"nodeB":{"__typename":"User","id":"testusr-654"}}`,
		},
		{
			TestName: "unknown prefix",
			schema: `directive @prefixedID(prefix: String!) on OBJECT

				type User implements Node @key(fields: "id") @prefixedID(prefix: "testusr") {
					id: ID!
				}
				interface Node @key(fields: "id") {
					id: ID!
				}
				type Query {
					"""
					Lookup a node by id.
					"""
					node(
						"""
						The ID of the node.
						"""
						id: ID!
					): Node!
				}`,
			query:    `{"query": "{ node(id: \"notreal-987\") { __typename id } }" }`,
			response: `{"node":null}`,
			errorMsg: "invalid id; unknown prefix",
		},
		{
			TestName: "multiple with one unknown prefix",
			schema: `directive @prefixedID(prefix: String!) on OBJECT

				type User implements Node @key(fields: "id") @prefixedID(prefix: "testusr") {
					id: ID!
				}
				interface Node @key(fields: "id") {
					id: ID!
				}
				type Query {
					"""
					Lookup a node by id.
					"""
					node(
						"""
						The ID of the node.
						"""
						id: ID!
					): Node!
				}`,
			query:    `{"query": "{ nodeA: node(id: \"testsrv-987\") { __typename id } nodeB: node(id: \"testusr-345\") { __typename id } }" }`,
			response: `{"nodeA":null,"nodeB":{"__typename":"User","id":"testusr-345"}}`,
			errorMsg: "invalid id; unknown prefix",
		},
		{
			TestName: "invalid prefix",
			schema: `directive @prefixedID(prefix: String!) on OBJECT

				type User implements Node @key(fields: "id") @prefixedID(prefix: "testusr") {
					id: ID!
				}
				interface Node @key(fields: "id") {
					id: ID!
				}
				type Query {
					"""
					Lookup a node by id.
					"""
					node(
						"""
						The ID of the node.
						"""
						id: ID!
					): Node!
				}`,
			query:    `{"query": "{ node(id: \"invalidtest-987\") { __typename id } }" }`,
			response: `{"node":null}`,
			errorMsg: "invalid id: expected prefix length is 7",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.TestName, func(t *testing.T) {
			resp, err := testQuery(tt.schema, tt.query)
			require.NoError(t, err)
			require.NotEmpty(t, resp)

			assert.Equal(t, tt.response, resp.Data)

			if tt.errorMsg == "" {
				assert.Empty(t, resp.Errors)
			} else {
				require.NotEmpty(t, resp.Errors)
				assert.Contains(t, resp.Errors[0].Message, tt.errorMsg)
			}
		})
	}
}

type queryResponse struct {
	Data    string
	RawData json.RawMessage `json:"data"`
	Errors  []queryError    `json:"errors"`
}

type queryError struct {
	Message   string               `json:"message"`
	Locations []queryErrorLocation `json:"locations"`
}

type queryErrorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

func testQuery(schema string, query string) (*queryResponse, error) {
	r, err := graphapi.NewResolver(zap.NewNop().Sugar(), schema)
	if err != nil {
		return nil, err
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/graphql", strings.NewReader(query))
	e := echo.New()
	c := e.NewContext(req, rec)

	err = r.GraphHandler(c)
	if err != nil {
		fmt.Println("error in handler")
		return nil, err
	}

	if rec.Code != http.StatusOK {
		return nil, fmt.Errorf("non-200 response code; got %d", rec.Code)
	}

	var resp queryResponse
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	if err != nil {
		return nil, err
	}

	resp.Data = string(resp.RawData)

	return &resp, nil
}
