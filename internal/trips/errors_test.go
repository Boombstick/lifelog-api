package trips

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestKindHTTPStatus(t *testing.T) {
	for kind, want := range map[Kind]int{
		KindValidation:   http.StatusBadRequest,
		KindNotFound:     http.StatusNotFound,
		KindConflict:     http.StatusConflict,
		KindUnauthorized: http.StatusUnauthorized,
		KindForbidden:    http.StatusForbidden,
		Kind(0):          http.StatusInternalServerError,
	} {
		if got := kind.HTTPStatus(); got != want {
			t.Errorf("Kind(%d).HTTPStatus() = %d, want %d", kind, got, want)
		}
	}
}

func TestStatusOfAndPublicMessage(t *testing.T) {
	plain := errors.New("connection reset by peer")

	cases := []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"validation", Validation("плохо"), http.StatusBadRequest, "плохо"},
		{"not found", NotFound(MsgTripNotFound), http.StatusNotFound, MsgTripNotFound},
		{"conflict", Conflict(MsgSequenceMoreThanOne), http.StatusConflict, MsgSequenceMoreThanOne},
		{"forbidden", Forbidden("нельзя"), http.StatusForbidden, "нельзя"},

		{"wrapped", fmt.Errorf("saving: %w", Conflict("занято")), http.StatusConflict, "занято"},

		{"infrastructure", plain, http.StatusInternalServerError, MsgInternal},
		{"integrity", &IntegrityError{SQLState: "23505", Message: "duplicate key"}, http.StatusInternalServerError, MsgInternal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StatusOf(c.err); got != c.status {
				t.Errorf("StatusOf = %d, want %d", got, c.status)
			}
			if got := PublicMessage(c.err); got != c.message {
				t.Errorf("PublicMessage = %q, want %q", got, c.message)
			}
		})
	}
}

func TestOnlyMissingSnapshotsAreRetryable(t *testing.T) {
	if !NoSnapshots(MsgNoSnapshotsLatest).Retryable {
		t.Error("a missing rate snapshot must be retryable")
	}
	if StatusOf(NoSnapshots(MsgNoSnapshotsLatest)) != http.StatusConflict {
		t.Error("over REST a missing snapshot is a 409")
	}
	for _, err := range []*Error{Validation("x"), NotFound("x"), Conflict("x"), Forbidden("x")} {
		if err.Retryable {
			t.Errorf("%v must be final for sync", err)
		}
	}
}
