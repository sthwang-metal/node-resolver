package graphapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/graphql-go/graphql"
	"github.com/labstack/echo/v4"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
	"go.infratographer.com/x/gidx"

	"go.uber.org/zap"
)

var ErrMissingNodeInterface = errors.New("interface for Node missing from schema")

// Resolver provides a graph response resolver
type Resolver struct {
	logger        *zap.SugaredLogger
	schemaDoc     *ast.SchemaDocument
	prefixMap     map[string]*graphql.Object
	interfaceMap  map[string]*graphql.Interface
	handlerSchema graphql.Schema
}

// NewResolver returns a resolver configured with the given logger
func NewResolver(logger *zap.SugaredLogger, rawSchema string) (*Resolver, error) {
	r := &Resolver{
		logger:       logger,
		prefixMap:    map[string]*graphql.Object{},
		interfaceMap: map[string]*graphql.Interface{},
	}

	schema, err := parser.ParseSchemas(&ast.Source{
		Input: rawSchema,
	})
	if err != nil {
		return nil, err
	}

	r.schemaDoc = schema

	for _, obj := range r.schemaDoc.Definitions {
		if len(obj.Interfaces) == 0 {
			// this definition isn't a object that has interfaces, skip it
			continue
		}

		ifaces := []*graphql.Interface{}

		for _, i := range obj.Interfaces {
			gi, ok := r.interfaceMap[i]
			if !ok {
				gi = graphInterfaceFor(i)
				r.interfaceMap[i] = gi
			}

			ifaces = append(ifaces, gi)
		}

		pd := obj.Directives.ForName("prefixedID")
		if pd == nil {
			logger.Warnw("missing @prefixedID directive", "graphql_type", obj.Name)
			continue
		}

		pa := pd.Arguments.ForName("prefix")
		if pa == nil {
			logger.Warnw("missing prefix on @prefixedID directive", "graphql_type", obj.Name)
			continue
		}

		prefix := pa.Value.String()
		// This value has the quotes in it, so we need to strip those
		prefix = strings.Trim(prefix, `"`)

		r.prefixMap[prefix] = graphTypeFor(obj.Name, ifaces)
	}

	q, err := r.Query()
	if err != nil {
		return nil, err
	}

	r.handlerSchema, err = graphql.NewSchema(graphql.SchemaConfig{
		Query: q,
		Types: r.GraphTypes(),
	})
	if err != nil {
		return nil, err
	}

	return r, nil
}

func graphTypeFor(name string, interfaces []*graphql.Interface) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: name,
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "The id of the node.",
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if node, ok := p.Source.(*Node); ok {
						return node.ID, nil
					}
					return "", errors.New("invalid node type")
				},
			},
		},
		Interfaces: interfaces,
	})
}

func graphInterfaceFor(name string) *graphql.Interface {
	return graphql.NewInterface(graphql.InterfaceConfig{
		Name: name,
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "The id of the node.",
			},
		},
		ResolveType: func(p graphql.ResolveTypeParams) *graphql.Object {
			node, ok := p.Value.(*Node)
			if !ok {
				return nil
			}

			return node.GraphType
		},
	})
}

func (r *Resolver) Query() (*graphql.Object, error) {
	nodeInt, ok := r.interfaceMap["Node"]
	if !ok {
		return nil, ErrMissingNodeInterface
	}

	return graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"node": &graphql.Field{
				Type: nodeInt,
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{
						Description: "ID of the node",
						Type:        graphql.ID,
					},
				},
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					id, err := gidx.Parse(p.Args["id"].(string))
					if err != nil {
						return nil, err
					}
					return r.GetNode(id)
				},
			},
		},
	}), nil
}

func (r *Resolver) GraphTypes() []graphql.Type {
	objs := []graphql.Type{}
	for _, obj := range r.prefixMap {
		objs = append(objs, obj)
	}

	return objs
}

type postData struct {
	Query     string                 `json:"query"`
	Operation string                 `json:"operation"`
	Variables map[string]interface{} `json:"variables"`
}

func (r *Resolver) Routes(e *echo.Group) {
	e.POST("/query", r.GraphHandler)
}

func (r *Resolver) GraphHandler(ctx echo.Context) error {
	var p postData
	if err := json.NewDecoder(ctx.Request().Body).Decode(&p); err != nil {
		return err
	}
	result := graphql.Do(graphql.Params{
		Context:        ctx.Request().Context(),
		Schema:         r.handlerSchema,
		RequestString:  p.Query,
		VariableValues: p.Variables,
		OperationName:  p.Operation,
	})

	return ctx.JSON(http.StatusOK, result)
}
