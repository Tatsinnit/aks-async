package operationsbus

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

type LongRunning struct{}

var _ APIOperation = (*LongRunning)(nil)

func TestMatcher(t *testing.T) {
	matcher := NewMatcher()

	operation := "LongRunning"
	matcher.Register(operation, &LongRunning{})

	retrieved, exists := matcher.Get(operation)
	if !exists {
		t.Fatalf("Operation %s should exist in the matcher, instead got: %t", operation, exists)
	}

	longRunningOp := &LongRunning{}
	longRunningOpType := reflect.TypeOf(longRunningOp).Elem()
	if retrieved != longRunningOpType {
		t.Fatalf("Expected %s. Instead got: %s", longRunningOpType, retrieved)
	}

	// Retrieve an instance of the type associated with the key operation
	instance, err := matcher.CreateInstance(operation)
	if err != nil {
		fmt.Println("Type not found")
		return
	}

	// Check if the created element is of the correct type.
	if reflect.TypeOf(instance).Elem() != longRunningOpType {
		t.Fatalf("The created instance is not of the correct type")
	}

	result := instance.Run(context.TODO())
	if result.HTTPCode != 200 {
		t.Fatalf("Result did not equal 200.")
	}
}

// Example implementation of APIOperation for LongRunning
func (lr *LongRunning) Run(ctx context.Context) *Result {
	fmt.Println("Running LongRunning operation")
	return &Result{
		HTTPCode: 200,
		Message:  "OK",
		Error:    nil,
	}
}

func (lr *LongRunning) Guardconcurrency(ctx context.Context, entity Entity) (*CategorizedError, error) {
	fmt.Println("Guarding concurrency in LongRunning operation")
	return &CategorizedError{}, nil
}

func (lr *LongRunning) Init(ctx context.Context, req OperationRequest) (APIOperation, error) {
	fmt.Println("Initializing LongRunning operation with request")
	return nil, nil
}

func (lr *LongRunning) GetOperationRequest(ctx context.Context) *OperationRequest {
	return &OperationRequest{}
}
