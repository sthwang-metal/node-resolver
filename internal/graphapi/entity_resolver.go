package graphapi

import (
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
	"go.infratographer.com/x/gidx"
)

// Entity represents an entity interface object when an _entities query is made
type Entity struct {
	typeName string //__typename that is provided in representations
	ID       gidx.PrefixedID
}

func (r *Resolver) entitiesResolver(p graphql.ResolveParams) (interface{}, error) {
	reps := p.Args["representations"].([]interface{})
	entities := make([]*Entity, len(reps))

	for repLoc, rep := range reps {
		re := rep.(map[string]interface{})
		id := gidx.PrefixedID(re["id"].(string))
		typename := re["__typename"].(string)

		entities[repLoc] = &Entity{typeName: typename, ID: id}
	}

	return entities, nil
}

// entityTypeResolver gets called after we convert the representations to an []*Entities. If for some reason one of those
// entities is not valid the only way to make it null and give an error is to panic with the error. This seems strange, but
// the graphql library catches the panic and returns the proper error to the user making the request.
func (r *Resolver) entityTypeResolver(p graphql.ResolveTypeParams) *graphql.Object {
	entity := p.Value.(*Entity)

	graphType, ok := r.interfaceMap[entity.typeName]
	if !ok {
		panic(gqlerrors.NewFormattedError(entity.typeName + " is an unknown interface type"))
	}

	objType, ok := r.prefixMap[entity.ID.Prefix()]
	if !ok {
		panic(gqlerrors.NewFormattedError(entity.ID.Prefix() + " is an unknown id prefix"))
	}
	if r.handlerSchema.IsPossibleType(graphType, objType) {
		return objType
	} else {
		panic(gqlerrors.NewFormattedError(objType.Name() + " doesn't implement interface " + graphType.Name()))
	}
}

func (r *Resolver) entitiesUnion() *graphql.Union {
	if r.entities != nil {
		return r.entities
	}

	entTypes := []*graphql.Object{}
	for _, obj := range r.prefixMap {
		entTypes = append(entTypes, obj)
	}

	r.entities = graphql.NewUnion(graphql.UnionConfig{
		Name:        "_Entities",
		Types:       entTypes,
		ResolveType: r.entityTypeResolver,
	})

	return r.entities
}
