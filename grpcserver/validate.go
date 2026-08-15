package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"buf.build/go/protovalidate"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func ValidationUnaryInterceptor() (grpc.UnaryServerInterceptor, error) {
	validator, err := protovalidate.New()
	if err != nil {
		return nil, fmt.Errorf("grpcserver: create validator: %w", err)
	}

	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		message, ok := req.(proto.Message)
		if !ok {
			return handler(ctx, req)
		}

		if err := validator.Validate(message); err != nil {
			return nil, validationStatus(err)
		}

		return handler(ctx, req)
	}, nil
}

func MustValidationUnaryInterceptor() grpc.UnaryServerInterceptor {
	interceptor, err := ValidationUnaryInterceptor()
	if err != nil {
		panic(err)
	}
	return interceptor
}

func validationStatus(err error) error {
	var validationErr *protovalidate.ValidationError
	if !errors.As(err, &validationErr) {
		return status.Error(codes.Internal, "validation failed")
	}

	violations := make([]*errdetails.BadRequest_FieldViolation, 0, len(validationErr.Violations))
	messages := make([]string, 0, len(validationErr.Violations))

	for _, violation := range validationErr.Violations {
		if violation.Proto == nil {
			continue
		}

		field := protovalidate.FieldPathString(violation.Proto.GetField())
		if field == "" {
			field = violation.Proto.GetRuleId()
		}

		description := violation.Proto.GetMessage()

		violations = append(violations, &errdetails.BadRequest_FieldViolation{
			Field:       field,
			Description: description,
		})
		messages = append(messages, field+": "+description)
	}

	st := status.New(codes.InvalidArgument, strings.Join(messages, "; "))

	withDetails, detailsErr := st.WithDetails(&errdetails.BadRequest{FieldViolations: violations})
	if detailsErr != nil {
		return st.Err()
	}

	return withDetails.Err()
}
