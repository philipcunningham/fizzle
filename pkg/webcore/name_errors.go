package webcore

import (
	"errors"

	"github.com/philipcunningham/fizzle/pkg/disk"
	fzfmodel "github.com/philipcunningham/fizzle/pkg/fzf"
)

func nameBoundaryError(err error, subject string, empty, tooLong, notASCII error) *Error {
	switch {
	case errors.Is(err, empty), errors.Is(err, tooLong):
		return errf(codeInvalidValue, "%s name must be 1 to %d characters", subject, disk.LabelSize)
	case errors.Is(err, notASCII):
		var nameErr *fzfmodel.NameError
		if errors.As(err, &nameErr) {
			return errf(codeInvalidValue, "%s name contains non-ASCII character %q", subject, string(nameErr.Character))
		}
		return errf(codeInvalidValue, "%s name contains a non-ASCII character", subject)
	default:
		return nil
	}
}
