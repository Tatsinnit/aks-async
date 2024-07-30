package operationsbus

import (
	"context"
)

// OpInterface is the interface all operations will need to implement.
type APIOperation interface {
	Run(context.Context) *Result
	Retry(context.Context) error
	Guardconcurrency(context.Context, Entity) (*CategorizedError, error)
	EntityFetcher(context.Context) (Entity, error)
	Init(context.Context, OperationRequest) (APIOperation, error)
	GetName(context.Context) string
	GetOperationRequest(context.Context) *OperationRequest
	NewContextForOperation(context.Context) (context.Context, error)
}
