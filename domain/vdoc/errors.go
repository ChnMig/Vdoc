package vdoc

import (
	"errors"

	commonvdoc "vdoc/common/vdoc"
)

var (
	ErrInvalidArgument    = commonvdoc.ErrInvalidArgument
	ErrUnauthenticated    = commonvdoc.ErrUnauthenticated
	ErrPermissionDenied   = commonvdoc.ErrPermissionDenied
	ErrNotFound           = commonvdoc.ErrNotFound
	ErrAlreadyExists      = commonvdoc.ErrAlreadyExists
	ErrFailedPrecondition = commonvdoc.ErrFailedPrecondition
)

func Is(err, target error) bool { return errors.Is(err, target) }
