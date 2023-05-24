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

type ErrInvalidSchema struct {
	message string
}

func (e ErrInvalidSchema) Error() string {
	return e.message
}

func newInvalidSchemaError(s string) error {
	return ErrInvalidSchema{message: s}
}

// Resolver provides a graph response resolver
type Resolver struct {
	logger        *zap.SugaredLogger
	schemaDoc     *ast.SchemaDocument
	prefixMap     map[string]*graphql.Object
	interfaceMap  map[string]*graphql.Interface
	scalars       map[string]*graphql.Scalar
	handlerSchema graphql.Schema
	entities      *graphql.Union
}

// NewResolver returns a resolver configured with the given logger
func NewResolver(logger *zap.SugaredLogger, rawSchema string) (*Resolver, error) {
	r := &Resolver{
		logger:       logger,
		prefixMap:    map[string]*graphql.Object{},
		interfaceMap: map[string]*graphql.Interface{},
		scalars: map[string]*graphql.Scalar{
			"_Any": {
				PrivateName: "_Any",
			},
		},
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
				gi = r.graphInterfaceFor(i)
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

		r.prefixMap[prefix] = r.graphTypeFor(obj.Name, prefix, ifaces)
	}

	if len(r.prefixMap) == 0 {
		return nil, newInvalidSchemaError("schema has no valid objet types")
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

func (r *Resolver) graphTypeFor(name string, prefix string, interfaces []*graphql.Interface) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: name,
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "The id of the node.",
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					switch o := p.Source.(type) {
					case *Node:
						return o.ID, nil
					case *Entity:
						return o.ID, nil
					default:
						return nil, errors.New("invalid node type")
					}
				},
			},
		},
		IsTypeOf: func(p graphql.IsTypeOfParams) bool {
			// TODO: This should be able to check account the name of the type instead :thinking-face:
			switch o := p.Value.(type) {
			case *Node:
				return o.ID.Prefix() == prefix
			case *Entity:
				return o.ID.Prefix() == prefix
			default:
				return false
			}
		},
		Interfaces: interfaces,
	})
}

func (r *Resolver) graphInterfaceFor(name string) *graphql.Interface {
	return graphql.NewInterface(graphql.InterfaceConfig{
		Name: name,
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "The id of the node.",
			},
		},
		ResolveType: func(p graphql.ResolveTypeParams) *graphql.Object {
			switch o := p.Value.(type) {
			case *Node:
				return o.GraphType
			case *Entity:
				return r.entityTypeResolver(graphql.ResolveTypeParams{Value: o})
			default:
				return nil
			}
		},
	})
}

func (r *Resolver) Query() (*graphql.Object, error) {
	nodeInt, ok := r.interfaceMap["Node"]
	if !ok {
		return nil, newInvalidSchemaError("interface for Node missing from schema")
	}

	return graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"node": &graphql.Field{
				Type: nodeInt,
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{
						Description: "ID of the node",
						Type:        graphql.NewNonNull(graphql.ID),
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
			"_entities": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(r.entitiesUnion())),
				Args: graphql.FieldConfigArgument{
					"representations": &graphql.ArgumentConfig{
						Description: "ID of the node",
						Type:        graphql.NewNonNull(graphql.NewList(r.scalars["_Any"])),
					},
				},
				Resolve: r.entitiesResolver,
			},
		},
	}), nil
}

func (r *Resolver) GraphTypes() []graphql.Type {
	objs := []graphql.Type{}
	for _, obj := range r.prefixMap {
		objs = append(objs, obj)
	}

	objs = append(objs, r.entitiesUnion())

	for _, obj := range r.scalars {
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
	r.logger.Infow("request info", "postData.Query", p.Query, "postData.Operation", p.Operation, "postdata.Variables", p.Variables)
	result := graphql.Do(graphql.Params{
		Context:        ctx.Request().Context(),
		Schema:         r.handlerSchema,
		RequestString:  p.Query,
		VariableValues: p.Variables,
		OperationName:  p.Operation,
	})

	return ctx.JSON(http.StatusOK, result)
}
