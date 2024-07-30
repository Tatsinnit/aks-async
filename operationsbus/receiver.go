package operationsbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sb "github.com/Azure/aks-async/servicebus"

	"github.com/Azure/aks-middleware/ctxlogger"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/Azure/go-shuttle/v2"
)

func CreateProcessor(serviceBusReceiver sb.ServiceBusReceiver, matcher *Matcher, operationController OperationController) (*shuttle.Processor, error) {
	panicOptions := &shuttle.PanicHandlerOptions{
		OnPanicRecovered: basicPanicRecovery(operationController),
	}

	//TODO(mheberling): Think if we need to change these time variables.
	lockRenewalInterval := 10 * time.Second
	lockRenewalOptions := &shuttle.LockRenewalOptions{Interval: &lockRenewalInterval}

	p := shuttle.NewProcessor(serviceBusReceiver.Receiver,
		shuttle.NewPanicHandler(panicOptions,
			shuttle.NewRenewLockHandler(lockRenewalOptions,
				myHandler(matcher, operationController))),
		&shuttle.ProcessorOptions{
			MaxConcurrency:  1,
			StartMaxAttempt: 5,
		},
	)

	return p, nil
}

// TODO(mheberling): is there a way to change this so that it doesn't rely only on azure service bus? Maybe try having a message type that has azservicebus.ReceivedMessage insinde and passing that here?
func myHandler(matcher *Matcher, operationController OperationController) shuttle.HandlerFunc {
	return func(ctx context.Context, settler shuttle.MessageSettler, message *azservicebus.ReceivedMessage) {
		logger := ctxlogger.GetLogger(ctx)

		// 1. Unmarshall the operatoin
		var body OperationRequest
		err := json.Unmarshal(message.Body, &body)
		if err != nil {
			logger.Error("Error calling ReceiveOperation: " + err.Error())
			panic(err)
		}

		if body.RetryCount >= 10 {
			logger.Error("Operation has passed the retry limit.")
			panic(errors.New(fmt.Sprintf("Operation has retried %d times. The limit is 10.", body.RetryCount)))
		}

		// 2 Match it with the correct type of operation
		operation, err := matcher.CreateInstance(body.OperationName)
		if err != nil {
			logger.Error("Operation type doesn't exist in the matcher: " + err.Error())
			panic(err)
		}

		panicOptions := &shuttle.PanicHandlerOptions{
			OnPanicRecovered: operationPanicRecovery(operation, operationController),
		}

		// We add a different panic handler here because we only want to retry the operation if: we are able to unmarshall the message,
		// the retry limit has not been exceeded, and wee were able to instantiate the operation. These 3 things will not change by
		// returning the message to the queue so we don't want to retry.
		handler := shuttle.NewPanicHandler(panicOptions, shuttle.HandlerFunc(func(ctx context.Context, settler shuttle.MessageSettler, message *azservicebus.ReceivedMessage) {

			// Set operation as IN_PROGRESS

			// 3. Init the operation with the information we have.
			_, err = operation.Init(ctx, body)
			if err != nil {
				logger.Error("Something went wrong initializing the operation.")
				panic(err)
			}

			// 4. Get the entity.
			entity, err := operation.EntityFetcher(ctx)
			if err != nil {
				logger.Error("Entity was not able to be retrieved: " + err.Error())
				panic(err)
			}

			// 5. Guard against concurrency.
			ce, err := operation.Guardconcurrency(ctx, entity)
			if err != nil {
				logger.Error("Error calling GuardConcurrency: " + err.Error())
				logger.Error("Categorized Error calling GuardConcurrency: " + ce.Error())
				panic(err)
			}

			//TODO(mheberling): Run this in a go func and check for expiration date constantly?
			// 6. Call run on the operation
			result := operation.Run(ctx)
			if result.Error != nil {
				logger.Error("Something went wrong running the operation: " + result.Error.Error())
				panic(result.Error)
			}

			// 7. Finish the message
			settleMessage(ctx, settler, message, nil)
			// Set operation as FINISHED
			operationController.OperationCompleted(ctx, operation.GetOperationRequest(ctx).OperationId)

			logger.Info("Operation run successfully!")
		}))

		handler.Handle(ctx, settler, message)
	}
}

func basicPanicRecovery(operationController OperationController) func(ctx context.Context, settler shuttle.MessageSettler, message *azservicebus.ReceivedMessage, recovered any) {
	return func(ctx context.Context, settler shuttle.MessageSettler, message *azservicebus.ReceivedMessage, recovered any) {
		logger := ctxlogger.GetLogger(ctx)
		logger.Info("Recovering from panic before getting operation.")

		var body OperationRequest
		err := json.Unmarshal(message.Body, &body)
		if err != nil {
			logger.Error("Error calling ReceiveOperation: " + err.Error())
			panic(err)
		}

		// Settle message
		settleMessage(ctx, settler, message, nil)

		// Cancel the operation
		operationController.OperationCancel(ctx, body.OperationId)
	}
}

// TODO(mheberling): Will probably have to add reason for cancellation here as well.
func operationPanicRecovery(operation APIOperation, operationController OperationController) func(ctx context.Context, settler shuttle.MessageSettler, message *azservicebus.ReceivedMessage, recovered any) {
	return func(ctx context.Context, settler shuttle.MessageSettler, message *azservicebus.ReceivedMessage, recovered any) {
		logger := ctxlogger.GetLogger(ctx)
		logger.Info("Recovering from panic after getting operation.")

		// Retry the message
		err := operation.Retry(ctx)
		if err != nil {
			logger.Error("Error retrying: " + err.Error())
		}

		// Settle message
		settleMessage(ctx, settler, message, nil)

		// Set the operation as Pending
		operationController.OperationPending(ctx, operation.GetOperationRequest(ctx).OperationId)
	}
}

func settleMessage(ctx context.Context, settler shuttle.MessageSettler, message *azservicebus.ReceivedMessage, options *azservicebus.CompleteMessageOptions) {
	logger := ctxlogger.GetLogger(ctx)
	logger.Info("Settling message.")

	err := settler.CompleteMessage(ctx, message, options)
	if err != nil {
		logger.Error("Unable to settle message.")
	}
}
