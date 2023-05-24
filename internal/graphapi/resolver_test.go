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
			schema:   validTestSchema,
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
			errorMsg: "schema has no valid objet types",
		},
		{
			TestName: "No node interface",
			schema: `directive @prefixedID(prefix: String!) on OBJECT
			interface Actor {
				id: ID!
			}
			type User implements Actor @key(fields: "id") @prefixedID(prefix: "testusr") {
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
		TestName  string
		query     string
		response  string
		errorMsgs []string
	}{
		{
			TestName: "resolves a single type",
			query:    `{"query": "{ node(id: \"testsrv-123\") { __typename id } }" }`,
			response: `{"node":{"__typename":"Server","id":"testsrv-123"}}`,
		},
		{
			TestName: "multiple queries of different types",
			query:    `{"query": "{ nodeA: node(id: \"testsrv-987\") { __typename id } nodeB: node(id: \"testusr-654\") { __typename id } }" }`,
			response: `{"nodeA":{"__typename":"Server","id":"testsrv-987"},"nodeB":{"__typename":"User","id":"testusr-654"}}`,
		},
		{
			TestName:  "unknown prefix",
			query:     `{"query": "{ node(id: \"notreal-987\") { __typename id } }" }`,
			response:  `{"node":null}`,
			errorMsgs: []string{"invalid id; unknown prefix"},
		},
		{
			TestName:  "multiple with one unknown prefix",
			query:     `{"query": "{ nodeA: node(id: \"testing-987\") { __typename id } nodeB: node(id: \"testusr-345\") { __typename id } }" }`,
			response:  `{"nodeA":null,"nodeB":{"__typename":"User","id":"testusr-345"}}`,
			errorMsgs: []string{"invalid id; unknown prefix"},
		},
		{
			TestName:  "invalid prefix",
			query:     `{"query": "{ node(id: \"invalidtest-987\") { __typename id } }" }`,
			response:  `{"node":null}`,
			errorMsgs: []string{"invalid id: expected prefix length is 7"},
		},
		{
			TestName: "Entities request successful for known type",
			query: `{
				"query": "query($representations:[_Any!]!){_entities(representations:$representations){...on Actor{__typename ...on User{__typename id}}}}",
				"variables": {"representations": [{ "__typename": "Actor", "id": "testusr-rXirlFQULBHDw9urtOjya" }]}
				}`,
			response: `{"_entities":[{"__typename":"User","id":"testusr-rXirlFQULBHDw9urtOjya"}]}`,
		},
		{
			TestName: "Entities request successful for multiple values of known types",
			query: `{
				"query": "query($representations:[_Any!]!){_entities(representations:$representations){...on Actor{__typename id}}}",
				"variables": {"representations": [{ "__typename": "Actor", "id": "testusr-rXirlFQULBHDw9urtOjya" },{ "__typename": "Actor", "id": "testtkn-DPCwfa6KxhXp_ociFWV8C" }]}
				}`,
			response: `{"_entities":[{"__typename":"User","id":"testusr-rXirlFQULBHDw9urtOjya"},{"__typename":"Token","id":"testtkn-DPCwfa6KxhXp_ociFWV8C"}]}`,
		},
		{
			TestName: "Entities request fails for unknown type but is successful for valid IDs",
			query: `{
				"query": "query($representations:[_Any!]!){_entities(representations:$representations){...on Actor{__typename id}}}",
				"variables": {"representations": [{ "__typename": "Actor", "id": "unknown-rXirlFQULBHDw9urtOjya" },{ "__typename": "Actor", "id": "testusr-DPCwfa6KxhXp_ociFWV8C" }]}
				}`,
			response:  `{"_entities":[null,{"__typename":"User","id":"testusr-DPCwfa6KxhXp_ociFWV8C"}]}`,
			errorMsgs: []string{"unknown is an unknown id prefix"},
		},
		{
			TestName: "Entities request returns multiple errors when more than one is returned",
			query: `{
				"query": "query($representations:[_Any!]!){_entities(representations:$representations){...on Actor{__typename id}}}",
				"variables": {"representations": [{ "__typename": "Actor", "id": "testsrv-rXirlFQULBHDw9urtOjya" },{ "__typename": "Actor", "id": "testtkn-NU0CbUfS_0yGG1hzvIfDH" },{ "__typename": "Actor", "id": "unknown-DPCwfa6KxhXp_ociFWV8C" }]}
				}`,
			response:  `{"_entities":[null,{"__typename":"Token","id":"testtkn-NU0CbUfS_0yGG1hzvIfDH"},null]}`,
			errorMsgs: []string{"Server doesn't implement interface Actor", "unknown is an unknown id prefix"},
		},
		{
			TestName: "Entities request returns error for an unknown interface even if type is valid",
			query: `{
				"query": "query($representations:[_Any!]!){_entities(representations:$representations){...on Actor{__typename id}}}",
				"variables": {"representations": [{ "__typename": "Hardware", "id": "testsrv-rXirlFQULBHDw9urtOjya" },{ "__typename": "Actor", "id": "testtkn-NU0CbUfS_0yGG1hzvIfDH" }]}
				}`,
			response:  `{"_entities":[null,{"__typename":"Token","id":"testtkn-NU0CbUfS_0yGG1hzvIfDH"}]}`,
			errorMsgs: []string{"Hardware is an unknown interface type"},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.TestName, func(t *testing.T) {
			resp, err := testQuery(validTestSchema, tt.query)
			require.NoError(t, err)
			require.NotEmpty(t, resp)

			assert.Equal(t, tt.response, resp.Data)

			if len(tt.errorMsgs) == 0 {
				assert.Empty(t, resp.Errors)
			} else {
				require.NotEmpty(t, resp.Errors)
				require.Equal(t, len(tt.errorMsgs), len(resp.Errors))
				for i, msg := range tt.errorMsgs {
					assert.Contains(t, resp.Errors[i].Message, msg)
				}
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

var validTestSchema = `directive @prefixedID(prefix: String!) on OBJECT

type User implements Node & Actor @key(fields: "id") @prefixedID(prefix: "testusr") {
	id: ID!
}
type Token implements Node & Actor @key(fields: "id") @prefixedID(prefix: "testtkn") {
	id: ID!
}
type Server implements Node @key(fields: "id") @prefixedID(prefix: "testsrv") {
	id: ID!
}
interface Actor @key(fields: "id") {
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
}`
