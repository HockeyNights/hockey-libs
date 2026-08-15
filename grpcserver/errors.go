package grpcserver

import (
	"context"
	"errors"

	"github.com/HockeyNights/hockey-libs/apperr"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ErrorMappingUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			return nil, MapError(err)
		}
		return resp, nil
	}
}

func ErrorMappingStreamInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv any,
		stream grpc.ServerStream,
		_ *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		return MapError(handler(srv, stream))
	}
}

const errorDomain = "hockeynights"

func MapError(err error) error {
	if err == nil {
		return nil
	}

	appErr, isAppErr := apperr.As(err)
	if !isAppErr {
		if _, ok := status.FromError(err); ok {
			return err
		}

		if errors.Is(err, context.Canceled) {
			return status.Error(codes.Canceled, "request canceled")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return status.Error(codes.DeadlineExceeded, "request timed out")
		}

		return status.Error(codes.Internal, "internal server error")
	}

	code := CodeOf(appErr.Kind)

	if code == codes.Internal {
		return status.Error(codes.Internal, "internal server error")
	}

	st := status.New(code, appErr.Message)

	if appErr.Code != "" {
		withDetails, detailsErr := st.WithDetails(&errdetails.ErrorInfo{
			Reason: appErr.Code,
			Domain: errorDomain,
		})
		if detailsErr == nil {
			st = withDetails
		}
	}

	return st.Err()
}

func CodeOf(kind apperr.Kind) codes.Code {
	switch kind {
	case apperr.KindInvalidArgument:
		return codes.InvalidArgument
	case apperr.KindUnauthenticated:
		return codes.Unauthenticated
	case apperr.KindPermissionDenied:
		return codes.PermissionDenied
	case apperr.KindNotFound:
		return codes.NotFound
	case apperr.KindAlreadyExists:
		return codes.AlreadyExists
	case apperr.KindConflict:
		return codes.Aborted
	case apperr.KindFailedPrecondition:
		return codes.FailedPrecondition
	case apperr.KindRateLimited:
		return codes.ResourceExhausted
	case apperr.KindTimeout:
		return codes.DeadlineExceeded
	case apperr.KindUnavailable:
		return codes.Unavailable
	default:
		return codes.Internal
	}
}
