package graphapi

import (
	"errors"

	"github.com/graphql-go/graphql"
	"go.infratographer.com/x/gidx"
)

var ErrUnknownPrefix = errors.New("invalid id; unknown prefix")

type Node struct {
	ID        gidx.PrefixedID `json:"id"`
	GraphType *graphql.Object
}

func (r *Resolver) GetNode(id gidx.PrefixedID) (*Node, error) {
	if resType, ok := r.prefixMap[id.Prefix()]; ok {
		return &Node{
			ID:        id,
			GraphType: resType,
		}, nil
	}

	return nil, ErrUnknownPrefix
}
