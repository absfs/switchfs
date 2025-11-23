package switchfs

import (
	"os"

	"github.com/absfs/absfs"
)

// OperationType represents the type of filesystem operation
type OperationType string

const (
	OpOpen      OperationType = "open"
	OpCreate    OperationType = "create"
	OpRemove    OperationType = "remove"
	OpRename    OperationType = "rename"
	OpStat      OperationType = "stat"
	OpMkdir     OperationType = "mkdir"
	OpChmod     OperationType = "chmod"
	OpChown     OperationType = "chown"
	OpChtimes   OperationType = "chtimes"
	OpTruncate  OperationType = "truncate"
)

// OperationContext contains context about a filesystem operation
type OperationContext struct {
	Operation  OperationType
	Path       string
	Backend    absfs.FileSystem
	Route      *Route
	Error      error
}

// Middleware intercepts filesystem operations
type Middleware interface {
	// Before is called before an operation
	Before(ctx *OperationContext) error

	// After is called after an operation
	After(ctx *OperationContext)
}

// middlewareChain applies multiple middleware in sequence
type middlewareChain struct {
	middlewares []Middleware
}

func (mc *middlewareChain) Before(ctx *OperationContext) error {
	for _, mw := range mc.middlewares {
		if err := mw.Before(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (mc *middlewareChain) After(ctx *OperationContext) {
	// Call in reverse order
	for i := len(mc.middlewares) - 1; i >= 0; i-- {
		mc.middlewares[i].After(ctx)
	}
}

// loggingMiddleware logs filesystem operations
type loggingMiddleware struct {
	logger func(string)
}

func (lm *loggingMiddleware) Before(ctx *OperationContext) error {
	if lm.logger != nil {
		lm.logger("Operation: " + string(ctx.Operation) + " Path: " + ctx.Path)
	}
	return nil
}

func (lm *loggingMiddleware) After(ctx *OperationContext) {
	if lm.logger != nil && ctx.Error != nil {
		lm.logger("Operation failed: " + ctx.Error.Error())
	}
}

// NewLoggingMiddleware creates a middleware that logs operations
func NewLoggingMiddleware(logger func(string)) Middleware {
	return &loggingMiddleware{logger: logger}
}

// accessControlMiddleware enforces access control rules
type accessControlMiddleware struct {
	allowRead  func(string) bool
	allowWrite func(string) bool
}

func (acm *accessControlMiddleware) Before(ctx *OperationContext) error {
	// Check read operations
	if ctx.Operation == OpOpen || ctx.Operation == OpStat {
		if acm.allowRead != nil && !acm.allowRead(ctx.Path) {
			return os.ErrPermission
		}
	}

	// Check write operations
	if ctx.Operation == OpCreate || ctx.Operation == OpRemove || ctx.Operation == OpRename ||
		ctx.Operation == OpMkdir || ctx.Operation == OpChmod || ctx.Operation == OpChown ||
		ctx.Operation == OpChtimes || ctx.Operation == OpTruncate {
		if acm.allowWrite != nil && !acm.allowWrite(ctx.Path) {
			return os.ErrPermission
		}
	}

	return nil
}

func (acm *accessControlMiddleware) After(ctx *OperationContext) {
	// No-op
}

// NewAccessControlMiddleware creates a middleware that enforces access control
func NewAccessControlMiddleware(allowRead, allowWrite func(string) bool) Middleware {
	return &accessControlMiddleware{
		allowRead:  allowRead,
		allowWrite: allowWrite,
	}
}
