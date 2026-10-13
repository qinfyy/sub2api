package service

import "context"

// TransactionRunner joins the current transaction, or owns a new one. The
// callback must not perform external side effects; it is never retried here.
type TransactionRunner interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}
